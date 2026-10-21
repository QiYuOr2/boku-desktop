// Package ui owns credential and connection configuration presentation.
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

// Connections is the cross-feature service contract of the connection form.
// The Shell supplies it; credential presentation does not construct translators.
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
