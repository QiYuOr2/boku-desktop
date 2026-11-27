// 本文件是划词翻译工具自己的配置：用哪个翻译器、怎么问、窗口如何表现。
//
// 它放在功能一起，而不是 internal/domain，是因为只有翻译工具读它。功能存的东西
// 应当像该功能本身，而只被一个功能使用的类型属于该功能——domain 留给多个功能
// 共享的实体与端口。
//
// 它写入的文件仍是应用的同一份设置文件，写入的键仍是存储设计 §8 描述的那些。
// internal/settings 拥有该文件，但不认识这个类型。
package translate

import (
	"sort"
	"strconv"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/translate/openai"
)

// customServicePrefix 是用户每个自有服务存储时所用 id 的前缀，从第一个起编号：
// "custom"、"custom-2"、"custom-3"……
//
// 它在此处拼写，并在 section.go 中作为 customID 拼写，因为两者是同一决定的两面：
// 设置文件怎么称呼一个服务，以及工具怎么称呼去问它的翻译器。
const customServicePrefix = "custom"

// Settings 是划词翻译工具被指定要做的事。
//
// 默认开启、其它均未配置：按快捷键应能到达工具，工具随后应告诉用户在哪里放 key，
// 而不是毫无反应。想让快捷键什么都不做的用户关闭 Enabled，它本会打开的窗口会说明
// 这一点。
type Settings struct {
	// Enabled 开启工具。关闭时快捷键打开一个说明工具已关闭的窗口，而不是一个出错的窗口。
	Enabled bool
	// Accelerator 是启动翻译的全局快捷键，采用框架快捷键 API 的形式，如
	// "CmdOrCtrl+Shift+T"。
	Accelerator string
	// ScreenshotAccelerator 启动原生区域截图。空值表示禁用。
	ScreenshotAccelerator string
	// Provider 按注册 id 指定要用的翻译器。没有任何翻译器响应的名字会被替换为默认值。
	Provider string
	// Furigana 让翻译器为日语汉字标注假名读音。
	Furigana bool
	// LoanwordGloss 让翻译器为片假名外来词标注其来源词。
	LoanwordGloss bool
	// Pinned 决定窗口是否以固定状态打开：固定的窗口停在放置处，点击别处不关闭。
	Pinned bool
	// CloseDelay 是窗口失去键盘焦点后等待多久自行关闭。它受下方常量约束，
	// 存储值越界时会被拉回范围内而非保留。
	CloseDelay time.Duration
	// Custom 是用户自己运行的 OpenAI 兼容服务，按各自存储的 id 索引
	// （"custom"、"custom-2"……）。
	//
	// 用 map 而非单个服务，是因为设置窗口提供「再添加一个」：用户可能既在本机有
	// 一个模型、又在别处有一个网关，都归他保留。某项的 id 就是 provider 设置所指
	// 的名字，因此记得 "custom-2" 的文件仍指向原来的服务。
	//
	// 它不敏感：凭据绝不放在这里。地址和名字是明文的，保存在设置文件中，正是
	// 存储设计 §8 所说的所在。
	Custom map[string]CustomSettings
}

// CustomSettings 是用户自己的一个翻译服务：它在哪，以及怎么称呼它。
type CustomSettings struct {
	// BaseURL 是服务地址，如 "https://api.example.com/v1"。
	// 为空表示服务未配置，窗口会说明这一点。
	BaseURL string
	// DisplayName 是用户对该服务的称呼。为空时界面使用自己的说法。
	DisplayName string
	// Model 是用户选定的 OpenAI 兼容模型名。
	Model string
	// SupportsImage 仅在 Boku 成功对该服务完成一次真实多模态请求后才置位。
	// 缺失表示尚未验证。
	SupportsImage bool
}

