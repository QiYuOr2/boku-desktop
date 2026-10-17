package translate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// Translate is the selection-translation tool: it reads what the user
// selected, asks the configured translator for the answer, and keeps the
// switches that decide all of it.
//
// It is the only place the tool's rules live: which translator is used, what
// happens when nothing is configured, which language a text is translated
// into, and what the user's own service was configured with. Every method is
// safe to call from any goroutine.
type Translate struct {
	translators domain.TranslatorRegistry
	secrets     domain.SecretStore
	section     *Section
	// saver is the store the section is saved through, or nil when the
	// settings are kept in memory only.
	saver     StoreSettings
	clipboard domain.ClipboardDevice
	selection domain.SelectionDevice
	now       func() time.Time
}

// TranslateOptions is what the service is assembled from. Everything is
// required except the two devices that read what the user chose: a
// translation that is handed its text needs neither, and a build with neither
// answers that there is nothing to translate.
type TranslateOptions struct {
	Translators domain.TranslatorRegistry
	Secrets     domain.SecretStore
	// Section is this feature's settings, as the store holds them: it is read
	// from at assembly and written through when a switch changes. A nil
	// section keeps the settings in memory, which is what a test wants and
	// what an assembly without a settings file must still survive.
	Section *Section
	// Settings is the store the section is saved through. A nil store keeps
	// the settings in memory only.
	Settings StoreSettings
	// Selection reads what the user has selected in another application,
	// when the platform can say. It is asked first: it is about what the
	// user is looking at, where the clipboard is about what they copied
	// last.
	Selection domain.SelectionDevice
	// Clipboard reads what is on the clipboard when the caller did not
	// bring text of its own and the platform cannot say what is selected.
	Clipboard domain.ClipboardDevice
	Clock     domain.Clock
}

// NewTranslate returns the service.
func NewTranslate(opts TranslateOptions) (*Translate, error) {
	if opts.Translators == nil {
		return nil, fmt.Errorf("%w: no translator registry", domain.ErrInvalidArgument)
	}
	if opts.Secrets == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	clock := opts.Clock
	if clock == nil {
		clock = domain.SystemClock{}
	}
	section := opts.Section
	if section == nil {
		section = NewSection()
	}
	service := &Translate{
		translators: opts.Translators,
		secrets:     opts.Secrets,
		section:     section,
		saver:       opts.Settings,
		clipboard:   opts.Clipboard,
		selection:   opts.Selection,
		now:         clock.Now,
	}
	return service, nil
}

// Request is one translation the interface asks for.
type Request struct {
	// Text is what to translate, and it is the text itself: the request
	// carries no hint to go and find one. What the user has in front of
	// them is read by Source, before the request is made — reading it means
	// asking the application the user is working in, and the card that
	// shows the answer takes that application's keyboard away.
	Text string
	// Target overrides the language the text would be translated into. It
	// is empty in practice; the tool decides from the source text.
	Target domain.Language
	// Annotate asks for the words of the text and how they are read, and for
	// no translation at all.
	//
	// It is the card's second request: the translation goes out with no
	// annotation to wait for, and the words behind it are asked for when the
	// user opens them. Both requests are the same call to the translator —
	// the annotation is what the prompt asks for instead of the paragraph —
	// so what tells them apart is this.
	Annotate bool
}

// Result is one answer, in the shape the window draws.
type Result struct {
	SourceText     string
	TranslatedText string
	// Segments is the piece-by-piece annotation, when the translator gave
	// one. Empty means the source is drawn as one line.
	Segments   []domain.Segment
	SourceLang domain.Language
	TargetLang domain.Language
	// ProviderID and ProviderName are what the window's footer says the
	// translation came from.
	ProviderID   string
	ProviderName string
	// Note is what the translator noticed: text it had to shorten, pieces
	// it could not annotate.
	Note string
	At   time.Time
}

// TranslatorView is one translator as the settings window shows it.
type TranslatorView struct {
	ID          string
	DisplayName string
	// DocsURL is where the user gets a credential.
	DocsURL string
	// Endpoint is the address as the settings stand now, or empty when the
	// user has not named one yet.
	Endpoint string
	// CredentialKey is the credential the translator asks for, without the
	// provider prefix.
	CredentialKey string
	// NeedsCredential is false for a translator that works without a key.
	NeedsCredential bool
	// Configured is whether what the translator needs is there now.
	Configured bool
}

