package panel

import "testing"

// A panel nobody has chosen about is on: the switches in the settings window
// are the only thing that turns one off, so a file that does not name a panel
// — every file written before the switch existed — reads as "on".
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

// The choices come back copied: a caller that wrote into the map it was handed
// would be changing the settings without going through Update.
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
