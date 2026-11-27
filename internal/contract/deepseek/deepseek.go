// Package deepseek 定义 DeepSeek 服务的身份：注册名、凭据 key 名和凭据申请页。
//
// 余额面板（internal/panel/deepseek）和翻译器（internal/translate/deepseek）都访问
// 同一个服务，各自独立，但必须在 provider id 和凭据 key 上保持一致——"deepseek.apiKey"
// 同时是凭据存储里的一条规定和跨端契约（provider 契约 §7）里的一个名字。字符串写两份
// 就有两次写错的机会，因此只放在这里。
package deepseek

// ProviderID 是服务 id：余额面板注册的 provider id，也是翻译工具提供的翻译器 id。
const ProviderID = "deepseek"

// APIKeyCredential 是两端共用的凭据 key（不含 provider 前缀）：存储里保存为
// CredentialKey(ProviderID, APIKeyCredential)。
const APIKeyCredential = "apiKey"

// DocsURL 是用户创建 key 的页面，两个功能都会展示。
const DocsURL = "https://platform.deepseek.com/api_keys"