// Settings returns the translation configuration as it stands.
func (t *Translate) Settings() (Settings, error) {
	return t.section.Current(), nil
}

// SaveSettings changes the configuration through a function, so that every
// switch is written the same way: change one thing in this feature's settings,
// and let the store put it back with every other feature's.
//
// It does not write the whole settings value. The file holds every feature's
// settings, and a value read here and written back would put back whatever
// another feature changed in between: flipping a translation switch would undo
// a clipboard switch flipped while this one was reading. The store's Update
// reads the file again, applies this feature's change and writes as one step,
// so what lands is the translation setting and nothing else.
//
// A switch that cannot be saved is reported, and the settings the application
// runs on are the ones on disk rather than the ones that were meant to be
// written.
func (t *Translate) SaveSettings(ctx context.Context, change func(*Settings)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.saver == nil {
		return nil // kept in memory only, which is what a test wants
	}
	return t.saver.Update(ctx, sectionName, func() error {
		settings := t.section.Current()
		change(&settings)
		t.section.Set(settings)
		return nil
	})
}

// SetEnabled turns the tool on or off.
func (t *Translate) SetEnabled(ctx context.Context, enabled bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Enabled = enabled })
}

// SetAccelerator remembers the shortcut that starts a translation, so that
// the next launch registers the same one.
//
// It does not judge the combination: whether the platform will take it is the
// platform's answer, and the window that recorded it has already had that
// answer. What is stored is what was bound.
func (t *Translate) SetAccelerator(ctx context.Context, accelerator string) error {
	trimmed := strings.TrimSpace(accelerator)
	if trimmed == "" {
		return fmt.Errorf("%w: the shortcut is empty", domain.ErrInvalidArgument)
	}
	return t.SaveSettings(ctx, func(s *Settings) { s.Accelerator = trimmed })
}

// SetProvider chooses the translator to use. An id no translator answers to
// is refused here rather than stored: the settings window offers only the
// ones that exist, and a file that names another was edited by hand.
func (t *Translate) SetProvider(ctx context.Context, id string) error {
	if _, err := t.translators.Translator(id); err != nil {
		return err
	}
	return t.SaveSettings(ctx, func(s *Settings) { s.Provider = id })
}

// SetFurigana turns the reading of Japanese kanji on or off.
func (t *Translate) SetFurigana(ctx context.Context, on bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Furigana = on })
}

// SetLoanwordGloss turns the English original of a katakana loanword on or
// off.
func (t *Translate) SetLoanwordGloss(ctx context.Context, on bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.LoanwordGloss = on })
}

// SetPinned decides whether the window starts pinned.
func (t *Translate) SetPinned(ctx context.Context, pinned bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Pinned = pinned })
}

// SetCloseDelay decides how long the window waits before closing itself once
// it loses the keyboard.
func (t *Translate) SetCloseDelay(ctx context.Context, delay time.Duration) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.CloseDelay = delay })
}

