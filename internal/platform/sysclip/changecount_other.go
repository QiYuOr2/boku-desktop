//go:build !windows && !darwin

package sysclip

// clipboardChangeCount 返回 0：本构建没有剪贴板写计数器，
// 图片缓存退化为依赖时间窗口判断。
func clipboardChangeCount() uint32 { return 0 }