// CustomServiceIDs 返回用户自有服务的 id，排序后即设置窗口绘制它们的顺序。
func (s Settings) CustomServiceIDs() []string {
	ids := make([]string, 0, len(s.Custom))
	for id := range s.Custom {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// CustomService 按 id 返回用户自有服务之一。
func (s Settings) CustomService(id string) (CustomSettings, bool) {
	service, ok := s.Custom[id]
	return service, ok
}

// NextCustomServiceID 是新增服务存储时用的 id："custom"、"custom-2"、"custom-3"……
// 中第一个未被占用的。
//
// 裸 "custom" 保留为第一个 id，因为在可以有多于一个服务之前写下的文件持有该名，
// 把它读作其中第一个正是这一变更无需迁移的原因。
func (s Settings) NextCustomServiceID() string {
	if _, taken := s.Custom[customServicePrefix]; !taken {
		return customServicePrefix
	}
	for n := 2; ; n++ {
		id := customServicePrefix + "-" + strconv.Itoa(n)
		if _, taken := s.Custom[id]; !taken {
			return id
		}
	}
}

// IsCustomService 报告一个 id 是否指用户自己的服务，而非内置翻译器。
//
// 判断依据是前缀而非固定 id：id 是生成的，让多个服务并存，且它们对窗口和去问
// 它们的后端而言都是同一类东西。
func IsCustomService(id string) bool {
	return id == customServicePrefix || strings.HasPrefix(id, customServicePrefix+"-")
}

// 翻译窗口自身行为的边界。
const (
	// DefaultProvider 是全新安装使用的翻译器：微软 Edge 引擎，无需 key、账号或配置。
	//
	// 它是免费引擎之一，而非在它们之间挑选的一层，因为设置页把各引擎作为独立选项
	// 呈现。配置了自有服务的用户会选它，此后选择属于用户——存储的 provider 永不被
	// 改写，这正是「我配了 key，为什么答案变了」不会成为无人能答的问题的原因
	// （免费翻译设计 §4.5）。
	DefaultProvider = contract.MicrosoftEdge
	// DefaultAccelerator 是启动翻译的快捷键：设计设置页展示的那个，按框架解析器
	// 读取的方式书写。"CmdOrCtrl" 由平台解析器解析：macOS 上是 Command，
	// Windows 与 Linux 上是 Ctrl。写 "Ctrl" 会在 macOS 上绑定 Control+Shift+T，
	// 那不是 Mac 用户想要的组合。
	DefaultAccelerator           = "CmdOrCtrl+Shift+T"
	DefaultScreenshotAccelerator = "CmdOrCtrl+Alt+T"
	// MinCloseDelay 与 MaxCloseDelay 约束窗口自行关闭前的等待。允许零，表示「立即」：
	// 想让窗口一移开视线就消失的用户可以这样设。
	MinCloseDelay     = 0
	MaxCloseDelay     = 5 * time.Second
	DefaultCloseDelay = 400 * time.Millisecond
	// MaxChars 约束发给翻译器的字符数。选中内容是一句话或一段话；整篇文档粘贴进
	// 剪贴板不是这个工具的用途，窗口会说明省略了什么。
	MaxChars = 4000
	// MaxNameChars 约束用户对服务的称呼长度：它画在窗口的一行里，不作为文档存储。
	MaxNameChars = 40
)

// Defaults 是从未打开过设置的用户的配置：工具开启、内置翻译器、两个日语注释开启、
// 窗口不固定、失去焦点后很快关闭。
func Defaults() Settings {
	return Settings{
		Enabled:               true,
		Accelerator:           DefaultAccelerator,
		ScreenshotAccelerator: DefaultScreenshotAccelerator,
		Provider:              DefaultProvider,
		Furigana:              true,
		LoanwordGloss:         true,
		CloseDelay:            DefaultCloseDelay,
	}.Normalize()
}

// Normalize 补全存储设置省略的内容，并把非自由形式的值拉回边界内。
// 它从不失败：手工编辑的文件不应阻止应用启动，而应被改成合理的值。
func (s Settings) Normalize() Settings {
	if strings.TrimSpace(s.Provider) == "" {
		s.Provider = DefaultProvider
	}
	if strings.TrimSpace(s.Accelerator) == "" {
		s.Accelerator = DefaultAccelerator
	}
	switch {
	case s.CloseDelay < MinCloseDelay:
		s.CloseDelay = DefaultCloseDelay
	case s.CloseDelay > MaxCloseDelay:
		s.CloseDelay = MaxCloseDelay
	}
	s.Custom = normalizeCustomServices(s.Custom)
	// 指定用户自有服务的 provider 必须仍指向一个存在的服务。删除服务后设置会指向
	// 一个已不在的 id，而 provider 不存在的工具无法翻译：此时回退到默认值，而不是
	// 每次按键都失败。
	//
	// 这里只检查用户自有服务。内置 id 是否为本版本所知是注册表的问题，而注册表
	// 无法从值类型访问——那时查找会失败、窗口会说明，这对手工编辑的文件是正确答复。
	if IsCustomService(s.Provider) {
		if _, exists := s.Custom[s.Provider]; !exists {
			s.Provider = DefaultProvider
		}
	}
	return s
}

// Normalize 清理用户自有服务的地址与名字：非 http 或 https 的地址被丢弃，
// 而非交给 HTTP 客户端；名字裁到一行能显示的长度。
func (c CustomSettings) Normalize() CustomSettings {
	c.BaseURL = NormalizeBaseURL(c.BaseURL)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	c.Model = strings.TrimSpace(c.Model)
	if runes := []rune(c.DisplayName); len(runes) > MaxNameChars {
		c.DisplayName = strings.TrimSpace(string(runes[:MaxNameChars]))
	}
	return c
}

// normalizeCustomServices 清理用户的每一个自有服务。
//
// 地址未能通过清理的服务被丢弃：无处可发的项不是服务，留下它会在窗口里留一行
// 永远无法作答的内容。nil map 变成空 map，让调用者无需检查即可添加。
func normalizeCustomServices(services map[string]CustomSettings) map[string]CustomSettings {
	cleaned := make(map[string]CustomSettings, len(services))
	for id, service := range services {
		if strings.TrimSpace(id) == "" {
			continue
		}
		normalized := service.Normalize()
		if normalized.BaseURL == "" {
			continue
		}
		cleaned[id] = normalized
	}
	return cleaned
}

// CloseDelayOr 是窗口自行关闭前的等待，无论存储值如何都在边界内。
func (s Settings) CloseDelayOr() time.Duration {
	switch {
	case s.CloseDelay < MinCloseDelay:
		return DefaultCloseDelay
	case s.CloseDelay > MaxCloseDelay:
		return MaxCloseDelay
	}
	return s.CloseDelay
}

// ProviderOr 是要用的翻译器，未指定可用值时用默认。
func (s Settings) ProviderOr() string {
	if strings.TrimSpace(s.Provider) == "" {
		return DefaultProvider
	}
	return s.Provider
}

// AcceleratorOr 是启动翻译的快捷键，未存储时用默认。
func (s Settings) AcceleratorOr() string {
	if strings.TrimSpace(s.Accelerator) == "" {
		return DefaultAccelerator
	}
	return s.Accelerator
}

// ScreenshotAcceleratorOr 返回截图快捷键。空是有意义的：用户显式清除了它。
func (s Settings) ScreenshotAcceleratorOr() string { return strings.TrimSpace(s.ScreenshotAccelerator) }

// NormalizeBaseURL 把用户输入转成应用会去问的地址，或者转成空。
//
// 它复用协议包自身的清理，而非重写一遍：后端会去问设置里存的地址，对同一字符串
// 做两次清理就是两次可能不一的机会——存下 "127.0.0.1:11434/v1" 却被后端读成
// https，就是对不提供 TLS 的端口发请求。什么算合法地址的规则留在讲该协议的包里；
// 本功能只决定保留一个地址。
func NormalizeBaseURL(raw string) string { return openai.NormalizeBaseURL(raw) }
