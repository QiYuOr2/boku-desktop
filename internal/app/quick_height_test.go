package app

import (
	"testing"
)

// The flyout is one of two heights, and both respect the half-screen ceiling:
// the base height when nothing is being read, and a share of the work area
// when a reading block is on.
func TestQuickWindowHeightByReadingBlocks(t *testing.T) {
	base := panelHeight
	// On a tall display the share decides.
	const tall = 1600
	if got, want := quickWindowHeightForContent(0, true, tall), int(float64(tall)*quickEnabledHeightShare); got != want {
		t.Errorf("a reading block on a %d work area = %d, want %d", tall, got, want)
	}
	if got := quickWindowHeightForContent(0, true, tall); got <= base {
		t.Errorf("a reading block must open taller than the base height: %d <= %d", got, base)
	}
	// On a display whose 30% is below the base height the share is a floor,
	// not a target: the flyout never gets smaller for having a block on.
	for _, workHeight := range []int{1040, 1080, 1200, 1400} {
		got := quickWindowHeightForContent(0, true, workHeight)
		want := max(base, int(float64(workHeight)*quickEnabledHeightShare))
		if got != want {
			t.Errorf("a reading block on a %d work area = %d, want %d", workHeight, got, want)
		}
		if got < base {
			t.Errorf("work area %d: a reading block made the flyout shorter than %d", workHeight, base)
		}
	}
	if got := quickWindowHeightForContent(0, false, tall); got != base {
		t.Errorf("nothing switched on = %d, want %d", got, base)
	}
}

// The height never depends on how much is in the blocks, so it cannot change
// while the user scrolls. This is the property the previous attempt broke.
func TestQuickWindowHeightDoesNotFollowContent(t *testing.T) {
	const workHeight = 1080
	first := quickWindowHeightForContent(0, true, workHeight)
	for _, tasks := range []int{0, 1, 2} {
		got := quickWindowHeightForContent(tasks, true, workHeight)
		if got < first {
			t.Errorf("with %d tasks the flyout = %d, want at least %d", tasks, got, first)
		}
	}
	// Repeating the same question must give the same answer: nothing here
	// reads a measurement that a scroll could move.
	for i := 0; i < 3; i++ {
		if got := quickWindowHeightForContent(0, true, workHeight); got != first {
			t.Errorf("call %d = %d, want %d", i, got, first)
		}
	}
}

// A flyout never occupies more than half the work area, on any display.
func TestQuickWindowHeightNeverExceedsHalfTheWorkArea(t *testing.T) {
	for _, workHeight := range []int{400, 600, 900, 1080, 1440} {
		for _, reading := range []bool{false, true} {
			got := quickWindowHeightForContent(8, reading, workHeight)
			if got > workHeight/2 {
				t.Errorf("work area %d with reading=%v: %d exceeds half (%d)",
					workHeight, reading, got, workHeight/2)
			}
		}
	}
}

// Image tasks still grow the flyout when no reading block is on.
func TestQuickWindowHeightGrowsWithImageTasks(t *testing.T) {
	const workHeight = 1080
	first := quickWindowHeightForContent(0, false, workHeight)
	second := quickWindowHeightForContent(2, false, workHeight)
	if second != first+2*quickTaskRowHeight {
		t.Errorf("two tasks = %d, want %d", second, first+2*quickTaskRowHeight)
	}
	// A task never makes the flyout shorter.
	if quickWindowHeightForContent(1, true, workHeight) < quickWindowHeightForContent(0, true, workHeight) {
		t.Error("a task shortened the flyout")
	}
}

// A reader who asked for the animation block or the followed UPs gets the
// taller flyout; one who asked for neither gets the base height.
func TestReadingBlocksOn(t *testing.T) {
	if (&State{}).readingBlocksOn() {
		t.Error("no services at all reported a reading block")
	}
	state := &State{bilibiliService: testBilibili{}}
	if !state.readingBlocksOn() {
		t.Error("a followed UP did not count as a reading block")
	}
	state = &State{bilibiliService: testBilibili{noUIDs: true}}
	if state.readingBlocksOn() {
		t.Error("a block with no UIDs counted as a reading block")
	}
}
