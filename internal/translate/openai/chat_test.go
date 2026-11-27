package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// 标注解析器把模型回答转成片段，它承诺从不失败：无论返回什么，窗口都能画出。
func TestParseAnnotation(t *testing.T) {
	t.Run("one object per line", func(t *testing.T) {
		content := `{"s":"機械語","r":"キカイゴ","t":"机器语言"}
{"s":"は","t":"是"}
{"s":"コンパイラ","g":"compiler","t":"编译器"}`
		got := ParseAnnotation(content)
		if !got.Segmented {
			t.Fatal("an answer of JSON objects was not read as segments")
		}
		if len(got.Segments) != 3 {
			t.Fatalf("read %d segments, want 3", len(got.Segments))
		}
		if got.Segments[0].Reading != "キカイゴ" || got.Segments[2].Gloss != "compiler" {
			t.Errorf("the pieces lost their annotations: %+v", got.Segments)
		}
		if got.TranslatedText != "机器语言是编译器" {
			t.Errorf("translation = %q", got.TranslatedText)
		}
		if got.UnreadableLines != 0 {
			t.Errorf("unreadable lines = %d, want 0", got.UnreadableLines)
		}
		if joined := JoinSegments(got.Segments); joined != "機械語はコンパイラ" {
			t.Errorf("JoinSegments = %q, want the pieces packed the way Japanese is written", joined)
		}
	})

	// 标注三条规则在 mock 里的写法：片假名外来语带原词，汉字词带读音，假名写的一律
	// 不带任何标注——无论模型是否填了字段。
	t.Run("an annotation only where it belongs", func(t *testing.T) {
		content := strings.Join([]string{
			`{"s":"は","r":"ハ","t":"是"}`,
			`{"s":"機械語","r":"キカイゴ","t":"机器语言"}`,
			`{"s":"モード","r":"モード","g":"mode","t":"模式"}`,
			`{"s":"する","r":"スル","t":"做"}`,
			`{"s":"。","t":"。"}`,
			`{"s":"コンパイラ","g":"compiler","t":"编译器"}`,
		}, "\n")
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 6 {
			t.Fatalf("segments = %+v", got.Segments)
		}
		// 助词不标注，即便模型说它该标注。
		for _, index := range []int{0, 3, 4} {
			if got.Segments[index].Reading != "" {
				t.Errorf("%q carries a reading it should not: %+v", got.Segments[index].Text, got.Segments[index])
			}
		}
		// 含汉字的词保留读音，且按词而非按字给。
		if got.Segments[1].Reading != "キカイゴ" {
			t.Errorf("機械語 lost its reading: %+v", got.Segments[1])
		}
		// 外来语保留原词，丢弃模型写成读音的片假名——它自身的片假名说明不了什么。
		if got.Segments[2].Gloss != "mode" || got.Segments[2].Reading != "" {
			t.Errorf("モード should carry its own word and no reading: %+v", got.Segments[2])
		}
		if got.Segments[5].Gloss != "compiler" {
			t.Errorf("コンパイラ lost its gloss: %+v", got.Segments[5])
		}
	})

	t.Run("a gloss on a piece that is not a loanword", func(t *testing.T) {
		// 什么都注音的模型：只有片假名片段有原词，窗口只画片段能有的标注。
		got := ParseAnnotation(`{"s":"機械語","r":"キカイゴ","g":"machine language","t":"机器语言"}`)
		if !got.Segmented || len(got.Segments) != 1 {
			t.Fatalf("segments = %+v", got.Segments)
		}
		if got.Segments[0].Gloss != "" {
			t.Errorf("a kanji word kept a loanword gloss: %+v", got.Segments[0])
		}
		if got.Segments[0].Reading != "キカイゴ" {
			t.Errorf("a kanji word lost its reading: %+v", got.Segments[0])
		}
	})

	t.Run("a plain translation is not segmented", func(t *testing.T) {
		got := ParseAnnotation("编译器是将源代码转换成机器语言的软件。")
		if got.Segmented {
			t.Error("a plain sentence was read as segments")
		}
		if got.Segments != nil {
			t.Errorf("segments = %+v, want none", got.Segments)
		}
		if got.TranslatedText != "编译器是将源代码转换成机器语言的软件。" {
			t.Errorf("translation = %q", got.TranslatedText)
		}
	})

	t.Run("inside a code fence", func(t *testing.T) {
		content := "```json\n{\"s\":\"機械語\",\"r\":\"キカイゴ\",\"t\":\"机器语言\"}\n```"
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 1 {
			t.Fatalf("a fenced answer was not read: %+v", got)
		}
		if got.Segments[0].Reading != "キカイゴ" {
			t.Errorf("reading = %q", got.Segments[0].Reading)
		}
	})

	t.Run("an array of objects", func(t *testing.T) {
		content := "[\n{\"s\":\"機械語\",\"t\":\"机器语言\"},\n{\"s\":\"に\",\"t\":\"成\"}\n]"
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 2 {
			t.Fatalf("an array answer was not read: %+v", got)
		}
		if got.TranslatedText != "机器语言成" {
			t.Errorf("translation = %q", got.TranslatedText)
		}
	})

	t.Run("a compact array on one line", func(t *testing.T) {
		// 无视「每行一个对象」、改用一个数组作答的模型仍算在回答：按行读时整个回答是
		// 一行不可读内容，用户要的标注就会丢失。
		content := `[{"s":"モード","r":"モード","g":"mode","t":"模式"},{"s":"完備","r":"カンビ","t":"完备"}]`
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 2 {
			t.Fatalf("a compact array was not read: %+v", got)
		}
		if got.Segments[0].Gloss != "mode" {
			t.Errorf("the loanword lost its gloss: %+v", got.Segments[0])
		}
		if got.TranslatedText != "模式完备" {
			t.Errorf("translation = %q", got.TranslatedText)
		}
		if got.UnreadableLines != 0 {
			t.Errorf("unreadable lines = %d, want 0", got.UnreadableLines)
		}
	})

	t.Run("a fenced array on one line", func(t *testing.T) {
		content := "```json\n[{\"s\":\"回想\",\"r\":\"カイソウ\",\"t\":\"回想\"},{\"s\":\"完備\",\"t\":\"完备\"}]\n```"
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 2 {
			t.Fatalf("a fenced array was not read: %+v", got)
		}
		if got.TranslatedText != "回想完备" {
			t.Errorf("translation = %q", got.TranslatedText)
		}
	})

	t.Run("the service's own format is never shown", func(t *testing.T) {
		// 用户的真实用例：模型先答散文，随后把自身格式打散在回答里。可读的显示，花括号
		// 不显示。窗口显示的是译文，不是线格式。
		content := `独当一面的魅魔成为了、人界降临莉莉娅！
{"s":"\n","t":"\n"}
{"s":"n","t":"n"}主线任务把推进一边、N
["s":"半端`
		got := ParseAnnotation(content)
		for _, piece := range []string{"{", "}", "[", `"s"`, `"t"`} {
			if strings.Contains(got.TranslatedText, piece) {
				t.Errorf("the wire format is in the translation (%q): %q", piece, got.TranslatedText)
			}
		}
		if !strings.Contains(got.TranslatedText, "独当一面的魅魔") {
			t.Errorf("the readable line was dropped: %q", got.TranslatedText)
		}
		if got.UnreadableLines == 0 {
			t.Error("the lines that could not be read were not counted")
		}
	})

	t.Run("an answer that is only its own format", func(t *testing.T) {
		// 完全没读到可读内容：译文留空而非填以花括号，调用方把它转成「无法使用此回答」
		// 的提示。
		content := `{"s": "one", "t": "一",
{"s":`
		got := ParseAnnotation(content)
		if got.TranslatedText != "" {
			t.Errorf("translation = %q, want nothing to show", got.TranslatedText)
		}
		if got.Segmented {
			t.Error("an unreadable answer was reported as segmented")
		}
	})

	t.Run("a malformed line is kept as text", func(t *testing.T) {
		// 解析器从不失败：读不了的行当作文本，窗口画出来，而非丢掉半个回答。
		content := "{\"s\":\"機械語\",\"r\":\"キカイゴ\",\"t\":\"机器语言\"}\n抱歉，我无法处理这一行\n{\"s\":\"です\",\"t\":\"是\"}"
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 2 {
			t.Fatalf("segments = %+v", got.Segments)
		}
		if got.UnreadableLines != 1 {
			t.Errorf("unreadable lines = %d, want 1", got.UnreadableLines)
		}
		if !strings.Contains(got.TranslatedText, "抱歉") {
			t.Errorf("the unreadable line was dropped from the translation: %q", got.TranslatedText)
		}
	})

	t.Run("an object with no source piece", func(t *testing.T) {
		// 只在对象里给出译文的模型：译文保留，但没有片段可承载读音。
		content := "{\"t\":\"编译器\"}\n{\"s\":\"機械語\",\"t\":\"机器语言\"}"
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 1 {
			t.Fatalf("segments = %+v", got.Segments)
		}
		if got.TranslatedText != "编译器机器语言" {
			t.Errorf("translation = %q, want the object without a source kept", got.TranslatedText)
		}
		if got.UnreadableLines != 1 {
			t.Errorf("unreadable lines = %d, want 1", got.UnreadableLines)
		}
	})

	t.Run("spelled-out field names", func(t *testing.T) {
		content := `{"text":"機械語","reading":"キカイゴ","translation":"机器语言"}
{"text":"モード","gloss":"mode","translation":"模式"}`
		got := ParseAnnotation(content)
		if !got.Segmented || len(got.Segments) != 2 {
			t.Fatalf("a spelled-out answer was not read: %+v", got)
		}
		if got.Segments[0].Reading != "キカイゴ" {
			t.Errorf("a spelled-out reading was not read: %+v", got.Segments[0])
		}
		if got.Segments[1].Gloss != "mode" {
			t.Errorf("a spelled-out gloss was not read: %+v", got.Segments[1])
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := ParseAnnotation("   ")
		if got.Segmented || got.TranslatedText != "" {
			t.Errorf("ParseAnnotation(blank) = %+v", got)
		}
	})

	t.Run("latin pieces keep their spaces", func(t *testing.T) {
		content := "{\"s\":\"A\",\"t\":\"一个\"}\n{\"s\":\"compiler\",\"t\":\"编译器\"}"
		got := ParseAnnotation(content)
		if !got.Segmented {
			t.Fatal("not segmented")
		}
		if got.TranslatedText != "一个编译器" {
			t.Errorf("translation = %q, want the two pieces joined", got.TranslatedText)
		}
		if joined := JoinSegments(got.Segments); joined != "A compiler" {
			t.Errorf("JoinSegments = %q, want a space between two Latin pieces", joined)
		}
	})
}

// 要求了读音却一条都没回来的情况要说出来：窗口画纯译文，要标注的用户得能分辨「模型没发」
// 与「本来就没有」。
func TestAnnotationNote(t *testing.T) {
	wanting := AnnotationOptions{Furigana: true, LoanwordGloss: true}
	plain := Annotation{TranslatedText: "完备回想模式"}
	if note := AnnotationNote(plain, wanting); note == "" {
		t.Error("an answer with no segments was not reported")
	}
	if note := AnnotationNote(plain, AnnotationOptions{}); note != "" {
		t.Errorf("a plain translation was reported as a missing annotation: %q", note)
	}
	segmented := Annotation{Segmented: true}
	if note := AnnotationNote(segmented, wanting); note != "" {
		t.Errorf("a fully read answer was reported: %q", note)
	}
	partly := Annotation{Segmented: true, UnreadableLines: 2}
	if note := AnnotationNote(partly, wanting); !strings.Contains(note, "2") {
		t.Errorf("the unreadable lines are not counted in %q", note)
	}
}

func TestSystemPrompt(t *testing.T) {
	plain := SystemPrompt(domain.LangEnglish, domain.LangChinese, AnnotationOptions{})
	if strings.Contains(plain, "JSON") {
		t.Errorf("the plain prompt asks for annotations:\n%s", plain)
	}
	if !strings.Contains(plain, "中文") || !strings.Contains(plain, "English") {
		t.Errorf("the plain prompt does not name the languages:\n%s", plain)
	}
	// 纯回答同样保留原文标点：用户要求【…】的括号保留到译文里。
	for _, want := range []string{"括号", "【】", "标点"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the plain prompt does not ask for %s:\n%s", want, plain)
		}
	}

	annotated := SystemPrompt(domain.LangJapanese, domain.LangChinese, AnnotationOptions{Furigana: true, LoanwordGloss: true})
	for _, want := range []string{`"s"`, `"t"`, `"r"`, `"g"`, "カイソウ", "mode"} {
		if !strings.Contains(annotated, want) {
			t.Errorf("the annotated prompt does not mention %s:\n%s", want, annotated)
		}
	}
	// 用户对模型提出的两件事：片假名外来语带原词，标点单独成片段以使译文保留原文括号。
	// 第三件是 mock 画出的规则：读音属于含汉字的词、按词而非按字给，助词上方什么都没有。
	for _, want := range []string{
		`"s":"モード"`,
		`"g":"mode"`,
		"写出它来源语言的单词",
		"不要拆成单个汉字",
		"平假名",
		`"s":"【","t":"【"`,
		"拼起来，必须与原文完全一致",
		"译文必须保留原文的格式与标点",
	} {
		if !strings.Contains(annotated, want) {
			t.Errorf("the annotated prompt does not say %s:\n%s", want, annotated)
		}
	}
	// 外来语以原词标注，而非它自身的片假名：示例不得在 モード 上方给读音。
	if strings.Contains(annotated, `{"s":"モード","r"`) {
		t.Errorf("the example gives a loanword a reading of its own:\n%s", annotated)
	}

	furiganaOnly := SystemPrompt(domain.LangJapanese, domain.LangChinese, AnnotationOptions{Furigana: true})
	if strings.Contains(furiganaOnly, `"g"`) {
		t.Errorf("the furigana-only prompt asks for glosses:\n%s", furiganaOnly)
	}
	if !(AnnotationOptions{Furigana: true}).Wants() {
		t.Error("furigana alone does not want an annotation")
	}
	if (AnnotationOptions{}).Wants() {
		t.Error("nothing wants an annotation")
	}
}

