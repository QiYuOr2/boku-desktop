package domain

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUpdateStatusNames(t *testing.T) {
	cases := []struct {
		status UpdateStatus
		name   string
	}{
		{UpdateIdle, "idle"},
		{UpdateChecking, "checking"},
		{UpdateUpToDate, "up_to_date"},
		{UpdateDownloading, "downloading"},
		{UpdateReady, "ready"},
		{UpdateUnsupported, "unsupported"},
		{UpdateFailed, "failed"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.name {
			t.Errorf("UpdateStatus(%d).String() = %q, want %q", int(c.status), got, c.name)
		}
		if !c.status.Valid() {
			t.Errorf("%s.Valid() = false", c.name)
		}
	}
	// The zero value is the one a view sees before anything has happened,
	// and it must be Idle rather than the first of the interesting states.
	var zero UpdateState
	if zero.Status != UpdateIdle {
		t.Errorf("the zero state is %s, want idle", zero.Status)
	}
	unknown := UpdateStatus(99)
	if unknown.Valid() {
		t.Error("an unknown status is Valid")
	}
	if got := unknown.String(); got != "update_status(99)" {
		t.Errorf("unknown status String() = %q", got)
	}
}

func TestUpdateStateSettled(t *testing.T) {
	settled := map[UpdateStatus]bool{
		UpdateIdle:        true,
		UpdateChecking:    false,
		UpdateUpToDate:    true,
		UpdateDownloading: false,
		UpdateReady:       true,
		UpdateUnsupported: true,
		UpdateFailed:      true,
	}
	for status, want := range settled {
		if got := (UpdateState{Status: status}).Settled(); got != want {
			t.Errorf("%s.Settled() = %v, want %v", status, got, want)
		}
	}
}

func TestUpdateStatePercent(t *testing.T) {
	cases := []struct {
		name       string
		downloaded int64
		total      int64
		want       int
		wantOK     bool
	}{
		{"nothing fetched", 0, 100, 0, true},
		{"half", 50, 100, 50, true},
		{"rounds down", 66, 100, 66, true},
		{"nearly there", 99, 100, 99, true},
		{"complete", 100, 100, 100, true},
		{"more than promised is still complete", 120, 100, 100, true},
		{"unknown total", 10, 0, 0, false},
		{"negative total", 10, -1, 0, false},
		{"negative download", -1, 100, 0, false},
	}
	for _, c := range cases {
		got, ok := (UpdateState{Downloaded: c.downloaded, Total: c.total}).Percent()
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: Percent() = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestUpdateReasonIsTrimmedToSnippet(t *testing.T) {
	if got := UpdateReason("  the manifest is empty\n"); got != "the manifest is empty" {
		t.Errorf("UpdateReason kept the whitespace: %q", got)
	}
	long := strings.Repeat("unexpected end of JSON input; ", 4*MaxSnippetBytes/29+1)
	got := UpdateReason(long)
	if len(got) > MaxSnippetBytes {
		t.Errorf("UpdateReason returned %d bytes, want at most %d", len(got), MaxSnippetBytes)
	}
	// A cut through a multi-byte character must not leave half of it: the
	// reason goes to a log, and half a character is not text.
	wide := strings.Repeat("é", MaxSnippetBytes)
	trimmed := UpdateReason(wide)
	if len(trimmed) > MaxSnippetBytes {
		t.Errorf("UpdateReason returned %d bytes of wide text, want at most %d", len(trimmed), MaxSnippetBytes)
	}
	if !utf8.ValidString(trimmed) {
		t.Error("a reason cut through a character is not valid UTF-8")
	}
	if strings.ContainsRune(trimmed, utf8.RuneError) {
		t.Error("a reason cut through a character carries a replacement character")
	}
}

func TestUpdateErrorsAreDistinct(t *testing.T) {
	if errors.Is(ErrUpdateUnsupported, ErrUpdateUnavailable) ||
		errors.Is(ErrUpdateUnavailable, ErrUpdateUnsupported) {
		t.Error("the two update errors are the same error")
	}
	if ErrUpdateUnsupported.Error() == "" || ErrUpdateUnavailable.Error() == "" {
		t.Error("an update error says nothing")
	}
	// A release that could not be read is a bad response, not a machine
	// that cannot update: the caller can tell them apart.
	bad := &BadResponseError{ProviderID: "mygo", Reason: "the manifest is empty"}
	if !errors.Is(bad, ErrProviderBadResponse) {
		t.Error("BadResponseError does not unwrap to ErrProviderBadResponse")
	}
	if errors.Is(bad, ErrUpdateUnavailable) || errors.Is(bad, ErrUpdateUnsupported) {
		t.Error("a bad response is also an update error")
	}
}
