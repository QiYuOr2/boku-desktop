package panel

import (
	"context"
	"fmt"

	"boku/internal/domain"
)

// HasDeviceLogin 报告该 provider 是否由本应用自己执行设备码登录。
func (p *Panels) HasDeviceLogin(providerID string) bool {
	_, ok := p.deviceLogin(providerID)
	return ok
}

// DeviceLoggedIn 报告 source 记忆中是否已完成登录，不读取凭据文件。
func (p *Panels) DeviceLoggedIn(providerID string) bool {
	source, ok := p.sources[providerID].(interface{ LoggedIn() bool })
	return ok && source.LoggedIn()
}

// BeginDeviceLogin 启动设备码登录并返回用户需要输入的码；用于换取凭据的密钥保留在 source 内。
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

// FinishDeviceLogin 等待 Begin 启动的设备码登录完成。
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

// CancelDeviceLogin 停止仍在等待的设备码登录。
func (p *Panels) CancelDeviceLogin(providerID string) {
	if source, ok := p.deviceLogin(providerID); ok {
		source.CancelDeviceLogin()
	}
}

// LogoutDeviceLogin 删除 source 存储的登录。
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
