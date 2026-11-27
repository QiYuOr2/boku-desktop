package free

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/translate/openai"
)

func chatAnswer(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}
	return `{"choices":[{"message":{"content":` + string(encoded) + `}}]}`
}

const microsoftAnswer = `[{"detectedLanguage":{"language":"en","score":0.9},
  "translations":[{"text":"你好，世界。","to":"zh-Hans","sentLen":{"srcSentLen":[12],"transSentLen":[6]}}]}]`

func plainRequest(text string) domain.TranslationRequest {
	return domain.TranslationRequest{
		Text:       text,
		SourceLang: domain.LangEnglish,
		TargetLang: domain.LangChinese,
	}
}

func TestTheEnginesCarryTheirOwnIdentity(t *testing.T) {
	client := httpc.New(httpc.Options{})
	microsoft := NewMicrosoft(client)
	bilibili := NewBilibili(client)

	if microsoft.ID() != contract.MicrosoftEdge {
		t.Errorf("microsoft id = %q, want %q", microsoft.ID(), contract.MicrosoftEdge)
	}
	if bilibili.ID() != contract.Bilibili {
		t.Errorf("bilibili id = %q, want %q", bilibili.ID(), contract.Bilibili)
	}
	// 一个引擎一个 id：两个免费引擎共用 id 会让设置文件里同一个条目有两种含义。
	if microsoft.ID() == bilibili.ID() {
		t.Error("the two free engines share an id")
	}
	// 两者在完全未配置时都可用，这正是它们的意义。
	for _, reply := range []domain.Translator{microsoft, bilibili} {
		descriptor := reply.Descriptor()
		if err := descriptor.Validate(); err != nil {
			t.Fatalf("%s does not validate: %v", reply.ID(), err)
		}
		if !descriptor.AllowEmptyCredential {
			t.Errorf("%s is reported as needing a credential", reply.ID())
		}
		if descriptor.NeedsCredential(contract.APIKeyCredential) {
			t.Errorf("%s requires the placeholder key it names", reply.ID())
		}
		if descriptor.DisplayName == "" {
			t.Errorf("%s has no name to draw", reply.ID())
		}
		if strings.TrimSpace(descriptor.Endpoint) == "" {
			t.Errorf("%s does not say where it asks", reply.ID())
		}
	}
}

func TestBilibiliDoesNotAdvertiseImageTranslation(t *testing.T) {
	bilibili := NewBilibili(httpc.New(httpc.Options{}))
	if bilibili.Descriptor().SupportsImage {
		t.Error("Bilibili must not advertise image support")
	}
	if _, ok := any(bilibili).(domain.ImageTranslator); ok {
		t.Error("Bilibili must not implement image translation")
	}
}

func TestTheEnginesAreNamedAsTheSettingsPageDrawsThem(t *testing.T) {
	client := httpc.New(httpc.Options{})
	if got := NewMicrosoft(client).Descriptor().DisplayName; got != "微软 Edge 翻译" {
		t.Errorf("microsoft is called %q", got)
	}
	if got := NewBilibili(client).Descriptor().DisplayName; got != "B站 Index-Translate" {
		t.Errorf("bilibili is called %q", got)
	}
}

func TestMicrosoftAsksAndReadsTheAnswer(t *testing.T) {
	var (
		path  string
		query string
		body  []string
		auth  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"/translate/translatetext?from=&to=%s&isEnterpriseClient=false")
	result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello world."))
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好，世界。" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceText != "Hello world." || result.TargetLang != domain.LangChinese {
		t.Errorf("result = %+v", result)
	}
	if result.CreatedAt.IsZero() {
		t.Error("the result has no time")
	}
	// 请求体是裸字符串数组，目标语言按该端点自己的拼法。
	if len(body) != 1 || body[0] != "Hello world." {
		t.Errorf("body = %v, want one string", body)
	}
	if path != "/translate/translatetext" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(query, "to=zh-Hans") {
		t.Errorf("query = %q, want the simplified-Chinese code", query)
	}
	// 引擎不发送凭据，因为无凭据可发：向公开端点发送空 bearer token 等于告诉
	// 对方一个不存在的 key。
	if auth != "" {
		t.Errorf("authorization = %q, want none", auth)
	}
}

