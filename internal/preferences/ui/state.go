package ui

import (
	"boku/internal/platform/autostart"
	"boku/internal/preferences"
	"boku/internal/presentation"
	"context"
	"github.com/egoist/mygo/ui"
)

type State struct {
	autostart        autostart.Manager
	autostartStatus  autostartStatus
	appearanceStatus string
	host             presentation.Host
}

func (s *State) SaveAppearance(model *preferences.Appearance, store preferences.Store, mode preferences.AppearanceMode) {
	s.appearanceStatus = ""
	if err := model.Save(context.Background(), store, mode); err != nil {
		s.appearanceStatus = "主题没能写进设置文件：" + presentation.FriendlyError(err) + "（本次运行内已生效）"
	}
}

func (s *State) AppearanceStatus() string { return s.appearanceStatus }

func New(manager autostart.Manager, host presentation.Host) *State {
	return &State{autostart: manager, host: host}
}

type GeneralProps struct {
	Mark                               *ui.SVG
	Appearance                         preferences.AppearanceMode
	SetAppearance                      func(preferences.AppearanceMode)
	AppearanceStatus, Status, DataPath string
	Panels                             func(*ui.Context)
	RevealDataPath                     func()
}