func TestTruncate(t *testing.T) {
	short, cut := Truncate("  hello  ", 10)
	if cut || short != "hello" {
		t.Errorf("Truncate(%q, 10) = %q, %v", "  hello  ", short, cut)
	}
	// 截断在字符边界上：半个多字节字符是一个坏请求。
	long, cut := Truncate("日本語のテキストです", 4)
	if !cut {
		t.Error("a text longer than the limit was not reported as cut")
	}
	if long != "日本語の" {
		t.Errorf("Truncate = %q, want four characters", long)
	}
	// 上限为 0 时全部截掉并报告：调用方被告知不要发送，而非发半个请求。
	empty, cut := Truncate("原文", 0)
	if !cut || empty != "" {
		t.Errorf("Truncate(text, 0) = %q, %v, want it cut to nothing", empty, cut)
	}
	if blank, cut := Truncate("   ", 0); cut || blank != "" {
		t.Errorf("Truncate(blank, 0) = %q, %v, want nothing to cut", blank, cut)
	}
}

func TestEndpointOf(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/v1":  "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/": "https://api.example.com/v1/chat/completions",
		"":                            "",
		"   ":                         "",
	}
	for in, want := range cases {
		if got := EndpointOf(in); got != want {
			t.Errorf("EndpointOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// HTTP 这一半是所有翻译必经之路：一次按文档形状的请求、从中读出一个回答，把每种出错
// 都转成域错误。
func TestTranslateReadsTheAnswer(t *testing.T) {
	var (
		gotPath   string
		gotAuth   string
		gotBody   map[string]any
		responder func(w http.ResponseWriter)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		responder(w)
	}))
	defer server.Close()

	client := httpc.New(httpc.Options{})
	opts := Options{
		Client:  client,
		BaseURL: server.URL + "/v1",
		Model:   "some-model",
		APIKey:  "sk-secret-value",
		System:  "translate this",
		Target:  domain.LangChinese,
		Text:    "hello",
	}

	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好"}}]}`))
	}
	content, err := Translate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if content != "你好" {
		t.Errorf("content = %q", content)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-secret-value" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotBody["model"] != "some-model" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if messages, ok := gotBody["messages"].([]any); !ok || len(messages) != 2 {
		t.Errorf("messages = %v, want a system and a user message", gotBody["messages"])
	}

	// 形状正确却内容为空的响应是坏响应，而非空译文。
	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"   "}}]}`))
	}
	if _, err := Translate(context.Background(), opts); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("an empty answer = %v, want ErrProviderBadResponse", err)
	}

	// 完全不是文档形状的响应是同一错误，且错误带端点而非密钥。
	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"result":"你好","apiKey":"sk-secret-value"}`))
	}
	_, err = Translate(context.Background(), opts)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("an unknown shape = %v, want ErrProviderBadResponse", err)
	}
	if strings.Contains(err.Error(), "sk-secret-value") {
		t.Errorf("the error carries the credential: %v", err)
	}
	var bad *domain.BadResponseError
	if !errors.As(err, &bad) || bad.Endpoint == "" {
		t.Errorf("the error does not name the endpoint it asked: %v", err)
	}
	if len(bad.Snippet) > domain.MaxSnippetBytes {
		t.Errorf("the snippet is %d bytes", len(bad.Snippet))
	}

	// 一个 choice 都没有。
	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}
	if _, err := Translate(context.Background(), opts); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("no choices = %v, want ErrProviderBadResponse", err)
	}
}

