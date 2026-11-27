// Package freetranslate 存放免费翻译引擎的身份：注册 id、描述符携带的凭据 key 和文档链接。
//
// id 被两处读取且必须一致：翻译后端（internal/translate/free）与设置窗口
// （internal/app 的翻译分区）。字符串只保留一份，避免两处不一致。
package freetranslate

// 免费引擎，名称与设置文件一致。
// 每个引擎既是独立翻译器也是独立选项：用户选哪个就问哪个，不做池化、不做回退，
// 失败的引擎直接报错，由用户自己换。
const (
	// MicrosoftEdge 是 Edge 自带翻译功能请求的端点。
	MicrosoftEdge = "free.microsoft"
	// Bilibili 是 B 站免费的 Index-Translate 模型，支持注释，日文场景需要。
	Bilibili = "free.bilibili"
)

// APIKeyCredential 是描述符携带的占位凭据名。
// 两个引擎都不需要凭据，由 AllowEmptyCredential 声明；这里保留名字是因为
// TranslatorDescriptor.Validate 不允许不声明凭据的翻译器。
const APIKeyCredential = "apiKey"

// DocsURL 是免费引擎的说明页，说明文本会发往哪些端点（并非发 key 的页面）。
const DocsURL = "https://github.com/bilibili/Index-Translate"
