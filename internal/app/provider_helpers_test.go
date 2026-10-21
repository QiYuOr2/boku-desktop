package app

import contractfree "boku/internal/contract/freetranslate"

// customTranslatorID is the translator whose settings the user fills in
// themselves. It is named here as well as in the storage package because the
// window has two things about it that no other translator has: an address
// and a name.
const customTranslatorID = "custom"

// freeTranslatorIDs are the free engines: no key, nothing to configure, and
// what a fresh installation starts on. They are named here for the reason the
// storage package names them — the settings file holds the same strings, and
// one copy of a value that must match is one chance to get it wrong.
const (
	freeMicrosoftID = contractfree.MicrosoftEdge
	freeBilibiliID  = contractfree.Bilibili
)

// isFreeTranslator reports whether a translator is one of the free engines,
// which is what tells the picker to draw it in the free group and the API Key
// section to leave it out.
func isFreeTranslator(id string) bool {
	return id == freeMicrosoftID || id == freeBilibiliID
}
