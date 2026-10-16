// Package freetranslate holds the identity of the free translation engines:
// the ids they are registered under, the credential key their descriptors
// carry, and the documentation link.
//
// It exists for one reason: the ids are read from two features that must agree
// — the translation backends (internal/translate/free) and the settings window
// (internal/app, which draws them in the translation section) — and two copies
// of a string that must match is two chances to get it wrong.
//
// The engines are the same shape as internal/panel/<vendor>: their identity
// belongs in a contract package, and their code belongs in the backend that
// asks them.
//
// Nothing here does anything.
package freetranslate

// The free engines, as the settings file names them.
//
// Each one is a translator of its own and a choice of its own: the user picks
// the engine they want from the translation section, and that engine is the one
// asked. There is no pool and no fallback — an engine that fails reports the
// failure, and the user changes engines if they want another one. Two options
// in the interface means two answers, not one answer with a preference.
const (
	// MicrosoftEdge is the endpoint Edge's own translate feature asks. It is
	// the wider, steadier of the two (§3.1 of the free-translate research) and the one
	// that answers faster.
	MicrosoftEdge = "free.microsoft"
	// Bilibili is B 站's Index-Translate model on its free public endpoint. It
	// is the one that can annotate (§3.2 of the free-translate research), which is what
	// the Japanese settings need.
	Bilibili = "free.bilibili"
)

// APIKeyCredential is the placeholder the descriptors carry.
//
// Neither engine needs a credential: AllowEmptyCredential is what says so, and
// a missing key is not a missing configuration. The name is here because
// TranslatorDescriptor.Validate refuses a translator that asks for none.
const APIKeyCredential = "apiKey"

// DocsURL is where the free engines are written down. It is not a page that
// hands out a key — there is none — but the descriptor requires one, and the
// user is owed a page that says which endpoints their text is sent to.
const DocsURL = "https://github.com/bilibili/Index-Translate"
