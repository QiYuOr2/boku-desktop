//go:build windows

package playtime

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/platform/proc"
)

// steamSampler names the Steam games behind a process list.
//
// A process is asked for its path, and for SteamAppId, only the first time
// it is seen. Exit times come from a wait on that process, not from the
// following poll.
type steamSampler struct {
	mu       sync.Mutex
	root     string
	lib      Library
	loaded   time.Time
	tracked  map[int]tracked
	rejected map[int]time.Time
}

type tracked struct {
	appID   string
	name    string
	created time.Time
}

type departed struct {
	appID string
	at    time.Time
	known bool
}

func canRecord() bool { return true }

func newSampler() (Sampler, error) {
	return &steamSampler{tracked: map[int]tracked{}, rejected: map[int]time.Time{}}, nil
}

func (s *steamSampler) Sample() (Sample, error) {
	clocks, err := proc.ReadClocks()
	if err != nil {
		return Sample{}, err
	}
	list, err := proc.Snapshot()
	if err != nil {
		return Sample{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	alive := map[int]proc.Process{}
	for _, process := range list {
		alive[process.PID] = process
	}
	var gone []departed
	for pid, game := range s.tracked {
		process, ok := alive[pid]
		if ok && sameInstant(process.CreatedAt, game.created) {
			continue
		}
		exit, known := proc.TakeExit(pid, game.created)
		gone = append(gone, departed{appID: game.appID, at: exit, known: known})
		delete(s.tracked, pid)
	}
	for pid, created := range s.rejected {
		process, ok := alive[pid]
		if !ok || !sameInstant(process.CreatedAt, created) {
			delete(s.rejected, pid)
		}
	}
	pending := map[int]proc.Process{}
	for pid, process := range alive {
		if game, ok := s.tracked[pid]; ok && sameInstant(process.CreatedAt, game.created) {
			continue
		}
		if created, ok := s.rejected[pid]; ok && sameInstant(process.CreatedAt, created) {
			continue
		}
		if notAGame(process.Name) {
			continue
		}
		pending[pid] = process
	}
	for len(pending) > 0 {
		progress := false
		for pid, process := range pending {
			appID, name, ok := s.identify(pid, process, alive)
			if !ok {
				continue
			}
			s.tracked[pid] = tracked{appID: appID, name: name, created: process.CreatedAt}
			_ = proc.WatchExit(pid, process.CreatedAt)
			delete(pending, pid)
			progress = true
		}
		if !progress {
			for pid, process := range pending {
				s.rejected[pid] = process.CreatedAt
			}
			break
		}
	}
	var stopped []Stop
	unknown := map[string]bool{}
	knownExit := map[string]time.Time{}
	for _, item := range gone {
		if !item.known {
			unknown[item.appID] = true
			continue
		}
		if item.at.After(knownExit[item.appID]) {
			knownExit[item.appID] = item.at
		}
	}
	for appID := range unknown {
		if !s.stillTracked(appID) {
			stopped = append(stopped, Stop{AppID: appID})
		}
	}
	for appID, at := range knownExit {
		if unknown[appID] || s.stillTracked(appID) {
			continue
		}
		stopped = append(stopped, Stop{AppID: appID, At: at})
	}
	games := map[string]Game{}
	for _, game := range s.tracked {
		current, ok := games[game.appID]
		if !ok || (!game.created.IsZero() && (current.RunningSince.IsZero() || game.created.Before(current.RunningSince))) {
			if current.Name != "" && game.name == "" {
				game.name = current.Name
			}
			games[game.appID] = Game{AppID: game.appID, Name: game.name, RunningSince: game.created}
			continue
		}
		if current.Name == "" && game.name != "" {
			current.Name = game.name
			games[game.appID] = current
		}
	}
	sample := Sample{At: time.Now().UTC(), Awake: clocks.Awake, Biased: clocks.Biased, Stopped: stopped}
	for _, game := range games {
		sample.Games = append(sample.Games, game)
	}
	return sample, nil
}

func (s *steamSampler) refresh() {
	if !s.loaded.IsZero() && time.Since(s.loaded) < time.Minute {
		return
	}
	root := s.root
	if root == "" {
		found, err := proc.SteamRoot()
		if err != nil {
			s.loaded = time.Now()
			return
		}
		root = found
		s.root = found
	}
	lib, err := LoadLibrary(root)
	if err != nil {
		s.loaded = time.Now()
		return
	}
	s.lib = lib
	s.loaded = time.Now()
}

func (s *steamSampler) identify(pid int, process proc.Process, alive map[int]proc.Process) (string, string, bool) {
	exe := process.Exe
	if exe == "" {
		if path, err := proc.ImagePath(pid); err == nil {
			exe = path
			process.Exe = path
		}
	}
	if appID, name, ok := s.lib.Identify(exe); ok {
		return appID, name, true
	}
	if parent, ok := s.tracked[process.PPID]; ok {
		return parent.appID, parent.name, true
	}
	if !steamOwned(process, exe, alive) {
		return "", "", false
	}
	appID, err := proc.SteamAppID(pid)
	if err != nil || appID == "" {
		return "", "", false
	}
	return appID, strings.TrimSuffix(process.Name, filepath.Ext(process.Name)), true
}

func steamOwned(process proc.Process, exe string, alive map[int]proc.Process) bool {
	if strings.Contains(strings.ToLower(exe), `\steamapps\`) {
		return true
	}
	pid := process.PPID
	for hop := 0; hop < 8 && pid > 0; hop++ {
		parent, ok := alive[pid]
		if !ok {
			return false
		}
		if strings.EqualFold(parent.Name, "steam.exe") {
			return true
		}
		pid = parent.PPID
	}
	return false
}

func (s *steamSampler) stillTracked(appID string) bool {
	for _, game := range s.tracked {
		if game.appID == appID {
			return true
		}
	}
	return false
}

func notAGame(name string) bool {
	switch strings.ToLower(name) {
	case "steam.exe", "steamwebhelper.exe", "steamservice.exe", "gameoverlayui.exe", "steamerrorreporter.exe", "streaming_client.exe":
		return true
	default:
		return false
	}
}

func sameInstant(a, b time.Time) bool {
	return a.UTC().Truncate(time.Second).Equal(b.UTC().Truncate(time.Second))
}