// 每种 HTTP 失败在到达调用方前都被翻译：被拒的密钥是密钥问题，5xx 是服务问题，都不该在
// 窗口里显示原始状态码。
func TestTranslateTranslatesStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, domain.ErrProviderUnauthorized},
		{http.StatusForbidden, domain.ErrProviderUnauthorized},
		{http.StatusTooManyRequests, domain.ErrProviderRateLimited},
		{http.StatusInternalServerError, domain.ErrProviderUnavailable},
		{http.StatusBadGateway, domain.ErrProviderUnavailable},
		{http.StatusBadRequest, domain.ErrProviderBadResponse},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			// 在错误里回显凭据的服务：错误携带的片段绝不能含有它。
			_, _ = w.Write([]byte(`{"error":{"message":"the request failed","key":"sk-live-9f8e7d6c5b4a"}}`))
		}))
		_, err := Translate(context.Background(), Options{
			Client:  httpc.New(httpc.Options{}),
			BaseURL: server.URL,
			Model:   "m",
			APIKey:  "sk-live-9f8e7d6c5b4a",
			Target:  domain.LangChinese,
			Text:    "hello",
		})
		server.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d = %v, want %v", tc.status, err, tc.want)
		}
		if strings.Contains(err.Error(), "sk-live-9f8e7d6c5b4a") {
			t.Errorf("status %d: the error carries the credential: %v", tc.status, err)
		}
	}
}

