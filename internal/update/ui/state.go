package ui

import (
	"boku/internal/domain"
	"boku/internal/presentation"
	"context"
)

type Service interface {
	State() domain.UpdateState
	Check(context.Context) error
	Install(context.Context) error
	OnChange(func())
}

type Notifier interface {
	Supported() bool
	Notify(id, title, body string, open func())
}

type State struct {
	updates           Service
	notifier          Notifier
	host              presentation.Host
	openSettings      func()
	version           string
	updateNotified    string
	updateCheckDialog bool
}

func New(service Service, notifier Notifier, host presentation.Host, open func(), version string) *State {
	s := &State{updates: service, notifier: notifier, host: host, openSettings: open, version: version}
	service.OnChange(s.onUpdateChanged)
	return s
}
