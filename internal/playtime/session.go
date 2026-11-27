// Package playtime 记录每个 Steam 游戏的运行时长。
//
// 会话（Session）是一次连续运行：开始时刻到最后进程退出的时刻。日/时/分
// 不落库，只是对会话的切片，所以图表可随时按任意粒度查询，不要求记录器
// 也按该频率采样。
package playtime

import "time"

// Session 是一次连续运行。
//
// EndedAt 为零表示游戏仍在运行。LastSeenAt 是崩溃后记录器敢认的截止点：
// 下次启动会把废弃会话收尾在此，而不是拉长到「现在」。
type Session struct {
	ID         string
	AppID      string
	Name       string
	StartedAt  time.Time
	EndedAt    time.Time
	LastSeenAt time.Time
	// Devices 列出运行期间连接过的控制器，空表示没有。中途拔掉的手柄也保留：
	// 记录的是「用过什么」，不是结束时的连接状态。
	Devices string
}

// Game 是采样器当前能看到的 Steam 游戏。
//
// RunningSince 是组成该游戏的进程的最早创建时间。Tracker 据此在迟发现游戏时
// 按真实创建时间起会话；若该时间落在已关闭的会话内则拒绝——崩溃空洞或睡眠
// 留下的空档保持为空。
type Game struct {
	AppID        string
	Name         string
	RunningSince time.Time
}

// Stop 表示游戏最后一个进程离开。At 是操作系统上报的真实退出时间；若记录器
// 只是下次查看时才发现进程消失，At 为零。
type Stop struct {
	AppID string
	At    time.Time
}

// Sample 是一次机器观察。
//
// Awake 与 Biased 同源，自开机起计数。Awake 在睡眠时暂停，Biased 不暂停。
// 两者都为零表示本系统无法提供，此时 tracker 不尝试剔除睡眠时间。
type Sample struct {
	At      time.Time
	Awake   time.Duration
	Biased  time.Duration
	Games   []Game
	Stopped []Stop
	// Devices 是本次观察连接着的控制器。tracker 会把看到的每种不同种类
	// 都记到未关闭的会话上。
	Devices []string
}
