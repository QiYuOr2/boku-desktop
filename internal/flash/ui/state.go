// Package ui is the flash capture line and the history window.
package ui

import (
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/flash"
	"boku/internal/presentation"
)

// State is the capture line, the history, and the shortcut row.
// Windows stay in the shell; this type only decides what they show and what a
// click does.
type State struct {
	flashes     *flash.Service
	host        presentation.Host
	copyText    func(string)
	rebindAll   func()
	showCapture func()
	hideCapture func()
	mark        *ui.SVG

	draft         string
	editingID     string
	captureDraft  string
	query         string
	accelerator   string
	entries       []flash.Entry
	loadErr       error
	notice        string
	noticeWarn    bool
	captureNotice string
	shortcutErr   string
	shortcutNote  string
	shortcutOn    bool
	recording     bool
	confirmDelete string
	closeMark     *ui.SVG
	commandMark   *ui.SVG
	returnMark    *ui.SVG
	focusDraft    bool
	focusCapture  bool
	draftHandle   ui.Handle
	captureHandle ui.Handle
}

// Config is what the shell injects. Copy and the window callbacks may be nil.
type Config struct {
	Service     *flash.Service
	Host        presentation.Host
	CopyText    func(string)
	RebindAll   func()
	ShowCapture func()
	HideCapture func()
	Mark        *ui.SVG
	CloseMark   *ui.SVG
	CommandMark *ui.SVG
	ReturnMark  *ui.SVG
}

// New returns the flash UI. A nil service still builds; the views say the tool is absent.
func New(config Config) *State {
	state := &State{
		flashes: config.Service, host: config.Host, copyText: config.CopyText,
		rebindAll: config.RebindAll, showCapture: config.ShowCapture, hideCapture: config.HideCapture,
		mark: config.Mark, closeMark: config.CloseMark,
		commandMark: config.CommandMark, returnMark: config.ReturnMark,
	}
	if state.flashes != nil {
		state.accelerator = state.flashes.Accelerator()
	} else {
		state.accelerator = ""
	}
	return state
}

// Reload reads the document. The view does not: a frame must not touch the file.
func (s *State) Reload() {
	if s.flashes == nil {
		s.entries = nil
		s.loadErr = nil
		return
	}
	entries, err := s.flashes.List()
	s.entries = entries
	s.loadErr = err
}

// Accelerator is the shortcut the shell should bind.
func (s *State) Accelerator() string {
	if s.flashes != nil {
		return s.flashes.Accelerator()
	}
	return s.accelerator
}

// Rebind registers this feature's shortcut with the registrar the shell just
// cleared. text and screenshot are combinations already taken by translation.
func (s *State) Rebind(register func(string, func()) error, text, screenshot string) {
	accelerator := s.Accelerator()
	s.accelerator = accelerator
	if strings.TrimSpace(accelerator) == "" {
		s.shortcutOn = false
		s.shortcutErr = ""
		return
	}
	if register == nil {
		s.shortcutOn = false
		s.shortcutErr = "没有可用的全局快捷键"
		return
	}
	if flash.SameAccelerator(accelerator, text) || flash.SameAccelerator(accelerator, screenshot) {
		s.shortcutOn = false
		s.shortcutErr = "不能与翻译快捷键相同"
		return
	}
	if err := register(accelerator, s.onShortcut); err != nil {
		s.shortcutOn = false
		s.shortcutErr = "快捷键没有绑上"
		return
	}
	s.shortcutOn = true
	s.shortcutErr = ""
}

func (s *State) onShortcut() {
	s.focusCapture = true
	if s.showCapture != nil {
		s.showCapture()
	}
}

// PrepareCapture asks the next frame to put the caret in the capture line.
func (s *State) PrepareCapture() { s.focusCapture = true }

func (s *State) beginEdit(entry flash.Entry) {
	s.editingID = entry.ID
	s.draft = entry.Text
	s.notice = ""
	s.noticeWarn = false
	s.focusDraft = true
}

func (s *State) clearDraft() {
	s.editingID = ""
	s.draft = ""
	s.notice = ""
	s.noticeWarn = false
}

func (s *State) escape() {
	if s.editingID == "" && s.draft == "" && s.notice == "" {
		return
	}
	s.clearDraft()
	s.host.Refresh()
}

func (s *State) setNotice(text string, warn bool) {
	s.notice = text
	s.noticeWarn = warn
}

func (s *State) commitCapture() {
	if s.flashes == nil {
		s.captureNotice = "闪念没有装上"
		s.host.Refresh()
		return
	}
	if _, err := s.flashes.SaveText(s.captureDraft, ""); err != nil {
		if errors.Is(err, flash.ErrEmpty) {
			s.captureNotice = "写一点再保存"
		} else if errors.Is(err, flash.ErrUnreadable) {
			s.captureNotice = "原文件读不了，没有覆盖"
		} else {
			s.captureNotice = "没有保存，内容还在"
		}
		s.host.Refresh()
		return
	}
	s.captureDraft = ""
	s.captureNotice = ""
	s.Reload()
	if s.hideCapture != nil {
		s.hideCapture()
	}
	s.host.Refresh()
}

func (s *State) commit() {
	if s.flashes == nil {
		s.setNotice("闪念没有装上", true)
		s.host.Refresh()
		return
	}
	if _, err := s.flashes.SaveText(s.draft, s.editingID); err != nil {
		if errors.Is(err, flash.ErrEmpty) {
			s.setNotice("写一点再保存", true)
		} else if errors.Is(err, flash.ErrUnreadable) {
			s.setNotice("原文件读不了，没有覆盖", true)
		} else {
			s.setNotice("没有保存，内容还在", true)
		}
		s.host.Refresh()
		return
	}
	s.clearDraft()
	s.Reload()
	s.host.Refresh()
}

func (s *State) remove(id string) {
	if s.confirmDelete != id {
		s.confirmDelete = id
		s.host.Refresh()
		return
	}
	s.confirmDelete = ""
	if s.flashes == nil {
		return
	}
	if err := s.flashes.Delete(id); err != nil {
		s.setNotice("没有删掉", true)
		s.host.Refresh()
		return
	}
	if s.editingID == id {
		s.clearDraft()
	}
	s.Reload()
	s.host.Refresh()
}

func (s *State) copy(text string) {
	if strings.TrimSpace(text) == "" || s.copyText == nil {
		s.setNotice("没有可复制的文字", true)
		return
	}
	s.copyText(text)
	s.setNotice("已复制", false)
}

func (s *State) visible() []flash.Entry {
	query := strings.ToLower(strings.TrimSpace(s.query))
	if query == "" {
		return s.entries
	}
	out := make([]flash.Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		if strings.Contains(strings.ToLower(entry.Text), query) {
			out = append(out, entry)
		}
	}
	return out
}

func when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("01-02 15:04")
}
