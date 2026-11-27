// Package proc 为游玩时长记录器列举进程。
//
// 本包不存放统计数据，只向操作系统询问谁在运行、进程创建时间，以及在
// Windows 上进程何时退出。其它系统无实现：记录器保持安静，不虚构会话。
package proc

import (
	"errors"
	"time"
)

// ErrUnsupported 在操作系统无法提供记录器所需的快照时返回。
var ErrUnsupported = errors.New("process snapshots are only available on windows")

// Process 是一个运行中的进程。进程无法打开时 Exe 为空；Name 仍是进程列表里的文件名。
type Process struct {
	PID       int
	PPID      int
	Name      string
	Exe       string
	CreatedAt time.Time
}

// Clocks 是开机以来的时间。Awake 不含休眠，Biased 包含。
// 记录器用两者之差在机器休眠时切断会话，而不是把合盖的小时也算进去。
type Clocks struct {
	Awake  time.Duration
	Biased time.Duration
}

// Exit 是被监视进程退出的时刻，按墙上时间表示。
type Exit struct {
	PID       int
	CreatedAt time.Time
	At        time.Time
}
