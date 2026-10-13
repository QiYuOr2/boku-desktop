package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestBadResponseErrorCarriesNoSecretAndIsClassifiable(t *testing.T) {
	err := &BadResponseError{
		ProviderID: "deepseek",
		Endpoint:   "https://api.deepseek.com/user/balance",
		HTTPStatus: 200,
		Reason:     "balance_infos is empty",
		Snippet:    `{"is_available":true}`,
	}
	if !errors.Is(err, ErrProviderBadResponse) {
		t.Error("errors.Is(err, ErrProviderBadResponse) is false")
	}
	message := err.Error()
	for _, want := range []string{"deepseek", "user/balance", "200", "balance_infos", `{"is_available":true}`} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q: %s", want, message)
		}
	}
}

func TestSnippetIsClipped(t *testing.T) {
	long := make([]byte, 4000)
	for i := range long {
		long[i] = 'a'
	}
	if got := len(Snippet(long)); got > MaxSnippetBytes {
		t.Errorf("Snippet kept %d bytes, want at most %d", got, MaxSnippetBytes)
	}
	if got := Snippet([]byte("  {\"a\":1}  ")); got != `{"a":1}` {
		t.Errorf("Snippet = %q, want the trimmed response", got)
	}
}