// 每次只答一句的引擎无法提供词内信息，如实说明而非失败：用户得到译文，并被明确
// 告知缺了什么。
func TestMicrosoftAnswersARequestForReadingsWithANote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
	request := plainRequest("【回想モード完備】")
	request.Annotation = domain.AnnotationOptions{Furigana: true, LoanwordGloss: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好，世界。" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.Segments != nil {
		t.Errorf("an engine that cannot annotate produced segments: %+v", result.Segments)
	}
	if result.Note != noReadingsNote {
		t.Errorf("note = %q, want the word about missing readings", result.Note)
	}
}

func TestMicrosoftFailuresAreDomainErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		payload string
		want    error
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":"too many"}`, domain.ErrProviderRateLimited},
		{"unauthorized", http.StatusUnauthorized, `{}`, domain.ErrProviderUnauthorized},
		{"down", http.StatusBadGateway, ``, domain.ErrProviderUnavailable},
		{"not JSON", http.StatusOK, `<html>`, domain.ErrProviderBadResponse},
		{"empty array", http.StatusOK, `[]`, domain.ErrProviderBadResponse},
		{"no translations", http.StatusOK, `[{"detectedLanguage":{"language":"en"}}]`, domain.ErrProviderBadResponse},
		{"empty translation", http.StatusOK, `[{"translations":[{"text":"  ","to":"zh-Hans"}]}]`, domain.ErrProviderBadResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()

			engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
			result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello world."))
			if err == nil {
				t.Fatalf("a %s answer was accepted", tc.name)
			}
			if !errorsIs(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
		})
	}
}

// 空文本是调用方的错误，不应向端点发出任何请求。
func TestTheEnginesRefuseAnEmptyRequest(t *testing.T) {
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
	}))
	defer server.Close()

	client := httpc.New(httpc.Options{})
	for _, reply := range []domain.Translator{
		newMicrosoft(client, server.URL+"?to=%s"),
		newBilibili(client, server.URL, "m"),
	} {
		if _, err := reply.Translate(t.Context(), domain.Credentials{}, plainRequest("   ")); err == nil {
			t.Errorf("%s answered a request with no text", reply.ID())
		}
	}
	if asked {
		t.Error("an engine was asked for a request with no text")
	}
}

// 被截断的文本会说明，未截断则不提。
func TestMicrosoftNotesALongText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()
	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")

	request := plainRequest(strings.Repeat("あ", DefaultMaxChars+10))
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if !strings.Contains(result.Note, "原文过长") {
		t.Errorf("note = %q, want the word about the cut", result.Note)
	}
	if len([]rune(result.SourceText)) != DefaultMaxChars {
		t.Errorf("the text sent is %d characters, want the ceiling", len([]rune(result.SourceText)))
	}
}

