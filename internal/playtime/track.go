package playtime

import (
	"sort"
	"strings"
	"time"

	"boku/internal/domain"
)

// sleepGap 是睡眠时钟需领先清醒时长多少，才把间隔视为睡眠。几秒一次的轮询
// 会让两个时钟同步推进，两者间的噪声远低于此。
const sleepGap = 2 * time.Second

// Tracker 把采样变成一局一局的记录。
//
// 它保留每一个已接受的会话。关闭一局不会丢弃它：
// 文件与图表读取的是同一份列表。
type Tracker struct {
	sessions []Session
	at       time.Time
	awake    time.Duration
	biased   time.Duration
	seen     bool
}

func (t *Tracker) Restore(sessions []Session) {
	t.sessions = append([]Session(nil), sessions...)
}

func (t *Tracker) Sessions() []Session {
	return append([]Session(nil), t.sessions...)
}

// CloseAbandoned 结束进程停止时仍处于打开状态的会话。每个会话结束于它自己
// 的最后一次观察，而不是本次调用的时刻：崩溃到下次启动之间的时间不算游玩。
func (t *Tracker) CloseAbandoned() bool {
	changed := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := t.sessions[i].LastSeenAt.UTC().Truncate(time.Second)
		if end.IsZero() || end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		changed = true
	}
	return changed
}

// Touch 把每个打开会话的最后观察时间向前推进。
//
// 由调用方按自己的心跳保存。Observe 不保存，否则一直开着的游戏会在每次轮询时重写文件。
func (t *Tracker) Touch(at time.Time) bool {
	at = at.UTC().Truncate(time.Second)
	moved := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		t.sessions[i].LastSeenAt = at
		moved = true
	}
	return moved
}

// Observe 应用一次采样。返回的 bool 表示是否有会话开启、关闭或被切分，
// 这正是需要写文件的时刻。
func (t *Tracker) Observe(sample Sample) (bool, error) {
	at := sample.At.UTC().Truncate(time.Second)
	if at.IsZero() {
		return false, nil
	}
	changed := false
	if t.seen && sample.Awake >= t.awake && sample.Biased >= t.biased {
		asleep := (sample.Biased - t.biased) - (sample.Awake - t.awake)
		if asleep >= sleepGap {
			cut := t.at.Add(sample.Awake - t.awake).UTC().Truncate(time.Second)
			if cut.Before(t.at) {
				cut = t.at
			}
			if t.closeAll(cut) {
				changed = true
			}
		}
	}

	for _, stop := range sample.Stopped {
		if t.closeApp(stop.AppID, stopAt(stop.At, at)) {
			changed = true
		}
	}

	open := map[string]int{}
	for i := range t.sessions {
		if t.sessions[i].EndedAt.IsZero() {
			open[t.sessions[i].AppID] = i
		}
	}
	present := map[string]Game{}
	for _, game := range sample.Games {
		if game.AppID == "" {
			continue
		}
		if prev, ok := present[game.AppID]; ok {
			if prev.Name == "" {
				prev.Name = game.Name
			}
			if !game.RunningSince.IsZero() && (prev.RunningSince.IsZero() || game.RunningSince.Before(prev.RunningSince)) {
				prev.RunningSince = game.RunningSince
			}
			present[game.AppID] = prev
			continue
		}
		present[game.AppID] = game
	}
	for id, game := range present {
		if i, ok := open[id]; ok {
			t.sessions[i].LastSeenAt = at
			if t.sessions[i].Name == "" && game.Name != "" {
				t.sessions[i].Name = game.Name
			}
			if mergeDevices(&t.sessions[i], sample.Devices) {
				changed = true
			}
			delete(open, id)
			continue
		}
		start := at
		if since := game.RunningSince.UTC().Truncate(time.Second); !since.IsZero() && !since.After(at) {
			last := t.lastEnd(id)
			if last.IsZero() || !since.Before(last) {
				start = since
			}
		}
		ident, err := domain.NewIDAt(start)
		if err != nil {
			return changed, err
		}
		session := Session{
			ID:         ident,
			AppID:      id,
			Name:       game.Name,
			StartedAt:  start,
			LastSeenAt: at,
		}
		mergeDevices(&session, sample.Devices)
		t.sessions = append(t.sessions, session)
		changed = true
	}
	for _, i := range open {
		t.sessions[i].EndedAt = at
		t.sessions[i].LastSeenAt = at
		changed = true
	}
	t.at = at
	t.awake = sample.Awake
	t.biased = sample.Biased
	t.seen = true
	return changed, nil
}

// deviceOrder 是会话存储手柄类型时的顺序。
var deviceOrder = []string{"dualsense", "switch-pro", "xbox"}

func mergeDevices(session *Session, extra []string) bool {
	have := splitDevices(session.Devices)
	changed := false
	for _, name := range extra {
		if name == "" || have[name] {
			continue
		}
		have[name] = true
		changed = true
	}
	if !changed {
		return false
	}
	session.Devices = joinDevices(have)
	return true
}

func splitDevices(text string) map[string]bool {
	have := map[string]bool{}
	for _, name := range strings.Split(text, ",") {
		if name != "" {
			have[name] = true
		}
	}
	return have
}

func joinDevices(have map[string]bool) string {
	parts := make([]string, 0, len(have))
	for _, name := range deviceOrder {
		if have[name] {
			parts = append(parts, name)
			delete(have, name)
		}
	}
	rest := make([]string, 0, len(have))
	for name := range have {
		rest = append(rest, name)
	}
	sort.Strings(rest)
	return strings.Join(append(parts, rest...), ",")
}

func stopAt(exit, sample time.Time) time.Time {
	exit = exit.UTC().Truncate(time.Second)
	if exit.IsZero() {
		return sample
	}
	return exit
}

func (t *Tracker) closeAll(at time.Time) bool {
	closed := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := at
		if end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		t.sessions[i].LastSeenAt = end
		closed = true
	}
	return closed
}

func (t *Tracker) closeApp(appID string, at time.Time) bool {
	if appID == "" {
		return false
	}
	for i := range t.sessions {
		if t.sessions[i].AppID != appID || !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := at
		if end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		t.sessions[i].LastSeenAt = end
		return true
	}
	return false
}

func (t *Tracker) lastEnd(appID string) time.Time {
	var last time.Time
	for _, session := range t.sessions {
		if session.AppID != appID || session.EndedAt.IsZero() {
			continue
		}
		if session.EndedAt.After(last) {
			last = session.EndedAt
		}
	}
	return last
}
