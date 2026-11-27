//go:build !windows && !darwin

package selection

import (
	"context"

	"boku/internal/domain"
)

// New 返回没有选中 API 的平台所用的设备。Linux 的答案在并不总在运行的辅助功能总线上，
// 因此回退到剪贴板，也就是本包存在之前工具的做法。
//
// options 里的剪贴板在这里不使用：复制方案需要平台按键和剪贴板写计数器，本构建两者都没有。
func New(Options) domain.SelectionDevice { return unsupported{} }

// unsupported 是诚实的答案：此平台无法回答。
type unsupported struct{}

func (unsupported) SelectedText(context.Context) (string, error) {
	return "", domain.ErrSelectionUnavailable
}
