package presentation

// Connection 是凭据配置与翻译服务选择器共享的展示元数据，绝不包含已存储的凭据值。
type Connection struct {
	ID                                            string
	DisplayName, DocsURL, Endpoint, CredentialKey string
	NeedsCredential, Configured, SupportsImage    bool
}
