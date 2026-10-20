package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletionsStreamsAValidImageTranslation(t *testing.T) {
	request := map[string]any{
		"model": "mock-translate", "stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "translate the image"},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Read this image"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="}},
			}},
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	(&server{}).chatCompletions(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))
	response := recorder.Result()
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	payload := recorder.Body.String()
	if !strings.Contains(payload, "data: [DONE]") || !strings.Contains(payload, "Boku screenshot test") {
		t.Fatalf("stream = %q, want a completed image translation", payload)
	}
}
