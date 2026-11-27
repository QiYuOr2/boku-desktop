// 本文件是划词翻译在 settings.json 中的那一半。
//
// 拥有该文件的存储在内容上一无所知：它把整份设置对象交给每个功能，再收回每个功能
// 声明归自己所有的键。本文件是翻译工具的那一半——哪些键属于它，以及它自己的
// Settings 类型如何写下去。
//
// 把它放在这里而不是 internal/settings，是为了让设置归属于功能：新增一个开关只改
// 本包、不改别处；其它功能也碰不到这些键，因为它们从不用到这些键解码出的类型。
package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 本功能在 settings.json 中拥有的键。它们是存储设计 §8 里人可编辑的名字，
// 所以只在这里拼写一次，绝不从 Go 字段名推导。
const (
	keyEnabled               = "translateEnabled"
	keyAccelerator           = "translateAccelerator"
	keyScreenshotAccelerator = "translateScreenshotAccelerator"
	keyProvider              = "translateProvider"
	keyFurigana              = "translateFurigana"
	keyLoanwordGloss         = "translateLoanwordGloss"
	keyPinned                = "translatePinned"
	keyCloseDelay            = "translateCloseDelayMs"
	keyCustomServices        = "translators"
)

// sectionName 是错误信息里本功能设置的称呼。
const sectionName = "translate"

// customID 是用户自己填写设置的翻译器 id。它在此处命名，而不从实现它的包导入，
// 因为文件中的字段名是存储决定：本包必须继续读取一个翻译器已迁移过的文件。
//
// 它是用户自有服务 id 的*前缀*，而不是一个独立 id：用户可保留多个服务，每个都是
// 单独一项（"custom"、"custom-2"……），各自的名字与地址独立。在可以有多于一个之前
// 写下的文件只含裸 "custom"，它被读作其中第一个。
const customID = "custom"

// StoreSettings 是本功能需要的设置存储那部分。
//
// 它在此处声明，而不取自 internal/settings，让本包不依赖存储实现：装配注入一个，
// 测试注入自己的。
type StoreSettings interface {
	Load(ctx context.Context) error
	Update(ctx context.Context, section string, change func() error) error
}

// customDocument 是文件为用户配置的翻译器保存的内容。
type customDocument struct {
	BaseURL       string `json:"baseURL,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	Model         string `json:"model,omitempty"`
	SupportsImage bool   `json:"supportsImage,omitempty"`
}

// Section 是存储眼中本功能的设置：一个名字、一种从文件中读出自己所拥有的键的方法，
// 以及一种把它们交回去的方法。
type Section struct {
	current Settings
}

// NewSection 返回一个空 section，持有本功能的默认值。
func NewSection() *Section {
	return &Section{current: Defaults()}
}

// Name 是错误信息里对本 section 的称呼。
func (s *Section) Name() string { return sectionName }

// Current 是当前状态的设置。
func (s *Section) Current() Settings { return s.current }

// Set 替换设置。
func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode 从 values（含所有功能的键）中读出翻译工具的键。
//
// 缺席的键保持设置不变。这对两个默认开启的开关最重要：工具尚不存在时写下的文件
// 根本没有它们的键，而把缺席的键读成 false 会关掉两个日语注释。
func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := into(values, keyEnabled, &settings.Enabled); err != nil {
		return err
	}
	if err := into(values, keyAccelerator, &settings.Accelerator); err != nil {
		return err
	}
	if err := into(values, keyScreenshotAccelerator, &settings.ScreenshotAccelerator); err != nil {
		return err
	}
	if err := into(values, keyProvider, &settings.Provider); err != nil {
		return err
	}
	if err := into(values, keyFurigana, &settings.Furigana); err != nil {
		return err
	}
	if err := into(values, keyLoanwordGloss, &settings.LoanwordGloss); err != nil {
		return err
	}
	if err := into(values, keyPinned, &settings.Pinned); err != nil {
		return err
	}
	// 延迟按「文件是否写了这个键」来读，而非「值是否非零」：零是用户可选的值
	// （MinCloseDelay），表示卡片立即关闭。把存在的零读成缺席的键，会让这些用户
	// 下次启动时回到默认值。
	if raw, present := values[keyCloseDelay]; present {
		var delayMs int64
		if err := json.Unmarshal(raw, &delayMs); err != nil {
			return fmt.Errorf("%s: %w", keyCloseDelay, err)
		}
		settings.CloseDelay = time.Duration(delayMs) * time.Millisecond
	}
	var custom map[string]customDocument
	if err := into(values, keyCustomServices, &custom); err != nil {
		return err
	}
	// 每项都保留在其存储 id 下。没有地址的项被丢弃，而不是作为无名服务保留：
	// 没有可问的对象，用户用不上的行只会碍事。
	services := make(map[string]CustomSettings, len(custom))
	for id, own := range custom {
		cleaned := CustomSettings{BaseURL: own.BaseURL, DisplayName: own.DisplayName, Model: own.Model, SupportsImage: own.SupportsImage}.Normalize()
		if cleaned.BaseURL == "" {
			continue
		}
		services[id] = cleaned
	}
	settings.Custom = services
	s.current = settings.Normalize()
	return nil
}

// Encode 返回翻译工具拥有的键，供存储合并进文件。
func (s *Section) Encode() (map[string]any, error) {
	settings := s.current.Normalize()
	services := make(map[string]customDocument, len(settings.Custom))
	for id, service := range settings.Custom {
		services[id] = customDocument{
			BaseURL:       service.BaseURL,
			DisplayName:   service.DisplayName,
			Model:         service.Model,
			SupportsImage: service.SupportsImage,
		}
	}
	return map[string]any{
		keyEnabled:               settings.Enabled,
		keyAccelerator:           settings.AcceleratorOr(),
		keyScreenshotAccelerator: settings.ScreenshotAcceleratorOr(),
		keyProvider:              settings.ProviderOr(),
		keyFurigana:              settings.Furigana,
		keyLoanwordGloss:         settings.LoanwordGloss,
		keyPinned:                settings.Pinned,
		keyCloseDelay:            settings.CloseDelayOr().Milliseconds(),
		// 即使 map 里没有值得保留的内容也会写出：它的存在告诉读取者这个版本写过
		// 该文件，因此其中缺席的键是选择而非某版本的缺口。
		keyCustomServices: services,
	}, nil
}

// into 解码设置对象里的一个键，文件未写它时保持不动。
func into(values map[string]json.RawMessage, key string, out any) error {
	raw, present := values[key]
	if !present {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
