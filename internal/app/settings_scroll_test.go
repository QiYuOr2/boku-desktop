package app

import "testing"

func TestSettingsSectionIndexFollowsThePage(t *testing.T) {
	sections := []settingsSectionBox{
		{kind: sectionAPIKey},
		{kind: sectionTranslate},
		{kind: sectionLive},
		{kind: sectionClipboard},
		{kind: sectionAbout},
	}
	index, ok := settingsSectionIndex(sectionLive, sections)
	if !ok || index != 2 || sections[index].kind != sectionLive {
		t.Fatalf("live index = %d ok %v", index, ok)
	}
	if ordinal := int(sectionLive); ordinal < len(sections) && sections[ordinal].kind == sectionLive {
		t.Fatal("sectionLive ordinal selects the live section; the lookup is no longer what keeps the scroll on the inserted section")
	}
}