// B 站引擎与付费后端一样请求 OpenAI-compatible 端点，并从模型答复中读出标注。它是
// 唯一能做这件事的免费引擎，因而也是翻译日语时该选的。
func TestBilibiliAsksForAnAnnotatedAnswer(t *testing.T) {
	var (
		path string
		body map[string]any
		auth string
	)
	lines := []string{
		`{"s":"【","t":"【"}`,
		`{"s":"回想","r":"カイソウ","t":"回想"}`,
		`{"s":"モード","g":"mode","t":"模式"}`,
	}
	answer := chatAnswer(strings.Join(lines, "\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "Index-Translate-35B-A3B")
	request := plainRequest("【回想モード】")
	request.SourceLang = domain.LangJapanese
	request.Annotation = domain.AnnotationOptions{Furigana: true, LoanwordGloss: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if path != "/chat/completions" {
		t.Errorf("path = %q", path)
	}
	if auth != "" {
		t.Errorf("authorization = %q, want none", auth)
	}
	if body["model"] != "Index-Translate-35B-A3B" {
		t.Errorf("model = %v", body["model"])
	}
	if len(result.Segments) != 3 {
		t.Fatalf("segments = %+v, want the pieces the model answered", result.Segments)
	}
	if result.Segments[1].Reading != "カイソウ" {
		t.Errorf("the reading is not carried: %+v", result.Segments[1])
	}
	if result.Segments[2].Gloss != "mode" {
		t.Errorf("the loanword gloss is not carried: %+v", result.Segments[2])
	}
}

// 请求分段却答了整段不算失败：整段即译文，提示说明缺了什么，与付费后端一致。
func TestBilibiliNotesAnAnswerWithNoPieces(t *testing.T) {
	answer := chatAnswer("【回忆模式已完备】")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	request := plainRequest("【回想モード完備】")
	request.Annotation = domain.AnnotationOptions{Furigana: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "【回忆模式已完备】" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.Segments != nil {
		t.Errorf("a plain answer produced segments: %+v", result.Segments)
	}
	if result.Note != noReadingsNote {
		t.Errorf("note = %q, want the word about missing readings", result.Note)
	}
}

// 模型用围栏代码块包裹答复（最常见的违背「不要代码块」的方式）也按同样方式读取：
// 解析器剥掉围栏，读不成片段的内容即为译文。
func TestBilibiliReadsAModelThatIgnoresTheFormat(t *testing.T) {
	answer := chatAnswer("```json\n" + `{"s":"こんにちは","t":"你好"}` + "\n```")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	request := plainRequest("こんにちは")
	request.SourceLang = domain.LangJapanese
	request.Annotation = domain.AnnotationOptions{Furigana: true}
	result, err := engine.Translate(t.Context(), domain.Credentials{}, request)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	// 围栏不是译文、也不画出：画的是围栏内的片段。
	if len(result.Segments) != 1 || result.Segments[0].Text != "こんにちは" {
		t.Errorf("segments = %+v, want the piece inside the fence", result.Segments)
	}
}

// 服务失败报告为失败，绝不报成空译文；且引擎是独立翻译器，失败是用户所选引擎报出
// 的，不会转问其它引擎。
func TestBilibiliReportsItsOwnFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"rate limited", http.StatusTooManyRequests, domain.ErrProviderRateLimited},
		{"down", http.StatusInternalServerError, domain.ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asked := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"no"}`))
			}))
			defer server.Close()

			engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
			result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello."))
			if err == nil {
				t.Fatal("a failed engine answered")
			}
			if !errorsIs(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
			// 引擎只被问一次：没有可转交文本的池。
			if asked != 1 {
				t.Errorf("the endpoint was asked %d times, want once", asked)
			}
		})
	}
}

func TestTheEndpointsAreTheOnesTheDesignMeasured(t *testing.T) {
	if !strings.HasPrefix(microsoftEndpoint, "https://edge.microsoft.com/translate/translatetext?") {
		t.Errorf("endpoint = %q", microsoftEndpoint)
	}
	if !strings.Contains(microsoftEndpoint, "isEnterpriseClient=false") {
		t.Errorf("endpoint = %q, want the parameter Edge sends", microsoftEndpoint)
	}
	if !strings.HasPrefix(bilibiliEndpoint, "https://index-translate.bilibili.com/v1") {
		t.Errorf("endpoint = %q", bilibiliEndpoint)
	}
}

// 每个引擎通过自己的时钟给答复打时间戳，测试借此决定答复的发生时刻。
func TestTheEnginesStampTheirAnswers(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(microsoftAnswer))
	}))
	defer server.Close()

	engine := newMicrosoft(httpc.New(httpc.Options{}), server.URL+"?to=%s")
	engine.now = func() time.Time { return at }
	result, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("Hello."))
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if !result.CreatedAt.Equal(at) {
		t.Errorf("created at %s, want %s", result.CreatedAt, at)
	}
}

// OpenAI-compatible 引擎保持共享协议一致：它请求的 prompt 与付费后端相同，标注
// 依赖于此。
func TestBilibiliSendsTheToolOwnPrompt(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(chatAnswer("你好")))
	}))
	defer server.Close()

	engine := newBilibili(httpc.New(httpc.Options{}), server.URL, "m")
	if _, err := engine.Translate(t.Context(), domain.Credentials{}, plainRequest("こんにちは")); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %+v, want a system prompt and the text", messages)
	}
	system, _ := messages[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "翻译引擎") {
		t.Errorf("the system prompt is %q, want the tool's own", system)
	}
	user, _ := messages[1].(map[string]any)["content"].(string)
	if user != "こんにちは" {
		t.Errorf("the text sent is %q", user)
	}
}

// 确保 prompt 与解析器都来自 openai 包，使本测试文件不会偏离协议。
var _ = openai.DefaultMaxChars
