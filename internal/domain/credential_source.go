package domain

import "context"

// CredentialSource 在凭据不属于 Boku 时，向 provider 提供它所需的东西：
// Codex 配额读取的是 Codex CLI 已建立的 ChatGPT 登录，把该 token 复制进
// Boku 自己的存储既无意义又更糟（见安全设计 §9）。
//
// 一个 source 由装配代码按某个 provider id 注册。用例向它而非 secret store
// 索取凭据，并且它是 provider 凭据唯一可能的另一来源：替代做法不是
// 「自己去查」，而是「请求一个 source」。
type CredentialSource interface {
	// ProviderID 是本 source 所供给的 provider。
	ProviderID() string
	// Credentials 返回该 provider 一次请求所需的内容，保存在内存中。
	// 凭据不存在时为 ErrCredentialMissing——此时 provider 不会被调用，
	// 界面说明原因而非显示一个数字。
	Credentials(ctx context.Context) (Credentials, error)
}

// LoginStarter 是一个凭据来自用户必须登录的工具、且其登录可从这里启动的
// CredentialSource。
//
// 它是可选能力而非每个 source 都有的方法：已登录的 source 没有可启动的登录，
// 界面便不为它提供按钮。
type LoginStarter interface {
	CredentialSource
	// StartLogin 启动该工具自身的登录。Boku 不实现该流程——登录属于工具——
	// 因此这里只负责找到一个终端。无法启动时返回 ErrLoginCommandMissing
	// 或 ErrLoginUnsupported。
	StartLogin() error
}

// CredentialRefresher 是能替换被端点刚刚拒绝的 token 的 source。
// 面板只尝试一次；无法刷新的 source 则由其把拒绝暴露出来。
type CredentialRefresher interface {
	RefreshCredentials(ctx context.Context) (Credentials, error)
}

// DevicePrompt 是用户需要输入或打开的内容。用于交换它的设备码留在 source 中。
type DevicePrompt struct {
	UserCode        string
	VerificationURL string
}

// DeviceLogin 是凭据来自本应用自行执行的设备码登录的 source。
type DeviceLogin interface {
	BeginDeviceLogin(ctx context.Context) (DevicePrompt, error)
	FinishDeviceLogin(ctx context.Context) error
	CancelDeviceLogin()
	Logout(ctx context.Context) error
}
