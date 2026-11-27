//go:build !windows

package gamepad

// 非 Windows 平台返回空：游玩时长标记只认识此处的 HID 枚举实现。
func Connected() []Pad {
	note(nil)
	return nil
}
