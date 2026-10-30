package ui

import (
	"context"
	"image"
	"strings"
	"time"

	"boku/internal/clipboard"
	contractfree "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/presentation"
	"boku/internal/translate"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const appName = "Boku"
const customTranslatorID = "custom"
const accessibilitySettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
const freeMicrosoftID = contractfree.MicrosoftEdge
const freeBilibiliID = contractfree.Bilibili

type RegionCapture interface {
	Capture(context.Context, image.Rectangle) ([]byte, error)
}
type Notifier interface {
	Supported() bool
	Notify(string, string, string, func())
}

type State struct {
	translate                                                     *translate.Translate
	clipboard                                                     *clipboard.Clipboard
	screenshot                                                    domain.ScreenshotDevice
	regionCapture                                                 RegionCapture
	qrDecode                                                      func([]byte) (string, error)
	regionSession                                                 *regionCaptureSession
	shortcuts                                                     ShortcutRegistrar
	translateWindow                                               CardWindow
	cardWindows                                                   func(func(), func(), func()) CardWindow
	translateNotice, delayInput                                   string
	translation                                                   translationState
	translationHeight, cardHeight, cardSettle                     int
	cardMeasuring                                                 bool
	cursor                                                        mygo.Point
	cursorSet                                                     bool
	translateCloseTimer                                           *time.Timer
	translationVisible                                            bool
	shortcut, screenshotShortcut                                  shortcutState
	imageTasks                                                    []imageTranslationTask
	nextImageTaskID                                               uint64
	selectionGranted                                              bool
	copyMark, pinMark, closeMark, screenshotMark, translationMark *ui.SVG
	notifier                                                      Notifier
	host                                                          presentation.Host
	dark                                                          func() bool
	connectionsBusy                                               func() bool
	// extraRebind registers shortcuts that share this registrar. MyGo can
	// only release every binding at once, so another feature's shortcut has
	// to be put back in the same rebind or the next translation change drops it.
	extraRebind func(register func(string, func()) error, text, screenshot string)
}

type Config struct {
	Service                                                       *translate.Translate
	Clipboard                                                     *clipboard.Clipboard
	Screenshot                                                    domain.ScreenshotDevice
	RegionCapture                                                 RegionCapture
	Shortcuts                                                     ShortcutRegistrar
	Host                                                          presentation.Host
	Dark                                                          func() bool
	Notifier                                                      Notifier
	ConnectionsBusy                                               func() bool
	CopyMark, PinMark, CloseMark, ScreenshotMark, TranslationMark *ui.SVG
}

func New(config Config) *State {
	s := &State{translate: config.Service, clipboard: config.Clipboard, screenshot: config.Screenshot, regionCapture: config.RegionCapture, shortcuts: config.Shortcuts, host: config.Host, dark: config.Dark, notifier: config.Notifier, connectionsBusy: config.ConnectionsBusy, copyMark: config.CopyMark, pinMark: config.PinMark, closeMark: config.CloseMark, screenshotMark: config.ScreenshotMark, translationMark: config.TranslationMark, qrDecode: kit.DecodeQRCodePNG, selectionGranted: true}
	if s.dark == nil {
		s.dark = func() bool { return false }
	}
	if s.connectionsBusy == nil {
		s.connectionsBusy = func() bool { return false }
	}
	if s.regionCapture != nil {
		s.screenshot = &nativeRegionScreenshot{state: s, capture: s.regionCapture}
	}
	return s
}

func (s *State) Refresh()                { s.host.Refresh() }
func backgroundContext() context.Context { return context.Background() }
func (s *State) useDesign(c *ui.Context) {
	presentation.ApplyDesign(s.dark())
	c.SetTheme(presentation.Theme(s.dark()))
}
func (s *State) translateSettings() (translate.Settings, error) {
	if s.translate == nil {
		return translate.Defaults(), nil
	}
	return s.translate.Settings()
}
func (s *State) translateSettingsOr() translate.Settings {
	settings, err := s.translateSettings()
	if err != nil {
		return translate.Defaults()
	}
	return settings
}
func isFreeTranslator(id string) bool { return id == freeMicrosoftID || id == freeBilibiliID }
func cardEndpointNote(endpoint string) string {
	if at := strings.Index(endpoint, "（"); at >= 0 {
		return strings.TrimSpace(endpoint[:at])
	}
	return strings.TrimSpace(endpoint)
}
func (s *State) translateCards() []presentation.Connection {
	if s.translate == nil {
		return nil
	}
	views, err := s.translate.Providers(context.Background())
	if err != nil {
		return nil
	}
	cards := make([]presentation.Connection, 0, len(views))
	for _, view := range views {
		cards = append(cards, presentation.Connection{ID: view.ID, DisplayName: view.DisplayName, DocsURL: view.DocsURL, Endpoint: view.Endpoint, CredentialKey: view.CredentialKey, NeedsCredential: view.NeedsCredential, Configured: view.Configured, SupportsImage: view.SupportsImage})
	}
	return cards
}
