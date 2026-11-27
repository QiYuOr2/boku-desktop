// Package logonce 抑制重复日志，避免每帧都记录同一条错误把文件写满。
//
// 判重只看消息内容而非时间：同一条记录无论间隔多短都算同一件事，只有直接重复才被丢弃；
// 同一个 key 下换成不同的错误视为新事实，照常写出。
//
// 单独成包而非放进 logging 包：logging 包只被 main.go 引用，其它层都走标准库 slog；
// 这里只依赖标准库，因此任何层都能用，而 logging 包自身不能用它。
package logonce

import "sync"

// seen 记录每个 key 上次写出的消息。key 是调用方给定的固定短串（调用点名称，而非错误），
// 因此同一调用点的不同失败会相互替换。
var (
	mu   sync.Mutex
	seen = map[string]string{}
)

// Should 返回 key 的消息是否与上次不同并记住它；可被多个 goroutine 并发调用。
//
// 同一 key 消息变了（失败变了）会重新写出，只丢弃完全相同的重复。
func Should(key, message string) bool {
	mu.Lock()
	defer mu.Unlock()
	if previous, ok := seen[key]; ok && previous == message {
		return false
	}
	seen[key] = message
	return true
}

// Forget 让 key 下次调用必定写出。用于调用方确认问题已解决（快捷键注册成功、
// 剪贴板写入成功）时，使之后再出现的同一失败重新上报，而不是被当成重复吞掉。
func Forget(key string) {
	mu.Lock()
	defer mu.Unlock()
	delete(seen, key)
}

// Reset 清空所有 key，仅供测试使用。
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	clear(seen)
}
