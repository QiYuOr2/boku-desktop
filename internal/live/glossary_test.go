package live

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
)

func TestMatchKeepsTheLongerOverlappingSource(t *testing.T) {
	entries := []Entry{{Source: "東京", Target: "东京"}, {Source: "東京大学", Target: "东京大学"}, {Source: "大学", Target: "大学"}}
	got := Match("東京大学に行く", entries)
	if len(got) != 1 || got[0].Source != "東京大学" {
		t.Fatalf("got %+v", got)
	}
}

func TestMatchKeepsAShorterWordThatDoesNotOverlap(t *testing.T) {
	entries := []Entry{{Source: "東京", Target: "东京"}, {Source: "大学", Target: "大学"}}
	got := Match("東京と大学", entries)
	if len(got) != 2 || got[0].Source != "東京" || got[1].Source != "大学" {
		t.Fatalf("got %+v", got)
	}
}

func TestMatchUsesFileOrderForEqualLengthsAndCapsAt32(t *testing.T) {
	entries := []Entry{{Source: "あい", Target: "1"}, {Source: "いう", Target: "2"}}
	got := Match("あいう", entries)
	if len(got) != 1 || got[0].Source != "あい" {
		t.Fatalf("equal %+v", got)
	}
	many := make([]Entry, 40)
	var sentence string
	for i := range many {
		source := string(rune('A'+i%26)) + string(rune('a'+i/26))
		many[i] = Entry{Source: source, Target: "x"}
		sentence += source + " "
	}
	got = Match(sentence, many)
	if len(got) != 32 || got[0].Source != many[0].Source || got[31].Source != many[31].Source {
		t.Fatalf("cap %d first %s", len(got), got[0].Source)
	}
}

func TestGlossaryRejectsDuplicatesAndABrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live_glossary.json")
	clock := testClock(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	glossary, err := LoadGlossary(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := glossary.Add("部室", "社团活动室"); err != nil {
		t.Fatal(err)
	}
	if err := glossary.Add(" 部室 ", "另一个"); err == nil {
		t.Fatal("duplicate source was saved")
	}
	if err := glossary.Add("  ", "空"); err == nil {
		t.Fatal("blank source was saved")
	}
	skipped, err := glossary.Import("# 注释\n\n部室 => 覆盖\n行く => 去\n坏行\n")
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Fatalf("skipped %d", skipped)
	}
	if len(glossary.Entries()) != 2 || glossary.Entries()[0].Target != "社团活动室" {
		t.Fatalf("entries %+v", glossary.Entries())
	}

	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken, err := LoadGlossary(path, clock)
	if err != nil || !broken.Broken() || len(broken.Entries()) != 0 {
		t.Fatalf("broken %+v err %v", broken, err)
	}
	before, _ := os.ReadFile(path)
	if err := broken.Add("部室", "社团活动室"); err == nil {
		t.Fatal("broken file was overwritten")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("file changed %q", after)
	}
}

func TestImportedExistingSourceDoesNotReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live_glossary.json")
	glossary, err := LoadGlossary(path, domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	if err := glossary.Add("部室", "社团活动室"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadGlossary(path, domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Entries()) != 1 || reloaded.Entries()[0].Target != "社团活动室" {
		t.Fatalf("%+v", reloaded.Entries())
	}
}

type testClock time.Time

func (c testClock) Now() time.Time { return time.Time(c) }
