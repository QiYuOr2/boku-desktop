package domain

import (
	"testing"
	"time"
)

// A kind is written to the history and read back from it, so Valid is what
// keeps a row from claiming a shape the application cannot draw.
func TestClipKindValid(t *testing.T) {
	for _, kind := range []ClipKind{
		ClipKindText, ClipKindURL, ClipKindJSON,
		ClipKindCode, ClipKindFile, ClipKindImage,
	} {
		if !kind.Valid() {
			t.Errorf("%q is not valid", kind)
		}
	}
	for _, kind := range []ClipKind{"", "html", "TEXT"} {
		if kind.Valid() {
			t.Errorf("%q should not be valid", kind)
		}
	}
}

// The record range is one of three, and anything else — a file that predates
// the setting, or a number a person edited in — is the default rather than a
// reason not to start.
func TestToolSettingsNormalizesTheRecordRange(t *testing.T) {
	for _, size := range ClipboardHistorySizes() {
		if !ValidClipboardHistorySize(size) {
			t.Errorf("%d is one of the record ranges but is not valid", size)
		}
		settings := ToolSettings{ClipboardHistorySize: size}
		if got := settings.Normalize().ClipboardHistorySize; got != size {
			t.Errorf("Normalize kept %d as %d, want %d", size, got, size)
		}
		if got := settings.HistorySize(); got != size {
			t.Errorf("HistorySize of %d = %d", size, got)
		}
	}
	for _, size := range []int{0, -1, 7, 11, 100, 1000} {
		if ValidClipboardHistorySize(size) {
			t.Errorf("%d should not be a record range", size)
		}
		settings := ToolSettings{ClipboardHistorySize: size}
		if got := settings.Normalize().HistorySize(); got != ClipboardDefaultHistorySize {
			t.Errorf("Normalize of the record range %d = %d, want the default %d",
				size, got, ClipboardDefaultHistorySize)
		}
		if got := settings.HistorySize(); got != ClipboardDefaultHistorySize {
			t.Errorf("HistorySize of %d = %d, want the default %d", size, got, ClipboardDefaultHistorySize)
		}
	}

	// Normalizing the range does not disturb the interval, which has its own
	// rule.
	settings := ToolSettings{ClipboardPollInterval: time.Second, ClipboardHistorySize: 7}.Normalize()
	if settings.Interval() != time.Second {
		t.Errorf("Normalize changed the interval to %s", settings.Interval())
	}
}

// A panel nobody has chosen about is on: the switches in the settings window
// are the only thing that turns one off, so a file that does not name a panel
// — every file written before the switch existed — reads as "on".
func TestAPanelNobodyChoseAboutIsOn(t *testing.T) {
	var none ToolSettings
	if !none.PanelOn("panel-1") {
		t.Error("a panel is off in settings that say nothing about it")
	}
	chosen := ToolSettings{Panels: map[string]bool{"panel-1": false, "panel-2": true}}
	if chosen.PanelOn("panel-1") {
		t.Error("a panel the file turns off reports itself on")
	}
	if !chosen.PanelOn("panel-2") {
		t.Error("a panel the file turns on reports itself off")
	}
	if !chosen.PanelOn("panel-3") {
		t.Error("a panel the file does not name is off")
	}
	if chosen.PanelOn(" panel-1 ") {
		t.Error("an id with room around it did not find the choice that was stored")
	}
}
