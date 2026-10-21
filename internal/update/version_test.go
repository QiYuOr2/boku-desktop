package update

import "testing"

// Compare orders versions the way the feed and the running build have to be
// compared. The first case is the one that was broken: a build reading its
// own release back must be told they are the same version, not a newer one.
func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		// The same version, however it is spelled on each side.
		{"0.1.1", "0.1.1", 0},
		{"v0.1.1", "0.1.1", 0},
		{"0.1.1", "v0.1.1", 0},
		{"0.1.1+build.7", "0.1.1", 0},
		{"0.1.0-beta.5", "0.1.0-beta.5", 0},

		// A newer release.
		{"0.1.2", "0.1.1", 1},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		{"0.1.1", "0.1.0-beta.5", 1},

		// An older release, including one that is older because it is a
		// pre-release of the version being run.
		{"0.1.0", "0.1.1", -1},
		{"0.1.0-beta.5", "0.1.0", -1},
		{"0.1.0-beta.4", "0.1.0-beta.5", -1},
		{"0.1.0-alpha", "0.1.0-beta", -1},

		// A version that stops early is smaller than one that continues it.
		{"0.1", "0.1.0", -1},
		{"0.1.0", "0.1", 1},

		// Edges that must not panic or report nonsense.
		{"", "", 0},
		{"", "0.0.1", -1},
		{"0.0.1", "", 1},
		// "dev" is a word, and a word sorts after a number: a development
		// build is offered the feed's release rather than treated as newer.
		{"dev", "0.1.1", 1},
		{"0.1.1", "dev", -1},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		// The comparison is a total order in both directions.
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

// A build running a version ahead of the feed — a local build, or a release
// withdrawn — is up to date rather than offered a downgrade.
func TestCompareDoesNotOfferADowngrade(t *testing.T) {
	if got := Compare("0.1.1", "0.1.2"); got >= 0 {
		t.Errorf("Compare(0.1.1, 0.1.2) = %d, want the feed to be older", got)
	}
}
