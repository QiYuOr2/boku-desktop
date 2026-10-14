package translator

import (
	"context"
	"errors"
	"testing"
	"time"

	"boku/internal/domain"
)

// A translator with an id and a working descriptor stands alone: there is no
// adapter to register with it, unlike a provider.
type fakeTranslator struct {
	id         string
	descriptor domain.TranslatorDescriptor
	err        error
	result     domain.TranslationResult
	calls      int
}

func (f *fakeTranslator) ID() string { return f.id }

func (f *fakeTranslator) Descriptor() domain.TranslatorDescriptor {
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

func (f *fakeTranslator) Translate(context.Context, domain.Credentials, domain.TranslationRequest) (domain.TranslationResult, error) {
	f.calls++
	return f.result, f.err
}

func TestRegisterAndLookUp(t *testing.T) {
	registry := NewRegistry()
	one := &fakeTranslator{id: "custom"}
	two := &fakeTranslator{id: "deepseek"}
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
	// The empty id names no translator either: a registration that is wrong
	// must not become a lookup that is lucky.
	if _, err := registry.Translator(""); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("the empty id = %v, want ErrProviderNotFound", err)
	}
}

func TestRegisterRefusesWhatIsWrong(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(nil) = %v", err)
	}
	// A descriptor nobody can configure is a startup error, not something
	// to discover when the shortcut is pressed.
	broken := &fakeTranslator{id: "broken", descriptor: domain.TranslatorDescriptor{ID: "broken"}}
	if err := registry.Register(broken); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(broken) = %v, want ErrInvalidArgument", err)
	}
	blank := &fakeTranslator{id: "  "}
	if err := registry.Register(blank); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Register(no id) = %v, want ErrInvalidArgument", err)
	}

	// Two translators answering to one id means two answers to the same
	// question: the second is refused rather than replacing the first.
	if err := registry.Register(&fakeTranslator{id: "custom"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Register(&fakeTranslator{id: "custom"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("a duplicate id = %v, want ErrAlreadyExists", err)
	}
	if registry.Len() != 1 {
		t.Errorf("Len() = %d, want the first registration kept", registry.Len())
	}
}

// A registry with nothing in it is not a failure: the tool says "go and
// configure one", which is what the use case needs to be able to tell.
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
	// The fake is used by other tests in this package and by the use case's
	// tests; this keeps it honest about the interface it stands in for.
	var reply domain.Translator = &fakeTranslator{id: "custom"}
	_, err := reply.Translate(context.Background(), domain.Credentials{}, domain.TranslationRequest{
		Text:       "hello",
		TargetLang: domain.LangChinese,
	})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	_ = time.Now()
}
