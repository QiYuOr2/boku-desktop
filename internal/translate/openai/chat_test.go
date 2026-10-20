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

// The annotation parser is what turns a model's answer into segments, and
// the promise it makes is that it never fails: whatever came back, the
// window can draw it.
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

	// The three rules of the annotation, as the mock writes them: a katakana
	// loanword carries the word it came from, a kanji word carries its
	// reading, and everything written in kana carries nothing at all —
	// whether or not the model filled a field in.
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
		// A particle is not annotated, even when the model says it is.
		for _, index := range []int{0, 3, 4} {
			if got.Segments[index].Reading != "" {
				t.Errorf("%q carries a reading it should not: %+v", got.Segments[index].Text, got.Segments[index])
			}
		}
		// A word with kanji keeps its reading, as one piece rather than one
		// per character.
		if got.Segments[1].Reading != "キカイゴ" {
			t.Errorf("機械語 lost its reading: %+v", got.Segments[1])
		}
		// A loanword keeps the word it came from, and drops the katakana the
		// model wrote as its reading — its own katakana says nothing.
		if got.Segments[2].Gloss != "mode" || got.Segments[2].Reading != "" {
			t.Errorf("モード should carry its own word and no reading: %+v", got.Segments[2])
		}
		if got.Segments[5].Gloss != "compiler" {
			t.Errorf("コンパイラ lost its gloss: %+v", got.Segments[5])
		}
	})

	t.Run("a gloss on a piece that is not a loanword", func(t *testing.T) {
		// A model that glosses everything: only a katakana piece has a word it
		// came from, and the window draws what a piece can have.
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
		// A model that ignores "每行一个对象" and answers one array is still
		// answering: read as lines, this whole answer is one unreadable line
		// and the annotations the user asked for are lost.
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
		// The user's own case: a model that answered prose and then broke its
		// own format across the answer. What is readable is shown; the braces
		// are not. The window shows a translation, not a wire format.
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
		// Nothing readable came back at all: the translation is left empty
		// rather than filled with braces, and the caller turns that into what
		// it says about an answer it could not use.
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
		// The parser never fails: a line it cannot read is text, and the
		// window draws it rather than losing half the answer.
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
		// A model that answers with the translation alone in an object: the
		// translation survives, and there is no segment to draw a reading
		// under.
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

// A request for readings that comes back without any is said out loud: the
// window draws a plain translation, and a user who asked for annotations has
// to be able to tell "the model did not send them" from "there are none".
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
	// A plain answer keeps the source's marks as well: the user asked for the
	// brackets of 【…】 to survive into the translation.
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
	// The two things the user asked for, as the model is told them: a
	// katakana loanword carries the word it came from, and the punctuation
	// gets pieces of its own so the translation keeps the source's brackets.
	// The third is the rule the mock draws: a reading belongs to a word with
	// kanji in it, per word and not per character, and nothing belongs above
	// a particle.
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
	// A loanword is annotated with the word it came from, not with its own
	// katakana: the example must not put a reading above モード.
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
	// The cut is on a character boundary: half a multi-byte character is a
	// broken request.
	long, cut := Truncate("日本語のテキストです", 4)
	if !cut {
		t.Error("a text longer than the limit was not reported as cut")
	}
	if long != "日本語の" {
		t.Errorf("Truncate = %q, want four characters", long)
	}
	// A limit of zero characters cuts everything and says so: the caller is
	// told to send nothing rather than half a request.
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

// The HTTP half is what every translation goes through: one request in the
// documented shape, one answer read out of it, and every way it can go wrong
// turned into a domain error.
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

	// A response that is the right shape but carries nothing is a bad
	// response, not an empty translation.
	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"   "}}]}`))
	}
	if _, err := Translate(context.Background(), opts); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("an empty answer = %v, want ErrProviderBadResponse", err)
	}

	// A response that is not the documented shape at all is the same error,
	// and it carries the endpoint rather than the key.
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

	// No choices at all.
	responder = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}
	if _, err := Translate(context.Background(), opts); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Errorf("no choices = %v, want ErrProviderBadResponse", err)
	}
}

// Every HTTP failure is translated before it reaches the caller: a rejected
// key is a key problem, a 5xx is a service problem, and neither is a raw
// status code in the window.
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
			// A service that echoes the credential back inside its error: the
			// fragment the error carries must not hold it.
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

// A response that is not JSON at all never reaches the caller as one: the
// HTTP client refuses it.
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

// A cancelled context stops the request rather than being waited out.
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

// A screenshot is read and translated in one model turn, which takes longer
// than an ordinary API call. The image request must not be cut off by the HTTP
// client's 10-second default: otherwise the stream stops mid-answer and the
// app reports its own deadline as a network timeout.
func TestImageRequestIsNotCappedByTheOrdinaryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"sourceLang\\\":\\\"en\\\",\\\"sourceText\\\":\\\"Hello\\\",\\\"targetLang\\\":\\\"zh\\\",\\\"translatedText\\\":\\\"你好\\\"}\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	// A client whose ordinary requests would give up long before the answer.
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

// A caller that needs a narrower deadline can still ask for one.
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
