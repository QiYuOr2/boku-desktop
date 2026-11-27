package domain

import (
	"context"
	"errors"
)

// SelectionDevice 是平台自身对「用户选中了什么」的回答，从用户正在工作的
// 应用读取，而非从剪贴板。
//
// 剪贴板只保存上一次复制的内容，可能是完全无关的东西（刚才的密码、另一个
// 窗口的一行），所以读它只是在猜。选区不是猜：用户此刻正看着它，读取它不
// 干扰任何东西，而复制它会覆盖用户原有的剪贴板内容，且在某些平台上还需要
// 一次合成的按键。
//
// 并非每个平台、每个应用都公开其选区。没有相应 API 的系统、不实现该模式的
// 控件、以及空选区都是普通结果：表示调用方应回退到剪贴板，而非描述为失败，
// 由 ErrSelectionUnavailable 表达。
type SelectionDevice interface {
	// SelectedText 返回用户正在工作的应用中选中的文本，无选中时为空串。
	// 平台无法告知时返回 ErrSelectionUnavailable。
	//
	// 它可能在任意 goroutine 上运行；实现方负责平台要求的线程。
	SelectedText(ctx context.Context) (string, error)
}

// ErrSelectionUnavailable 表示平台无法说明选中了什么：没有此类 API、前台
// 应用不公开其选区、或没有选中任何内容。
var ErrSelectionUnavailable = errors.New("the platform has no selected text to give")

// SelectionAuthorizer 由能请求平台弹出权限提示的选区设备实现。macOS 是需
// 要它的平台：它的辅助功能列表只在该应用请求后才能识别它，且开关归系统而
// 非应用所有。无需权限的平台直接不实现它。
type SelectionAuthorizer interface {
	// RequestSelectionAccess 请求系统弹出权限提示，并报告权限是否已授予。
	// 提示后返回 false 表示用户尚未授予，而非请求失败。
	RequestSelectionAccess() bool
}

// SelectionAccessState 报告读取选区的平台权限是否已授予，且不主动请求。
// 设置页用它来在开关打开后不再显示请求。
type SelectionAccessState interface {
	SelectionAccessGranted() bool
}