// AddCustomTranslator records a service of the user's own and returns the id it
// was stored under, which is the translator id that now asks it.
//
// The id is chosen here rather than by the caller, because it is this feature's
// storage decision: the first service takes "custom", the ones after it are
// numbered, and a file written before there could be several services keeps
// reading as the first of them.
//
// The address is cleaned the way this feature cleans it, so that what is stored
// is what will be asked. A service with no address is refused rather than
// stored: there would be nothing to ask and nothing to show.
func (t *Translate) AddCustomTranslator(ctx context.Context, settings CustomSettings) (string, error) {
	settings = settings.Normalize()
	if settings.BaseURL == "" {
		return "", fmt.Errorf("%w: 自定义服务至少需要一个 Base URL", domain.ErrInvalidArgument)
	}
	id := ""
	err := t.SaveSettings(ctx, func(s *Settings) {
		if s.Custom == nil {
			s.Custom = map[string]CustomSettings{}
		}
		id = s.NextCustomServiceID()
		s.Custom[id] = settings
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// SetCustomTranslator records where one of the user's own services is and what
// to call it, leaving the others where they are.
func (t *Translate) SetCustomTranslator(ctx context.Context, id string, settings CustomSettings) error {
	settings = settings.Normalize()
	if !IsCustomService(id) {
		return fmt.Errorf("%w: %s is not one of the user's own services", domain.ErrInvalidArgument, id)
	}
	return t.SaveSettings(ctx, func(s *Settings) {
		if s.Custom == nil {
			s.Custom = map[string]CustomSettings{}
		}
		s.Custom[id] = settings
	})
}

// RemoveCustomTranslator forgets one of the user's own services.
//
// The translator setting is left alone: if the service being removed was the
// one in use, the id it names stops answering, and Translate says so. Moving
// the user to another service on their behalf would be changing a choice they
// made — the window asks them what to switch to instead.
func (t *Translate) RemoveCustomTranslator(ctx context.Context, id string) error {
	if !IsCustomService(id) {
		return fmt.Errorf("%w: %s is not one of the user's own services", domain.ErrInvalidArgument, id)
	}
	return t.SaveSettings(ctx, func(s *Settings) { delete(s.Custom, id) })
}

// Providers lists the translators the settings window offers, in the order
// it shows them, with what each one is configured with and whether it can be
// used right now.
func (t *Translate) Providers(ctx context.Context) ([]TranslatorView, error) {
	settings, err := t.Settings()
	if err != nil {
		return nil, err
	}
	ids := t.translators.TranslatorIDs()
	views := make([]TranslatorView, 0, len(ids))
	for _, id := range ids {
		reply, err := t.translators.Translator(id)
		if err != nil {
			continue
		}
		descriptor := reply.Descriptor()
		view := TranslatorView{
			ID:              descriptor.ID,
			DisplayName:     descriptor.DisplayName,
			DocsURL:         descriptor.DocsURL,
			Endpoint:        descriptor.Endpoint,
			NeedsCredential: !descriptor.AllowEmptyCredential && len(descriptor.CredentialKeys) > 0,
		}
		if len(descriptor.CredentialKeys) > 0 {
			view.CredentialKey = descriptor.CredentialKeys[0]
		}
		// A translator the user points at a service of their own is
		// configured when the address of that service is there; the address
		// lives in the settings rather than in a credential, and it is looked
		// up by the translator's own id so that each of the user's services
		// answers for itself.
		hasAddress := true
		if descriptor.BaseURLSetting != "" {
			service, _ := settings.CustomService(descriptor.ID)
			hasAddress = service.BaseURL != ""
		}
		view.Configured = hasAddress && (!view.NeedsCredential || t.credentialStored(ctx, descriptor))
		views = append(views, view)
	}
	// The order is the registry's own — the ids, sorted — and it is the same
	// order every time, whatever the user picks.
	//
	// It used to put the translator in use first, which made the list rearrange
	// itself under the pointer: clicking a row moved it, and the row that took
	// its place became the thing under the cursor. A list of choices must stay
	// where it is while the user is choosing.
	return views, nil
}

// credentialStored reports whether what a translator asked for is in the
// credential store. It never returns the value: a view only needs to know
// that one is there.
func (t *Translate) credentialStored(ctx context.Context, descriptor domain.TranslatorDescriptor) bool {
	for _, key := range descriptor.CredentialKeys {
		secret, err := t.secrets.Get(ctx, domain.CredentialKey(descriptor.ID, key))
		if err != nil || strings.TrimSpace(secret.Text()) == "" {
			return false
		}
	}
	return true
}

// Descriptor returns what one translator is called and where it asks, so the
// settings window can show it without knowing which translator it is.
func (t *Translate) Descriptor(id string) (domain.TranslatorDescriptor, error) {
	reply, err := t.translators.Translator(id)
	if err != nil {
		return domain.TranslatorDescriptor{}, err
	}
	return reply.Descriptor(), nil
}

// TestTranslator checks a configuration without storing it: it translates a
// short text and reports what happened. The settings window uses it to tell
// the user whether a key works before saving it.
//
// The key is taken from override when the user typed one, and from the
// credential store otherwise, so that "test what is stored" and "test what I
// just typed" are the same call.
func (t *Translate) TestTranslator(ctx context.Context, providerID string, override map[string]string) error {
	reply, err := t.translators.Translator(providerID)
	if err != nil {
		return err
	}
	descriptor := reply.Descriptor()
	// A translator the user points at a service of their own has nothing to
	// test until they name it. Saying so here, rather than letting the
	// translator answer, keeps the message the user reads the same for every
	// implementation.
	if descriptor.BaseURLSetting != "" {
		settings, err := t.Settings()
		if err != nil {
			return err
		}
		service, _ := settings.CustomService(descriptor.ID)
		if strings.TrimSpace(service.BaseURL) == "" {
			return fmt.Errorf("%w: 还没有填写自定义服务的地址", domain.ErrTranslateUnavailable)
		}
	}
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    map[string]string{},
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		if value := strings.TrimSpace(override[key]); value != "" {
			creds.Secrets[key] = value
			continue
		}
		if descriptor.AllowEmptyCredential {
			continue
		}
		secret, err := t.secrets.Get(ctx, domain.CredentialKey(descriptor.ID, key))
		if errors.Is(err, domain.ErrSecretNotFound) {
			return fmt.Errorf("%w: %s", domain.ErrCredentialMissing, domain.CredentialKey(descriptor.ID, key))
		}
		if err != nil {
			return err
		}
		creds.Secrets[key] = secret.Text()
	}
	// A test is a translation of a short sentence: a service that answers
	// this is a service that will answer the real thing. It is not the
	// user's text, and it asks for no annotation.
	request := domain.TranslationRequest{
		Text:       testText,
		SourceLang: domain.LangEnglish,
		TargetLang: domain.LangChinese,
	}
	if _, err := reply.Translate(ctx, creds, request); err != nil {
		return err
	}
	return nil
}

// testText is what a connection test translates. It is short, it is
// unmistakably a test rather than the user's own text, and an answer to it
// costs almost nothing.
const testText = "Hello, this is a connection test."

// Source is the text one translation is about, read the way the tool reads it:
// what the user has selected when the platform can say, and what is on the
// clipboard when it cannot.
//
// It is a step of its own rather than the first thing Translate does, because
// the order matters to the window that asks for it: reading the selection means
// asking the application the user is working in, and the card that shows the
// translation takes the keyboard away from that application. Read first, show
// the card, then translate — the translation the shortcut asks for is about the
// text this answered (§2.2 of the tool design).
//
// Nothing readable in either place is an empty answer, not an error: the
// platform saying "nothing is selected" and the clipboard holding a file list
// are both "the user has to select or copy some text". The one error is a
// clipboard that cannot be read at all, which is a fault worth showing.
func (t *Translate) Source(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// The selection is asked first: it is about what the user is looking at,
	// where the clipboard is about what they copied last. An element that
	// does not publish its text, a platform with no such interface and a
	// call that took too long are all the same answer here — nothing — and
	// the clipboard is asked instead.
	if text := t.selectionText(ctx); strings.TrimSpace(text) != "" {
		return text, nil
	}
	return t.clipboardText(ctx)
}

// Translate answers one request.
//
// The order below is the tool's rules, and it is the same order the window
// expects: a switch that is off, a translator that is not configured and a
// text that is not there are three different answers, and none of them is a
// request to a service.
func (t *Translate) Translate(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	settings, err := t.Settings()
	if err != nil {
		return Result{}, err
	}
	if !settings.Enabled {
		return Result{}, fmt.Errorf("%w: 划词翻译已关闭", domain.ErrTranslateDisabled)
	}
	providerID := settings.ProviderOr()
	reply, err := t.translators.Translator(providerID)
	if err != nil {
		// The service the settings name is not registered. That is a settings
		// file naming a translator this build does not have — a hand-edited
		// file, or one written by a version that shipped a service this one
		// does not.
		return Result{}, fmt.Errorf("%w: 找不到 %s，请在设置里重新选择一个翻译服务",
			domain.ErrTranslateUnavailable, providerID)
	}
	descriptor := reply.Descriptor()

	text := normalizeText(req.Text)
	if text == "" {
		// Nothing to translate: Source read nothing out of the selection and
		// nothing usable out of the clipboard — an empty one, a file list, an
		// image — so the user has to select or copy some text.
		return Result{}, fmt.Errorf("%w: 先选中文本并复制，再按快捷键", domain.ErrInvalidArgument)
	}

	creds, err := t.credentials(ctx, descriptor)
	if err != nil {
		return Result{}, err
	}
	source := domain.Language(kit.DetectLanguage(text))
	target := req.Target
	if target == "" || !target.Valid() {
		target = domain.Language(kit.TargetLanguage(string(source)))
	}
	// The annotation is asked for, and paid for, only by the request that
	// wants it: a translation with no annotation is a shorter prompt and a
	// shorter answer, which is the whole reason the card makes two requests.
	annotation := domain.AnnotationOptions{}
	if req.Annotate {
		annotation = domain.AnnotationOptions{
			Furigana:      settings.Furigana,
			LoanwordGloss: settings.LoanwordGloss,
		}
	}
	translation, err := reply.Translate(ctx, creds, domain.TranslationRequest{
		Text:       text,
		SourceLang: source,
		TargetLang: target,
		Annotation: annotation,
	})
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SourceText:     translation.SourceText,
		TranslatedText: translation.TranslatedText,
		Segments:       translation.Segments,
		SourceLang:     translation.SourceLang,
		TargetLang:     translation.TargetLang,
		ProviderID:     descriptor.ID,
		ProviderName:   descriptor.DisplayName,
		Note:           translation.Note,
		At:             translation.CreatedAt,
	}
	if result.At.IsZero() {
		result.At = t.now()
	}
	return result, nil
}

// normalizeText is the text as it is sent and drawn: the line breaks the user
// copied are kept, and they are made one kind of break.
//
// What a selection or the clipboard carries on Windows is \r\n, and the
// paragraphs the window draws are split on \n: a carriage return left inside
// one is a character nothing asked for, and the format the user copied — the
// lines of a poem, the rows of a list — is what they expect to see again.
func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

// clipboardText reads what is on the clipboard, as text. A clipboard holding
// a file list or an image is not a text to translate: it answers with the
// same error as an empty one, because that is what the user has to fix.
func (t *Translate) clipboardText(ctx context.Context) (string, error) {
	if t.clipboard == nil {
		return "", fmt.Errorf("%w: 剪贴板不可用", domain.ErrUnsupportedClipboardFormat)
	}
	content, err := t.clipboard.Read(ctx)
	if err != nil {
		return "", err
	}
	switch content.Kind {
	case domain.ClipKindFile, domain.ClipKindImage:
		return "", nil
	}
	return content.Text, nil
}

// RequestSelectionAccess asks the platform to show its permission prompt, on
// the platforms that have one. The app calls it from the settings page and
// before the first read: macOS does not list an application in Accessibility
// until the application asks.
func (t *Translate) RequestSelectionAccess() bool {
	if t.selection == nil {
		return false
	}
	authorizer, ok := t.selection.(domain.SelectionAuthorizer)
	if !ok {
		return false
	}
	return authorizer.RequestSelectionAccess()
}

// SelectionAccessGranted reports whether the platform already permits reading
// the selection. A platform with no permission model answers true, so the
// settings page only shows the request where there is something to request.
func (t *Translate) SelectionAccessGranted() bool {
	if t.selection == nil {
		return true
	}
	state, ok := t.selection.(domain.SelectionAccessState)
	if !ok {
		return true
	}
	return state.SelectionAccessGranted()
}

// selectionText asks the platform what is selected, and answers with nothing
// when it cannot say.
//
// Nothing here is an error. A platform with no API for it, a control that does
// not publish its text, a selection of nothing and a call that took too long
// are the same thing to the user: the older question — what is on the
// clipboard — is asked instead, and it is the one that always has an answer.
func (t *Translate) selectionText(ctx context.Context) string {
	if t.selection == nil {
		return ""
	}
	text, err := t.selection.SelectedText(ctx)
	if err != nil {
		return ""
	}
	return text
}

// credentials collects what a translator asked for. A missing one is
// ErrCredentialMissing before the translator is called at all: a request
// with no key would only teach the service that the key is wrong.
func (t *Translate) credentials(ctx context.Context, descriptor domain.TranslatorDescriptor) (domain.Credentials, error) {
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    make(map[string]string, len(descriptor.CredentialKeys)),
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		ref := domain.CredentialKey(descriptor.ID, key)
		secret, err := t.secrets.Get(ctx, ref)
		if errors.Is(err, domain.ErrSecretNotFound) {
			if descriptor.AllowEmptyCredential {
				continue
			}
			return creds, fmt.Errorf("%w: %s is not configured", domain.ErrCredentialMissing, ref)
		}
		if err != nil {
			return creds, err
		}
		value := strings.TrimSpace(secret.Text())
		if value == "" {
			if descriptor.AllowEmptyCredential {
				continue
			}
			return creds, fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, ref)
		}
		creds.Secrets[key] = value
	}
	return creds, nil
}
