package translate

import (
	"context"
	"errors"
	"testing"
	"time"

	"boku/internal/domain"
)

// 一个带 id 和可用描述符的翻译器可以单独注册：它不像 provider 那样需要配对的 adapter。
type registryTranslator struct {
	id         string
	descriptor domain.TranslatorDescriptor
	err        error
	result     domain.TranslationResult
	calls      int
}

func (f *registryTranslator) ID() string { return f.id }

func (f *registryTranslator) Descriptor() domain.TranslatorDescriptor {
	if f.descriptor.ID == "" {
		return domain.TranslatorDescriptor{
			ID:             f.id,
			DisplayName:    "Fake",
			CredentialKeys: []string{"apiKey"},
			DocsURL:        "https://example.com/keys",
			Endpoint:       "https://example.com/chat/completions",
		}
	}
	return f.descriptor
}

func (f *registryTranslator) Translate(context.Context, domain.Credentials, domain.TranslationRequest) (domain.TranslationResult, error) {
	f.calls++
	return f.result, f.err
}

func TestRegisterAndLookUp(t *testing.T) {
	registry := NewRegistry()
	one := &registryTranslator{id: "custom"}
	two := &registryTranslator{id: "deepseek"}
	if err := registry.Register(one); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Register(two); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	got, err := registry.Translator("custom")
	if err != nil {
		t.Fatalf("Translator failed: %v", err)
	}
	if got.ID() != "custom" {
		t.Errorf("looked up %q", got.ID())
	}
	if ids := registry.TranslatorIDs(); len(ids) != 2 || ids[0] != "custom" || ids[1] != "deepseek" {
		t.Errorf("TranslatorIDs() = %v, want them sorted", ids)
	}
	if registry.Len() != 2 {
		t.Errorf("Len() = %d", registry.Len())
	}
	if _, err := registry.Translator("nobody"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("an unknown id = %v, want ErrProviderNotFound", err)
	}
	// 空 id 也不指向任何翻译器：错误的注册不能变成一次侥幸的查找。
	if _, err := registry.Translator(""); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("the empty id = %v, want ErrProviderNotFound", err)
	}
}

func TestRegisterRefusesWhatIsWrong(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(nil) = %v", err)
	}
	// 无人可配置的描述符是启动错误，而不是等按下快捷键时才发现。
	broken := &registryTranslator{id: "broken", descriptor: domain.TranslatorDescriptor{ID: "broken"}}
	if err := registry.Register(broken); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(broken) = %v, want ErrInvalidArgument", err)
	}
	blank := &registryTranslator{id: "  "}
	if err := registry.Register(blank); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(no id) = %v, want ErrInvalidArgument", err)
	}

	// 两个翻译器共用一个 id 意味着同一个问题有两个答案：第二个被拒绝，而非替换第一个。
	if err := registry.Register(&registryTranslator{id: "custom"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Register(&registryTranslator{id: "custom"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("a duplicate id = %v, want ErrAlreadyExists", err)
	}
	if registry.Len() != 1 {
		t.Errorf("Len() = %d, want the first registration kept", registry.Len())
	}
}

// 空注册表不是失败：工具会说「去配置一个」，这正是用例需要能区分的情况。
func TestEmptyRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry.Len() != 0 {
		t.Errorf("Len() = %d", registry.Len())
	}
	if ids := registry.TranslatorIDs(); len(ids) != 0 {
		t.Errorf("TranslatorIDs() = %v", ids)
	}
	if _, err := registry.Translator("anything"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("error = %v, want ErrProviderNotFound", err)
	}
}

func TestFakeTranslatorIsUseful(t *testing.T) {
	// 该 fake 被本包其他测试和用例测试使用，这里用它校验其接口实现保持诚实。
	var reply domain.Translator = &registryTranslator{id: "custom"}
	_, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	_ = time.Now()
}
