package main

import (
	"boku/internal/platform/httpc"
	"boku/internal/translate"
	customtranslator "boku/internal/translate/custom"
	"boku/internal/translate/openai"
)

// ownServices keeps the registry in step with the services the user runs
// themselves.
//
// One translator per stored service, registered under the id the settings file
// holds it as. The set is not written down anywhere, because it is not fixed:
// the user adds a service from the settings window, and the translator for it
// has to exist before the next translation asks one. Registration is therefore
// something that can be done again, and doing it again is cheap — a service
// that is already registered is skipped.
//
// It exists as a type rather than a handful of lines in services() so that
// "catch up with what the user added" is one call, made wherever a service is
// added; keeping the two in step is then a property of the code rather than
// something a caller has to remember.
type ownServices struct {
	client   *httpc.Client
	registry *translate.Registry
	section  *translate.Section
}

// newOwnServices returns the registrar over the registry the assembly built.
func newOwnServices(client *httpc.Client, registry *translate.Registry, section *translate.Section) *ownServices {
	return &ownServices{client: client, registry: registry, section: section}
}

// registerAll registers a translator for every service the settings hold, and
// leaves the ones already registered alone.
//
// registerAll makes the registry match the settings: a translator for every
// service they hold, and none for the ones they no longer hold.
//
// It is what a launch calls once, and what the settings window calls after the
// user adds a service or removes one. Both directions are the same job — the
// cards the window draws come from the registry, and which services exist comes
// from the settings, so the two have to say the same thing. A service the
// settings dropped but the registry kept would be a card left on the page with
// no address and nothing to ask.
//
// A service whose translator is already there is left alone rather than
// registered twice: the registry refuses a duplicate id, and re-registering a
// service is not a mistake worth failing on.
func (o *ownServices) registerAll() error {
	present := map[string]bool{}
	for _, id := range o.section.Current().CustomServiceIDs() {
		present[id] = true
		if err := o.register(id); err != nil {
			return err
		}
	}
	for _, id := range o.registry.TranslatorIDs() {
		if present[id] || !translate.IsCustomService(id) {
			continue
		}
		o.registry.Remove(id)
	}
	return nil
}

// register adds the translator for one service, unless it is already there.
func (o *ownServices) register(id string) error {
	if o.registry.Has(id) {
		return nil
	}
	// The address and the name are read from the section each time the
	// translator is asked, not captured here: the user may change them while
	// the application runs, and the next translation should use what they
	// changed it to. It is in memory, so asking costs no file read.
	settings := o.section
	return o.registry.Register(customtranslator.New(o.client, id, func() openai.CustomService {
		service, _ := settings.Current().CustomService(id)
		return openai.CustomService{
			BaseURL:     service.BaseURL,
			DisplayName: service.DisplayName,
		}
	}))
}
