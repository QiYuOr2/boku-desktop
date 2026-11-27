package app

import "boku/internal/platform/gamepad"

// SetPlaytimeControllers 设置游戏时长标题要绘制的控制器列表。
// watcher 在主线程上以已拍好的快照调用它。
func (s *State) SetPlaytimeControllers(pads []gamepad.Pad) {
	if s == nil || s.playtimePanel == nil {
		return
	}
	s.playtimePanel.SetControllers(pads)
}

// CachePlaytimeCovers 解码磁盘上已有的封面文件。下载由录制器一侧完成；
// 这里只在浮窗打开或新文件保存后运行，且运行在主线程。
func (s *State) CachePlaytimeCovers() {
	if s.playtimePanel != nil {
		s.playtimePanel.Prepare(s.playtime)
	}
}
