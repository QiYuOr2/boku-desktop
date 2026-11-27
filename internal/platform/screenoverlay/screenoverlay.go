// Package screenoverlay 为框选遮罩设置 MyGo 窗口选项无法表达的本地窗口属性。
//
// 遮罩必须覆盖整个屏幕，包括菜单栏和 Dock。macOS 下带标题栏的窗口做不到：
// 标题栏会跑到屏幕外，AppKit 显示窗口时把 frame 下移菜单栏的高度，而 MyGo
// 的悬浮层级又低于菜单栏和 Dock，会被持续覆盖。遮罩因此改为无边框并置于
// 屏幕保护程序层级。同时关闭窗口动画：NSPanel 关闭后会淡出，紧接着的选区
// 像素读取会拍到正在淡出的遮罩而非桌面。MyGo 不暴露这些能力，本包通过 MyGo
// 暴露的窗口句柄直接操作原生窗口。
package screenoverlay

// Cover 让一个创建后隐藏、尚未定位的窗口覆盖整个屏幕：去掉标题栏、抬高到
// 菜单栏与 Dock 之上，并关闭开/关动画，使窗口一关闭即消失。必须在给窗口设置
// 边界并显示之前调用，否则 AppKit 已按标题栏约束了 frame。
//
// 返回平台修改窗口失败的错误；窗口选项已足够的平台直接返回 nil。
func Cover(window uintptr) error { return cover(window) }
