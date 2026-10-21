package update

import (
	"strconv"
	"strings"
)

// Compare compares two version strings the way semantic versioning does and
// reports -1, 0 or 1.
//
// It exists so that "is the release newer than the running version" is
// answered by one rule and not by a string comparison. A feed may name the
// version of the build that is asking — the newest published release is
// exactly the version a current install runs — and comparing that with == or
// with a plain sort would either offer a reinstall of the same version or
// rank "v0.1.1" and "0.1.1" as different versions of themselves.
//
// The rules are semantic versioning's: a leading "v" and a "+build" suffix are
// not part of the number, fields compare numerically where both are numbers, a
// pre-release sorts before its release, and a version that stops early is
// smaller than one that continues it ("0.1" < "0.1.0").
//
// It is the comparison MyGo's own updater makes before it offers an update,
// and the adapter in main.go uses it for that reason: what this package calls
// newer and what the framework installs have to agree.
func Compare(a, b string) int {
	aRelease, aPrerelease := splitVersion(a)
	bRelease, bPrerelease := splitVersion(b)
	if c := compareFields(aRelease, bRelease); c != 0 {
		return c
	}
	return comparePrerelease(aPrerelease, bPrerelease)
}

// splitVersion reduces a version to the two parts that order it: the release
// number and the pre-release tag. A leading "v" is how tags are written and a
// "+build" suffix says how a build was made; neither changes which version it
// is.
func splitVersion(version string) (release, prerelease string) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version, _, _ = strings.Cut(version, "+")
	release, prerelease, _ = strings.Cut(version, "-")
	return release, prerelease
}

// comparePrerelease orders two pre-release tags. An absent one sorts after
// every present one, because a release is newer than its own pre-releases.
func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	return compareFields(a, b)
}

// compareFields compares two dot-separated version parts field by field.
// Numbers compare numerically; a numeric field sorts before a word; two words
// compare as strings; a field one side does not have makes that side smaller.
func compareFields(a, b string) int {
	af, bf := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(af) || i < len(bf); i++ {
		var x, y string
		if i < len(af) {
			x = af[i]
		}
		if i < len(bf) {
			y = bf[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case x == y:
			continue
		case x == "":
			return -1
		case y == "":
			return 1
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xerr == nil:
			return -1 // numbers sort before words
		case yerr == nil:
			return 1
		default:
			return strings.Compare(x, y)
		}
	}
	return 0
}
