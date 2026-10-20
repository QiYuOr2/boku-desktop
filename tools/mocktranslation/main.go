package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Command mocktranslation is a local OpenAI-compatible translation service, for
// trying Boku's custom-service card without signing up for anything.
//
// It is a development tool, not part of the application: nothing in
// apps/desktop/internal imports it, and it is not in the build of the shipped
// binary. Run it with
//
//	go run ./tools/mocktranslation
//
// and it answers on http://127.0.0.1:11434/v1 — the address to type into the
// user's own service card in Boku's settings.
//
// What it does: it speaks just enough of the chat-completions protocol for the
// translation tool, answers with a canned translation, and — when the request
// asks for the piece-by-piece annotation the Japanese settings want — answers
// with the line-per-piece JSON the real backends answer with. That is what lets
// the whole path be watched end to end: the settings card's test button, the
// clipboard shortcut, the card's readings, and the note when nothing was
// annotated.
//
// It also prints every request it receives, so the prompt and the text Boku
// actually sends are visible rather than guessed at.
//
// Flags:
//
//	-addr      where to listen (default 127.0.0.1:11434)
//	-key       a key to require, for trying the card's key field (default: none)
//	-echo      answer with the text it was sent, prefixed, instead of a fixed
//	           translation — handy for seeing which request is which
//	-fail      fail every request with this status (0 = never fail)
//	-delay     wait this long before answering, for watching the loading card
//	-annotate  answer every request with pieces (default: only when asked)
func main() {
	addr := flag.String("addr", "127.0.0.1:11434", "address to listen on")
	key := flag.String("key", "", "require this API key (empty = accept any)")
	echo := flag.Bool("echo", false, "answer with the text that was sent")
	fail := flag.Int("fail", 0, "fail every request with this HTTP status")
	delay := flag.Duration("delay", 0, "wait this long before answering")
	annotate := flag.Bool("annotate", false, "answer with pieces even when not asked")
	flag.Parse()

	server := &server{
		key:      strings.TrimSpace(*key),
		echo:     *echo,
		fail:     *fail,
		delay:    *delay,
		annotate: *annotate,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", server.chatCompletions)
	mux.HandleFunc("/v1/models", server.models)
	// Anything else is reported rather than 404'd silently: a wrong Base URL is
	// the most likely thing to be wrong the first time.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("!! %s %s — Boku asked for a path this mock does not serve; "+
			"the Base URL should end in /v1", r.Method, r.URL.Path)
		http.Error(w, "not found: the Base URL should end in /v1", http.StatusNotFound)
	})

	log.Printf("mock translation service listening on http://%s/v1", *addr)
	log.Printf("in Boku, open 设置 → API Key → 自定义, and fill in:")
	log.Printf("   名称     : 本地测试服务")
	log.Printf("   Base URL : http://%s/v1", *addr)
	log.Printf("   API Key  : %s", keyHint(*key))
	if err := http.ListenAndServe(*addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "mocktranslation: %v\n", err)
		os.Exit(1)
	}
}

// keyHint is what the startup banner says about the key field.
func keyHint(key string) string {
	if strings.TrimSpace(key) == "" {
		return "（留空即可，本服务不校验）"
	}
	return key
}

type server struct {
	key      string
	echo     bool
	fail     int
	delay    time.Duration
	annotate bool
}

