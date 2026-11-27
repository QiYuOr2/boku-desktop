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

// Command mocktranslation 是本地 OpenAI 兼容翻译服务，用于在不注册任何服务的情况下
// 试用 Boku 的自定义服务卡片。
//
// 它是开发工具，不属于应用：apps/desktop/internal 不引用它，也不进入发布二进制。
// 用 go run ./tools/mocktranslation 启动，在 http://127.0.0.1:11434/v1 应答，
// 即 Boku 设置里自定义服务卡片应填的地址。
//
// 它只实现翻译工具所需的那部分 chat-completions 协议，返回固定译文；当请求要求
// 逐词注解（日文设置）时，返回与真实后端一致的逐行 JSON。借此可端到端观察整条
// 路径：设置卡片的测试按钮、剪贴板快捷键、卡片读音、以及未注解时的提示。
//
// 它还会打印收到的每个请求，便于看到 Boku 实际发送的提示词与文本。
//
// 参数：
//
//	-addr      监听地址（默认 127.0.0.1:11434）
//	-key       要求携带的 key，用于测试卡片的 key 字段（默认不校验）
//	-echo      返回带前缀的原文而非固定译文，便于区分各请求
//	-fail      用该状态码让每个请求失败（0 表示不失败）
//	-delay     应答前等待的时长，用于观察加载卡片
//	-annotate  每个请求都返回逐词结果（默认仅在要求时）
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
	// 其它路径会明确报告而非静默 404：第一次配置时最可能出错的就是 Base URL。
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
	// 打印收到的内容：本 mock 的意义就在于看到 Boku 实际发送的提示词，
	// 提示词决定应答形态。逐词结果由提示词里要求字段判定。
	wantsPieces := strings.Contains(system, `"s"`) || strings.Contains(system, `"t"`)
	log.Printf(">> model=%q wantsPieces=%v text=%q", body.Model, wantsPieces || s.annotate, text)
	if system != "" {
		log.Printf("   system prompt: %s", firstLine(system))
	}

	content := s.answer(text, wantsPieces || s.annotate)
	writeChat(w, body.Model, content)
}

// stringContent 读取纯文本消息。图片消息的 content 是数组，
// 若把数组当文本处理，会让本 mock 拒绝它本应支持的截图请求。
func stringContent(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}

// userContent 读取普通文本消息或 OpenAI 兼容的多模态 content 数组。
// 这里刻意不保留也不打印 data URL，只确认 Boku 发送了图片。
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
	return `{"status":"success","sourceLang":"en","sourceText":"Boku screenshot test","targetLang":"zh","translatedText":"Boku 截图测试","failure":null}`
}

func writeImageStream(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	answer, _ := json.Marshal(imageAnswer())
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\ndata: [DONE]\n\n", answer)
}

// answer 返回应答：固定译文，或 -echo 时的原文，或要求逐词时的注解结果。
func (s *server) answer(text string, pieces bool) string {
	if !pieces {
		if s.echo {
			return "【mock】" + text
		}
		return cannedTranslation(text)
	}
	// 每行一个 JSON 对象，这是工具提示词要求的形态，也是 openai.ParseAnnotation
	// 读取的形态。示例即提示词自带的例子，让日文原文带读音与外来语注释，
	// 卡片才有内容可画。
	if strings.Contains(text, "回想") || strings.Contains(text, "モード") {
		return strings.Join([]string{
			`{"s":"【","t":"【"}`,
			`{"s":"回想","r":"カイソウ","t":"回想"}`,
			`{"s":"モード","g":"mock","t":"模式"}`,
			`{"s":"完備","r":"カンビ","t":"完备"}`,
			`{"s":"】","t":"】"}`,
		}, "\n")
	}
	// 其它情况：整段作为一个结果，避免卡片显示空答案。
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

// writeChat 按 OpenAI 兼容翻译器读取的格式应答：choices 数组的首条消息带内容。
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

// cannedTranslation 覆盖工具测试按钮和常见剪贴板文本，让本 mock 的表现接近真实。
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

// firstLine 取提示词首行用于日志：完整提示词很长，首行即可辨别是哪一条。
func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

// jsonEscape 把字符串转义为 JSON，用于手工拼装应答。
func jsonEscape(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		return text
	}
	// 去掉首尾引号：调用方会自行在字段两侧加引号。
	return string(encoded[1 : len(encoded)-1])
}
