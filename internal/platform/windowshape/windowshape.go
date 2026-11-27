// Package windowshape 把窗口裁剪到顶部矩形区域。
//
// 实时字幕打开期间保持固定的窗口高度，卡片在该窗口内向下缓动而不是改变
// 窗口大小。卡片下方的透明区域否则会截走本该落到后面窗口的点击。MyGo 无法
// 只把窗口裁剪到框架的一部分，因此本包通过 MyGo 暴露的句柄直接操作原生窗口。
package windowshape

// ClipTop 将窗口限制在顶部 dip 个设备无关像素内，dip 为卡片高度，返回
// 裁剪是否已生效。dip 非正或窗口尚未上屏时返回 false，调用方可稍后重试；
// dip 覆盖整个窗口时清除裁剪；无法裁剪部分窗口的平台返回 true 且不做任何事。
func ClipTop(window uintptr, dip int) bool { return clipTop(window, dip) }