// 根本不是 JSON 的响应绝不会以此形态到达调用方：HTTP 客户端拒收它。
func TestTranslateRefusesANonJSONAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>gateway timeout</html>`))
	}))
	defer server.Close()
	_, err := Translate(context.Background(), Options{
		Client:  httpc.New(httpc.Options{}),
		BaseURL: server.URL,
		Model:   "m",
		Target:  domain.LangChinese,
		Text:    "hello",
	})
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("a non-JSON answer = %v, want ErrProviderBadResponse", err)
	}
}

func TestTranslateRefusesWhatItCannotSend(t *testing.T) {
	client := httpc.New(httpc.Options{})
	cases := []struct {
		name string
		opts Options
	}{
		{"no client", Options{BaseURL: "https://example.com", Model: "m", Target: domain.LangChinese, Text: "hello"}},
		{"no address", Options{Client: client, Model: "m", Target: domain.LangChinese, Text: "hello"}},
		{"no model", Options{Client: client, BaseURL: "https://example.com", Target: domain.LangChinese, Text: "hello"}},
		{"no text", Options{Client: client, BaseURL: "https://example.com", Model: "m", Target: domain.LangChinese}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Translate(context.Background(), tc.opts); !errors.Is(err, domain.ErrInvalidArgument) {
				t.Errorf("Translate = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// 已取消的 context 会中止请求，而非等它结束。
func TestTranslateHonoursCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Translate(ctx, Options{
		Client:  httpc.New(httpc.Options{}),
		BaseURL: server.URL,
		Model:   "m",
		Target:  domain.LangChinese,
		Text:    "hello",
	})
	if err == nil {
		t.Fatal("a cancelled request succeeded")
	}
	if !errors.Is(err, domain.ErrProviderTimeout) && !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled request = %v", err)
	}
}

// 一张截图在一次模型回合内完成识别与翻译，比普通 API 调用久。图片请求不能被 HTTP 客户端
// 10 秒默认截断：否则流会在回答中途停下，应用便把自己的期限报成网络超时。
func TestImageRequestIsNotCappedByTheOrdinaryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"status\\\":\\\"success\\\",\\\"sourceLang\\\":\\\"en\\\",\\\"sourceText\\\":\\\"Hello\\\",\\\"targetLang\\\":\\\"zh\\\",\\\"translatedText\\\":\\\"你好\\\",\\\"failure\\\":null}\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	// 一个普通请求会远早于答案就放弃的客户端。
	client := httpc.New(httpc.Options{Timeout: 20 * time.Millisecond})
	if _, err := TranslateImage(context.Background(), ImageOptions{
		Client:  client,
		BaseURL: server.URL,
		Model:   "vision",
		Image:   []byte{0x89, 'P', 'N', 'G'},
		System:  "translate",
		Target:  domain.LangChinese,
	}); err != nil {
		t.Fatalf("the image request used the client's ordinary timeout: %v", err)
	}
}

// 需要更短期限的调用方仍可自行指定。
func TestImageRequestHonoursANarrowerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	_, err := TranslateImage(context.Background(), ImageOptions{
		Client:  httpc.New(httpc.Options{}),
		BaseURL: server.URL,
		Model:   "vision",
		Image:   []byte{0x89, 'P', 'N', 'G'},
		System:  "translate",
		Target:  domain.LangChinese,
		Timeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, domain.ErrProviderTimeout) {
		t.Fatalf("a narrower image timeout = %v, want ErrProviderTimeout", err)
	}
}

// 截图的思考链比普通 JSON 体大。图片流必须接受它，否则卡片会把这个上限报成无法读取的响应。
func TestImageStreamIsNotCappedByTheOrdinaryResponseLimit(t *testing.T) {
	event := "data: {\"choices\":[{\"delta\":{\"content\":null,\"reasoning_content\":\"x\"}}]}\n\n"
	var body strings.Builder
	for body.Len() <= httpc.DefaultMaxBytes {
		body.WriteString(event)
	}
	body.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"status\\\":\\\"success\\\",\\\"sourceLang\\\":\\\"en\\\",\\\"sourceText\\\":\\\"Hi\\\",\\\"targetLang\\\":\\\"zh\\\",\\\"translatedText\\\":\\\"嗨\\\",\\\"failure\\\":null}\"}}]}\n\ndata: [DONE]\n\n")
	payload := body.String()
	if len(payload) >= ImageMaxBytes {
		t.Fatalf("fixture is %d bytes, want it under the image ceiling", len(payload))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()

	content, err := TranslateImage(context.Background(), ImageOptions{
		Client:  httpc.New(httpc.Options{}),
		BaseURL: server.URL,
		Model:   "vision",
		Image:   []byte{0x89, 'P', 'N', 'G'},
		System:  "translate",
		Target:  domain.LangChinese,
	})
	if err != nil {
		t.Fatalf("a stream above the ordinary ceiling = %v", err)
	}
	if !strings.Contains(content, `"translatedText":"嗨"`) {
		t.Errorf("content = %q", content)
	}
}
