package panel

import "testing"

// 无人做出选择的面板是开启的：设置窗口的开关是唯一能关闭它的东西，因此文件未提及
// 某个面板（包括所有在开关存在之前写入的文件）都读作"开启"。
func TestAPanelNobodyChoseAboutIsOn(t *testing.T) {
	none := NewSection()
	if !none.On("panel-1") {
		t.Error("a panel is off in settings that say nothing about it")
	}

	chosen := NewSection()
	chosen.Set("panel-1", false)
	chosen.Set("panel-2", true)
	if chosen.On("panel-1") {
		t.Error("a panel the file turns off reports itself on")
	}
	if !chosen.On("panel-2") {
		t.Error("a panel the file turns on reports itself off")
	}
	if !chosen.On("panel-3") {
		t.Error("a panel the file does not name is off")
	}
	if chosen.On(" panel-1 ") {
		t.Error("an id with room around it did not find the choice that was stored")
	}
}

// 交出的选择是副本：调用方若写入拿到的 map，就绕过了 Update 修改设置。
func TestTheChoicesHandedOutAreACopy(t *testing.T) {
	section := NewSection()
	section.Set("panel-1", false)
	choices := section.Choices()
	choices["panel-1"] = true
	choices["panel-2"] = false
	if section.On("panel-1") {
		t.Error("writing into the map handed out changed the settings")
	}
	if _, present := section.Choices()["panel-2"]; present {
		t.Error("adding to the map handed out changed the settings")
	}
}