// chatRequest is the part of the request this mock reads.
type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.fail != 0 {
		log.Printf("<< answering %d on purpose", s.fail)
		http.Error(w, `{"error":"this failure was asked for with -fail"}`, s.fail)
		return
	}
	if !s.authorized(r) {
		log.Printf("<< 401: the request's key is not the one -key requires")
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
		return
	}

	var body chatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"the body is not JSON"}`, http.StatusBadRequest)
		return
	}
	system, text := "", ""
	imageRequest := false
	for _, message := range body.Messages {
		switch message.Role {
		case "system":
			system = stringContent(message.Content)
		case "user":
			text, imageRequest = userContent(message.Content)
		}
	}
	if imageRequest {
		log.Printf(">> model=%q image=true", body.Model)
		if body.Stream {
			writeImageStream(w, body.Model)
			return
		}
		writeChat(w, body.Model, imageAnswer())
		return
	}
	// What was asked is printed, because the point of this mock is to see what
	// Boku sends: the prompt is what decides the shape of the answer.
	wantsPieces := strings.Contains(system, `"s"`) || strings.Contains(system, `"t"`)
	log.Printf(">> model=%q wantsPieces=%v text=%q", body.Model, wantsPieces || s.annotate, text)
	if system != "" {
		log.Printf("   system prompt: %s", firstLine(system))
	}

	content := s.answer(text, wantsPieces || s.annotate)
	writeChat(w, body.Model, content)
}

// stringContent reads a text-only chat message. Image messages have an array
// content value; treating that array as text used to make this local mock
// reject the very screenshot request it exists to exercise.
func stringContent(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}

// userContent reads either a conventional text message or the standard
// OpenAI-compatible multimodal content array. The mock deliberately does not
// retain or print the data URL: it only confirms that Boku sent an image.
func userContent(raw json.RawMessage) (string, bool) {
	if text := stringContent(raw); text != "" {
		return text, false
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	for _, part := range parts {
		if part.Type == "image_url" && strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,") {
			return "", true
		}
	}
	return "", false
}

func imageAnswer() string {
	return `{"sourceLang":"en","sourceText":"Boku screenshot test","targetLang":"zh","translatedText":"Boku 截图测试"}`
}

func writeImageStream(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	answer, _ := json.Marshal(imageAnswer())
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\ndata: [DONE]\n\n", answer)
}

// answer is what the service answers with: a canned translation, or the text
// it was sent when -echo is on, and the piece-by-piece annotation when one was
// asked for.
func (s *server) answer(text string, pieces bool) string {
	if !pieces {
		if s.echo {
			return "【mock】" + text
		}
		return cannedTranslation(text)
	}
	// One JSON object per line, which is the shape the tool's own prompt asks
	// for and openai.ParseAnnotation reads. The example is the one the prompt
	// itself uses, so a Japanese source gets readings and a loanword gloss and
	// the card has something to draw.
	if strings.Contains(text, "回想") || strings.Contains(text, "モード") {
		return strings.Join([]string{
			`{"s":"【","t":"【"}`,
			`{"s":"回想","r":"カイソウ","t":"回想"}`,
			`{"s":"モード","g":"mock","t":"模式"}`,
			`{"s":"完備","r":"カンビ","t":"完备"}`,
			`{"s":"】","t":"】"}`,
		}, "\n")
	}
	// Anything else: one piece per rune-free chunk, so the card still draws a
	// column rather than an empty answer.
	return strings.Join([]string{
		`{"s":"` + jsonEscape(text) + `","t":"【mock 逐词】` + jsonEscape(text) + `"}`,
	}, "\n")
}

func (s *server) models(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"data": []any{
		map[string]any{"id": "mock-translate", "object": "model"},
	}})
}

func (s *server) authorized(r *http.Request) bool {
	if s.key == "" {
		return true
	}
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == s.key
}

// writeChat answers in the shape the OpenAI-compatible translators read: a
// choices array whose first message carries the content.
func writeChat(w http.ResponseWriter, model, content string) {
	if strings.TrimSpace(model) == "" {
		model = "mock-translate"
	}
	writeJSON(w, map[string]any{
		"id":      "mock-1",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("!! could not write the answer: %v", err)
	}
}

// cannedTranslation is a translation of the sentences the tool's own test
// button and the usual clipboard text use, so that a run of the mock looks like
// a real one.
func cannedTranslation(text string) string {
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.Contains(trimmed, "connection test"):
		return "你好，这是一次连接测试。"
	case strings.Contains(trimmed, "Hello world"):
		return "你好，世界。"
	case trimmed == "":
		return "（空）"
	default:
		return "【mock 译文】" + trimmed
	}
}

// firstLine is a prompt's first line, for the log: the whole prompt is long and
// its first line is what says which one it is.
func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

// jsonEscape quotes a string as JSON, for building an answer by hand.
func jsonEscape(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		return text
	}
	// Strip the surrounding quotes: the caller adds its own around the field.
	return string(encoded[1 : len(encoded)-1])
}
