// Package ui 负责凭据与连接配置的展示层。
package ui

import (
	"boku/internal/credential"
	"boku/internal/presentation"
	"boku/internal/translate"
	"context"
	"github.com/egoist/mygo/ui"
)

const customTranslatorID = "custom"

type State struct {
	Form          ConnectForm
	service       Connections
	secrets       *credential.Secrets
	host          presentation.Host
	ownServices   func() error
	refreshPanels func()
	keyMark       *ui.SVG
}

// Connections 是连接表单的跨功能服务契约，由 Shell 注入；凭据展示层不自行构造翻译器。
type Connections interface {
	Settings() (translate.Settings, error)
	Providers(context.Context) ([]translate.TranslatorView, error)
	RemoveCustomTranslator(context.Context, string) error
	SetProvider(context.Context, string) error
	TestTranslator(context.Context, string, map[string]string) error
	AddCustomTranslator(context.Context, translate.CustomSettings) (string, error)
	DetectCustomImageCapability(context.Context, string) (bool, error)
}

func New(service Connections, secrets *credential.Secrets, host presentation.Host, register func() error, refresh func(), key *ui.SVG) *State {
	return &State{service: service, secrets: secrets, host: host, ownServices: register, refreshPanels: refresh, keyMark: key}
}
