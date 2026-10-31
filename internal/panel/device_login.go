package panel

import (
	"context"
	"fmt"

	"boku/internal/domain"
)

// HasDeviceLogin reports whether the provider logs in with a device code
// this application performs itself.
func (p *Panels) HasDeviceLogin(providerID string) bool {
	_, ok := p.deviceLogin(providerID)
	return ok
}

// DeviceLoggedIn reports the source's memory of a completed login. It does
// not read the credential file.
func (p *Panels) DeviceLoggedIn(providerID string) bool {
	source, ok := p.sources[providerID].(interface{ LoggedIn() bool })
	return ok && source.LoggedIn()
}

// BeginDeviceLogin starts a device-code login and returns the code the user
// has to enter. The secret that exchanges it stays with the source.
func (p *Panels) BeginDeviceLogin(ctx context.Context, providerID string) (domain.DevicePrompt, error) {
	source, ok := p.deviceLogin(providerID)
	if !ok {
		return domain.DevicePrompt{}, fmt.Errorf("%w: provider %s has no device login", domain.ErrProviderNotFound, providerID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return source.BeginDeviceLogin(ctx)
}

// FinishDeviceLogin waits for the device-code login that Begin started.
func (p *Panels) FinishDeviceLogin(ctx context.Context, providerID string) error {
	source, ok := p.deviceLogin(providerID)
	if !ok {
		return fmt.Errorf("%w: provider %s has no device login", domain.ErrProviderNotFound, providerID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return source.FinishDeviceLogin(ctx)
}

// CancelDeviceLogin stops a device-code login that is still waiting.
func (p *Panels) CancelDeviceLogin(providerID string) {
	if source, ok := p.deviceLogin(providerID); ok {
		source.CancelDeviceLogin()
	}
}

// LogoutDeviceLogin deletes the login the source stored.
func (p *Panels) LogoutDeviceLogin(ctx context.Context, providerID string) error {
	source, ok := p.deviceLogin(providerID)
	if !ok {
		return fmt.Errorf("%w: provider %s has no device login", domain.ErrProviderNotFound, providerID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return source.Logout(ctx)
}

func (p *Panels) deviceLogin(providerID string) (domain.DeviceLogin, bool) {
	source, ok := p.sources[providerID].(domain.DeviceLogin)
	return source, ok
}
