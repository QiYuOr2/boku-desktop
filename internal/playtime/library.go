package playtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installed is one game directory the Steam libraries named.
type installed struct {
	appID  string
	name   string
	prefix string
}

// Library matches an executable path to the Steam game installed there.
type Library struct{ games []installed }

// LoadLibrary reads Steam's library folders and the manifest of each
// installed game. root is the Steam installation, the directory that
// contains steamapps.
func LoadLibrary(root string) (Library, error) {
	root = filepath.Clean(root)
	folders := []string{root}
	more, err := libraryFolders(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil && !os.IsNotExist(err) {
		return Library{}, err
	}
	for _, folder := range more {
		if !samePath(folder, root) {
			folders = append(folders, folder)
		}
	}
	var games []installed
	for _, folder := range folders {
		manifests, err := filepath.Glob(filepath.Join(folder, "steamapps", "appmanifest_*.acf"))
		if err != nil {
			return Library{}, err
		}
		for _, manifest := range manifests {
			game, ok, err := readManifest(folder, manifest)
			if err != nil || !ok {
				continue
			}
			games = append(games, game)
		}
	}
	return Library{games: games}, nil
}

// Identify returns the game whose install directory contains exe.
// The longest directory wins, so one install cannot claim a game that
// lives inside another.
func (l Library) Identify(exe string) (appID, name string, ok bool) {
	exe = normalizePath(exe)
	var best installed
	for _, game := range l.games {
		if exe == strings.TrimSuffix(game.prefix, string(filepath.Separator)) || strings.HasPrefix(exe, game.prefix) {
			if len(game.prefix) > len(best.prefix) {
				best = game
			}
		}
	}
	if best.appID == "" {
		return "", "", false
	}
	return best.appID, best.name, true
}

func readManifest(library, path string) (installed, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return installed{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	root, err := parseVDF(string(data))
	if err != nil {
		return installed{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	state, _ := root["AppState"].(map[string]any)
	appID, _ := state["appid"].(string)
	name, _ := state["name"].(string)
	dir, _ := state["installdir"].(string)
	if appID == "" || dir == "" {
		return installed{}, false, nil
	}
	prefix := normalizePath(filepath.Join(library, "steamapps", "common", dir)) + string(filepath.Separator)
	return installed{appID: appID, name: name, prefix: prefix}, true, nil
}

func libraryFolders(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := parseVDF(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	folders, _ := root["libraryfolders"].(map[string]any)
	var paths []string
	for _, value := range folders {
		entry, _ := value.(map[string]any)
		folder, _ := entry["path"].(string)
		if folder != "" {
			paths = append(paths, filepath.Clean(folder))
		}
	}
	return paths, nil
}

func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func samePath(a, b string) bool { return normalizePath(a) == normalizePath(b) }
