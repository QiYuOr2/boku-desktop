package ui

import (
	"testing"

	"boku/internal/domain"
)

func TestGroupAndEpisodeSummary(t *testing.T) {
	items := []domain.EpisodeRelease{
		{SubjectID: 1, SubjectName: "A", EpisodeSort: 1, EpisodeName: "first"},
		{SubjectID: 1, SubjectName: "A", EpisodeSort: 2},
		{SubjectID: 2, SubjectName: "B", EpisodeSort: 1},
	}
	updates := Group(items)
	if len(updates) != 2 || len(updates[0].Episodes) != 2 {
		t.Fatalf("grouped updates = %#v", updates)
	}
	if got, want := EpisodeSummary(updates[0].Episodes), "第 1 集 · first、第 2 集"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
