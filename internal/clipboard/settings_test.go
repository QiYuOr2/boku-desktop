package clipboard

import (
	"testing"
	"time"
)

// The record range is one of three, and anything else — a file that predates
// the setting, or a number a person edited in — is the default rather than a
// reason not to start.
func TestTheRecordRangeIsBroughtIntoItsBounds(t *testing.T) {
	for _, size := range HistorySizes() {
		if !ValidHistorySize(size) {
			t.Errorf("%d is one of the record ranges but is not valid", size)
		}
		settings := Settings{HistorySize: size}.Normalize()
		if got := settings.HistorySize; got != size {
			t.Errorf("Normalize kept %d as %d, want %d", size, got, size)
		}
		if got := settings.KeepsHistory(); got != size {
			t.Errorf("KeepsHistory of %d = %d", size, got)
		}
	}
	for _, size := range []int{0, -1, 7, 11, 100, 1000} {
		if ValidHistorySize(size) {
			t.Errorf("%d should not be a record range", size)
		}
		settings := Settings{HistorySize: size}.Normalize()
		if got := settings.KeepsHistory(); got != DefaultHistorySize {
			t.Errorf("the record range %d became %d, want the default %d",
				size, got, DefaultHistorySize)
		}
	}

	// Normalizing the range does not disturb the interval, which has its own
	// rule.
	settings := Settings{PollInterval: time.Second, HistorySize: 7}.Normalize()
	if settings.Interval() != time.Second {
		t.Errorf("Normalize changed the interval to %s", settings.Interval())
	}
}

// The poll interval has a floor: a file that asks for a poll faster than the
// clipboard can be read would spend the machine's time asking.
func TestThePollIntervalHasAFloor(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, time.Millisecond, MinPollInterval - 1} {
		settings := Settings{PollInterval: interval}.Normalize()
		if got := settings.Interval(); got != DefaultPollInterval {
			t.Errorf("an interval of %s became %s, want the default %s", interval, got, DefaultPollInterval)
		}
	}
	settings := Settings{PollInterval: 3 * time.Second}.Normalize()
	if got := settings.Interval(); got != 3*time.Second {
		t.Errorf("an interval inside the bounds became %s", got)
	}
}

// Reads is what decides whether the clipboard is read at all: a copy is read
// when the region is drawn and the content it would draw is being collected,
// and either switch off means no read.
func TestReadsNeedsBothSwitches(t *testing.T) {
	cases := []struct {
		region, monitor bool
		want            bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{true, true, true},
	}
	for _, c := range cases {
		settings := Settings{RegionVisible: c.region, Monitoring: c.monitor}
		if got := settings.Reads(); got != c.want {
			t.Errorf("Reads with region=%v monitoring=%v = %v, want %v",
				c.region, c.monitor, got, c.want)
		}
	}
}

// A fresh installation reads nothing: the clipboard is the user's, and being
// asked before it is read is the whole point of the switch.
func TestAFreshInstallationReadsNothing(t *testing.T) {
	settings := Settings{}.Normalize()
	if settings.Reads() {
		t.Error("a fresh installation starts reading the clipboard")
	}
	if settings.Monitoring {
		t.Error("a fresh installation starts monitoring")
	}
}
