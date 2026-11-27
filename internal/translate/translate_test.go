package translate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	contractfree "boku/internal/contract/freetranslate"
	"boku/internal/domain"
)

// translate 服务是工具的规则：用哪个翻译器、什么都没配时如何、文本翻成哪种语言，
// 以及用户自有服务配置了什么。它在 fake 上测试，无网络也无窗口。

// fakeTranslator 是答案由测试决定的翻译器。
type fakeTranslator struct {
	id         string
	descriptor domain.TranslatorDescriptor
	// baseURL 是翻译器指向用户自有服务时读取地址的来源（当描述符说地址是设置）。
	// 真实实现读同一份设置，用例必须分辨不出差别。
	baseURL func() string

	mu       sync.Mutex
	calls    int
	requests []domain.TranslationRequest
	result   domain.TranslationResult
	err      error
	gate     chan struct{}
}

func newFakeTranslator(id, displayName string) *fakeTranslator {
	return &fakeTranslator{
		id: id,
		descriptor: domain.TranslatorDescriptor{
			ID:             id,
			DisplayName:    displayName,
			CredentialKeys: []string{"apiKey"},
			DocsURL:        "https://example.com/keys",
			Endpoint:       "https://api.example.com/v1/chat/completions",
		},
		result: domain.TranslationResult{
			SourceText:     "hello",
			TranslatedText: "你好",
			SourceLang:     domain.LangEnglish,
			TargetLang:     domain.LangChinese,
			ProviderID:     id,
			CreatedAt:      time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
		},
	}
}

func (f *fakeTranslator) ID() string { return f.id }

func (f *fakeTranslator) Descriptor() domain.TranslatorDescriptor {
	descriptor := f.descriptor
	if descriptor.BaseURLSetting != "" && f.baseURL != nil {
		// 地址按真实翻译器的方式从设置计算得出。
		if base := f.baseURL(); base != "" {
			descriptor.Endpoint = base + "/chat/completions"
		}
	}
	return descriptor
}

func (f *fakeTranslator) Translate(ctx context.Context, creds domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	f.mu.Lock()
	f.calls++
	f.requests = append(f.requests, req)
	gate, err, result := f.gate, f.err, f.result
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return domain.TranslationResult{}, ctx.Err()
		}
	}
	if err != nil {
		return domain.TranslationResult{}, err
	}
	result.SourceText = req.Text
	result.SourceLang = req.SourceLang
	result.TargetLang = req.TargetLang
	result.ProviderID = f.id
	return result, nil
}

func (f *fakeTranslator) TranslateImage(ctx context.Context, creds domain.Credentials, image []byte, target domain.Language) (domain.TranslationResult, error) {
	if len(image) == 0 {
		return domain.TranslationResult{}, domain.ErrInvalidArgument
	}
	return f.Translate(ctx, creds, domain.TranslationRequest{Text: "image probe", SourceLang: domain.LangEnglish, TargetLang: target})
}

func (f *fakeTranslator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeTranslator) lastRequest() domain.TranslationRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return domain.TranslationRequest{}
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeTranslator) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeTranslator) hold() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = make(chan struct{})
	return f.gate
}

// fakeTranslators 是基于 fake 的 domain.TranslatorRegistry。
type fakeTranslators struct {
	byID map[string]*fakeTranslator
}

func newFakeTranslators(replies ...*fakeTranslator) *fakeTranslators {
	registry := &fakeTranslators{byID: map[string]*fakeTranslator{}}
	for _, reply := range replies {
		registry.byID[reply.id] = reply
	}
	return registry
}

func (r *fakeTranslators) Translator(id string) (domain.Translator, error) {
	reply, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
	}
	return reply, nil
}

// TranslatorIDs 返回按序排好的已注册 id，与真实注册表完全一致：设置窗口按此顺序
// 绘制行，而按 map 顺序返回的 fake 会让测试通过，实际列表却在帧间自行重排。
func (r *fakeTranslators) TranslatorIDs() []string {
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// translateFixture 是在 fake 上构建的服务，附带凭据存储与测试可查看的设置。
type translateFixture struct {
	translate *Translate
	device    *fakeDevice
	selection *fakeSelection
	tools     *fakeTools
	secrets   *memoryStore
	deepseek  *fakeTranslator
	custom    *fakeTranslator
	microsoft *fakeTranslator
	bilibili  *fakeTranslator
	clock     *testClock
}

// fakeSelection 是平台对「选中了什么」的回答，由测试决定：文本、空、或平台无法作答。
type fakeSelection struct {
	mu     sync.Mutex
	text   string
	err    error
	asked  int
	answer func() (string, error)
}

func (s *fakeSelection) SelectedText(context.Context) (string, error) {
	s.mu.Lock()
	s.asked++
	answer, text, err := s.answer, s.text, s.err
	s.mu.Unlock()
	if answer != nil {
		return answer()
	}
	return text, err
}

// asks 是平台被问及选中内容的次数。
func (s *fakeSelection) asks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked
}

// newTranslate 构建 fixture，其中已为 deepseek 存好 key，即用户配置完面板后到达的状态。
func newTranslate(t *testing.T) *translateFixture {
	t.Helper()
	deepseek := newFakeTranslator("deepseek", "DeepSeek")
	customReply := newFakeTranslator("custom", "自定义")
	customReply.descriptor.BaseURLSetting = "custom.baseURL"
	customReply.descriptor.Endpoint = ""
	customReply.descriptor.AllowEmptyCredential = true
	// 免费引擎：全新安装的起始状态，因此这些测试必须像正式构建那样覆盖它们——
	// 两个不需要任何凭据的翻译器。
	microsoft := newFakeTranslator(contractfree.MicrosoftEdge, "微软 Edge 翻译")
	microsoft.descriptor.AllowEmptyCredential = true
	microsoft.descriptor.DocsURL = "https://example.com/free"
	microsoft.descriptor.Endpoint = "edge.microsoft.com/translate/translatetext"
	bilibili := newFakeTranslator(contractfree.Bilibili, "B站 Index-Translate")
	bilibili.descriptor.AllowEmptyCredential = true
	bilibili.descriptor.DocsURL = "https://example.com/free"
	bilibili.descriptor.Endpoint = "index-translate.bilibili.com/v1/chat/completions"

	device := &fakeDevice{}
	// 设置从全新安装的样子开始：文件没有该键时存储会填入翻译默认值，
	// 所以这个 fake 充当已读过一次的存储。
	tools := newFakeTools()
	// 用户自有的翻译器从这些设置读取地址。
	customReply.baseURL = func() string {
		service, _ := tools.section.Current().CustomService(customReply.id)
		return service.BaseURL
	}
	secrets := newMemoryStore()
	if err := secrets.Put(context.Background(),
		domain.CredentialKey("deepseek", "apiKey"), []byte("sk-test-0001"), domain.SecretMeta{}); err != nil {
		t.Fatalf("store the credential: %v", err)
	}
	clock := newTestClock()
	selection := &fakeSelection{err: domain.ErrSelectionUnavailable}
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(deepseek, customReply, microsoft, bilibili),
		Secrets:     secrets,
		Section:     tools.section,
		Settings:    tools,
		Selection:   selection,
		Clipboard:   device,
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	return &translateFixture{
		translate: service,
		device:    device,
		selection: selection,
		tools:     tools,
		secrets:   secrets,
		deepseek:  deepseek,
		custom:    customReply,
		microsoft: microsoft,
		bilibili:  bilibili,
		clock:     clock,
	}
}

// useDeepSeek 选用付费翻译器，即用户配置了自己的 key 后到达的状态。关于凭据、
// 或关于失败服务的测试都针对该翻译器并明说：默认是免费引擎，假设相反的测试
// 就是在测错的东西。
func useDeepSeek(t *testing.T, f *translateFixture) {
	t.Helper()
	if err := f.translate.SetProvider(context.Background(), "deepseek"); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
}

// source 读取用户所选的内容，方式同窗口在卡片接管键盘前：先问平台，平台答不了时问剪贴板。
func source(t *testing.T, f *translateFixture) string {
	t.Helper()
	text, err := f.translate.Source(context.Background())
	if err != nil {
		t.Fatalf("Source failed: %v", err)
	}
	return text
}

func TestTranslateNormal(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	f.device.set(textReading("A compiler converts source code."))
	result, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.TranslatedText != "你好" {
		t.Errorf("translation = %q", result.TranslatedText)
	}
	if result.SourceText != "A compiler converts source code." {
		t.Errorf("source = %q", result.SourceText)
	}
	if result.ProviderID != "deepseek" || result.ProviderName != "DeepSeek" {
		t.Errorf("provider = %q / %q", result.ProviderID, result.ProviderName)
	}
	if result.SourceLang != domain.LangEnglish || result.TargetLang != domain.LangChinese {
		t.Errorf("languages = %q -> %q", result.SourceLang, result.TargetLang)
	}
	if result.At.IsZero() {
		t.Error("the result has no time")
	}
	if f.deepseek.count() != 1 {
		t.Errorf("the translator was asked %d times", f.deepseek.count())
	}
	// 剪贴板只回答一次，且仅一次：请求前的那次读取。请求本身不向任何设备发问。
	if f.device.readCount() != 1 {
		t.Errorf("the clipboard was read %d times", f.device.readCount())
	}
}

// 用户选中的内容就是要翻译的内容：先问平台，而剪贴板——存着最后一次复制的内容，
// 可能完全是别的东西——在平台能作答时完全不查。
func TestSourcePrefersTheSelection(t *testing.T) {
	f := newTranslate(t)
	f.device.set(textReading("what was copied an hour ago"))
	f.selection.err, f.selection.text = nil, "コンパイラはソフトです。"

	if got := source(t, f); got != "コンパイラはソフトです。" {
		t.Errorf("source = %q", got)
	}
	if f.selection.asks() != 1 {
		t.Errorf("the platform was asked %d times what is selected", f.selection.asks())
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read although the selection answered")
	}
}

// 无法说明选中内容的平台、说没有选中任何东西的、调用失败的，以及根本没有该设备的
// 构建，对用户而言都是同一件事：由剪贴板作答，如同还没有可读选中内容时那样。
func TestSourceFallsBackToTheClipboard(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*translateFixture)
	}{
		{"the platform cannot say", func(f *translateFixture) {
			f.selection.err = domain.ErrSelectionUnavailable
		}},
		{"the platform says nothing is selected", func(f *translateFixture) {
			f.selection.err, f.selection.text = nil, "   "
		}},
		{"the platform call failed", func(f *translateFixture) {
			f.selection.err = errors.New("the provider answered nothing")
		}},
		{"there is no selection device", func(f *translateFixture) {
			f.translate.selection = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslate(t)
			f.device.set(textReading("A compiler converts source code."))
			tc.prepare(f)

			if got := source(t, f); got != "A compiler converts source code." {
				t.Errorf("source = %q", got)
			}
			if f.device.readCount() != 1 {
				t.Errorf("the clipboard was read %d times", f.device.readCount())
			}
		})
	}
}

// 完全无法读取的剪贴板是故障而非空答案：窗口显示故障，而不是让用户去复制可能
// 早已复制过的东西。
func TestSourceReportsAClipboardThatCannotBeRead(t *testing.T) {
	f := newTranslate(t)
	f.device.fail(errors.New("the clipboard is held by another application"))
	if _, err := f.translate.Source(context.Background()); err == nil {
		t.Fatal("Source answered nothing for a clipboard it could not read")
	}
}

// 自带文本的请求不向平台发问：调用者手上的文本就是要翻译的文本。
func TestTranslateDoesNotAskForASelectionWithItsOwnText(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	if _, err := f.translate.Translate(context.Background(), Request{Text: "hello"}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if f.selection.asks() != 0 {
		t.Errorf("the platform was asked %d times for a request with its own text", f.selection.asks())
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read %d times for a request with its own text", f.device.readCount())
	}
}

// 中文选中内容被翻成英语，而不是走进死胡同；语言由文本本身决定。
func TestTranslatePicksTheTargetFromTheSource(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	result, err := f.translate.Translate(context.Background(), Request{Text: "编译器把源代码转换成机器语言。"})
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result.SourceLang != domain.LangChinese {
		t.Errorf("source language = %q", result.SourceLang)
	}
	if result.TargetLang != domain.LangEnglish {
		t.Errorf("target language = %q, want the other direction", result.TargetLang)
	}
	if f.device.readCount() != 0 {
		t.Errorf("the clipboard was read for a request that carried its own text")
	}
}

// 注释只由需要它的请求索取：翻译请求发出时只要段落，开关决定的是「要词」的请求
// 索取什么。
func TestTheAnnotationIsItsOwnRequest(t *testing.T) {
	f := newTranslate(t)
	f.tools.section.Set(Defaults())
	useDeepSeek(t, f)

	// 翻译请求不索取注释，无论开关如何。
	if err := f.translate.SetFurigana(context.Background(), true); err != nil {
		t.Fatalf("SetFurigana failed: %v", err)
	}
	if _, err := f.translate.Translate(context.Background(), Request{Text: "コンパイラ"}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if request := f.deepseek.lastRequest(); request.Annotation.Wants() {
		t.Errorf("the translation asked for the annotation: %+v", request.Annotation)
	}

	// 要词的请求携带开关，正是这让开关有了意义。
	if err := f.translate.SetLoanwordGloss(context.Background(), false); err != nil {
		t.Fatalf("SetLoanwordGloss failed: %v", err)
	}
	if _, err := f.translate.Translate(context.Background(), Request{Text: "コンパイラ", Annotate: true}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	request := f.deepseek.lastRequest()
	if !request.Annotation.Furigana || request.Annotation.LoanwordGloss {
		t.Errorf("the annotation request does not carry the switches: %+v", request.Annotation)
	}
	if request.Text != "コンパイラ" {
		t.Errorf("the annotation request is for %q", request.Text)
	}
	if request.SourceLang != domain.LangJapanese {
		t.Errorf("source language = %q", request.SourceLang)
	}
}

// 用户复制的换行在请求中保留：Windows 以 \r\n 交出选中内容，窗口绘制的段落按 \n
// 切分，回车符和丢失的行都不是要翻译的东西。
func TestTheCopiedLineBreaksSurvive(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	text := "一行目\r\n二行目\r三行目\n\n四行目"
	if _, err := f.translate.Translate(context.Background(), Request{Text: text}); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	want := "一行目\n二行目\n三行目\n\n四行目"
	if got := f.deepseek.lastRequest().Text; got != want {
		t.Errorf("the text sent is %q, want %q", got, want)
	}
}

// 关闭的工具会说明已关闭，且不读剪贴板、不问服务。
//
// 它是独立的错误而非「翻译不可用」：二者对用户是不同的话，窗口绘制方式也不同。
// 关闭不是故障——是用户要求的；而无处发送文本的工具是需要用户修复的。
func TestTranslateWhenTheToolIsOff(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SetEnabled(context.Background(), false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrTranslateDisabled) {
		t.Fatalf("error = %v, want ErrTranslateDisabled", err)
	}
	// 两者不同：只处理未配置情况的调用者不能捕获到这一个。
	if errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Error("a switched-off tool is reported as an unconfigured one")
	}
	if !strings.Contains(err.Error(), "已关闭") {
		t.Errorf("the error does not say the tool is off: %v", err)
	}
	if f.deepseek.count() != 0 || f.device.readCount() != 0 {
		t.Error("a switched-off tool did work")
	}
}

// 设置指向本版本没有的翻译器的工具会说明是哪个，好让用户知道该改选什么。
func TestTranslateWithNothingConfigured(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.SaveSettings(context.Background(), func(s *Settings) {
		s.Provider = "nobody"
	}); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Fatalf("error = %v, want ErrTranslateUnavailable", err)
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("the error does not name the translator that is missing: %v", err)
	}
	if !strings.Contains(err.Error(), "设置") {
		t.Errorf("the error does not say where to fix it: %v", err)
	}
}

// 缺少凭据是独立答案：没有 key 就不值得发请求。
func TestTranslateWithoutACredential(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("the translator was asked without a credential")
	}
	// 错误只命名凭据，绝不包含其值。
	if !strings.Contains(err.Error(), "deepseek.apiKey") {
		t.Errorf("the error does not name the credential: %v", err)
	}
}

// 没有选中内容、剪贴板也没有可用内容，是用户需自行处理的问题，有专门的措辞：
// 用户得先选中或复制点什么。
func TestTranslateWithNothingToTranslate(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	_, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "复制") {
		t.Errorf("the error does not tell the user what to do: %v", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("an empty clipboard reached the translator")
	}
}

// 剪贴板里是文件列表或图片时，不是可翻译的文本。
func TestTranslateWithANonTextClipboard(t *testing.T) {
	for _, reading := range []domain.ClipContent{
		{Kind: domain.ClipKindFile, Files: []string{"/tmp/a.txt"}},
		{Kind: domain.ClipKindImage, ImageBytes: 1024},
	} {
		f := newTranslate(t)
		useDeepSeek(t, f)
		f.device.set(reading)
		_, err := f.translate.Translate(context.Background(), Request{Text: source(t, f)})
		if !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("kind %s = %v, want ErrInvalidArgument", reading.Kind, err)
		}
		if f.deepseek.count() != 0 {
			t.Errorf("kind %s reached the translator", reading.Kind)
		}
	}
}

// 失败的服务报告为失败，绝不报告为空翻译。
func TestTranslateReportsServiceFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"unauthorized", fmt.Errorf("%w: deepseek.apiKey was refused", domain.ErrProviderUnauthorized)},
		{"rate limited", fmt.Errorf("%w: too many requests", domain.ErrProviderRateLimited)},
		{"unavailable", fmt.Errorf("%w: no answer", domain.ErrProviderUnavailable)},
		{"timeout", fmt.Errorf("%w: took too long", domain.ErrProviderTimeout)},
		{"bad response", fmt.Errorf("%w: the answer is not JSON", domain.ErrProviderBadResponse)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslate(t)
			useDeepSeek(t, f)
			f.deepseek.fail(tc.err)
			result, err := f.translate.Translate(context.Background(), Request{Text: "hello"})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if result.TranslatedText != "" {
				t.Errorf("a failure produced a translation: %+v", result)
			}
		})
	}
}

// 取消的 context 会终止请求：关掉窗口的用户不应干等服务。
func TestTranslateHonoursCancellation(t *testing.T) {
	f := newTranslate(t)
	useDeepSeek(t, f)
	gate := f.deepseek.hold()
	defer close(gate)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.translate.Translate(ctx, Request{Text: "hello"})
		done <- err
	}()
	waitFor(t, "the translator to be asked", func() bool { return f.deepseek.count() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled translation did not return")
	}
}

// 每个开关都走同一路径写入，存下的就是设置窗口读回的值。
func TestSettingsRoundTrip(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	f.tools.section.Set(Defaults())

	// 服务先存，因为它必须存在才能成为正在使用的那一个：名为用户自有服务的
	// provider 在该服务不存在时会被拉回默认值。
	if err := f.translate.SetCustomTranslator(ctx, "custom", CustomSettings{
		BaseURL:     "api.example.com/v1/",
		DisplayName: "  本地 Ollama  ",
	}); err != nil {
		t.Fatalf("SetCustomTranslator failed: %v", err)
	}
	if err := f.translate.SetProvider(ctx, "custom"); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	if err := f.translate.SetPinned(ctx, true); err != nil {
		t.Fatalf("SetPinned failed: %v", err)
	}
	if err := f.translate.SetCloseDelay(ctx, 1200*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}

	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.ProviderOr() != "custom" {
		t.Errorf("provider = %q", settings.ProviderOr())
	}
	if !settings.Pinned {
		t.Error("pinned was not stored")
	}
	if settings.CloseDelayOr() != 1200*time.Millisecond {
		t.Errorf("close delay = %s", settings.CloseDelayOr())
	}
	service, ok := settings.CustomService("custom")
	if !ok {
		t.Fatalf("the service that was saved is not in the settings: %+v", settings.Custom)
	}
	if service.BaseURL != "https://api.example.com/v1" {
		t.Errorf("custom base URL = %q, want it cleaned", service.BaseURL)
	}
	if service.DisplayName != "本地 Ollama" {
		t.Errorf("custom name = %q, want it trimmed", service.DisplayName)
	}
	// 读取翻译设置不会扰动它们。两个工具共用一个文件，防止一方丢失另一方开关的，
	// 是每个 section 只拥有自己的键——翻译 section 没有可写的剪贴板字段。
	before := f.tools.section.Current()
	if _, err := f.translate.Settings(); err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	after := f.tools.section.Current()
	if after.Provider != before.Provider || !sameServices(after.Custom, before.Custom) {
		t.Errorf("reading the translation settings changed them: %+v then %+v", before, after)
	}
}

// 没有翻译器响应的 provider 被拒绝，而非存储。
func TestSetProviderRefusesAnUnknownID(t *testing.T) {
	f := newTranslate(t)
	err := f.translate.SetProvider(context.Background(), "nobody")
	if !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("error = %v, want ErrProviderNotFound", err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.ProviderOr() != DefaultProvider {
		t.Errorf("provider = %q, want the stored one kept", settings.ProviderOr())
	}
}

// 无法写入的开关会被报告，应用运行时的值是磁盘上的值，而非本打算写入的值。
func TestSettingsReportAWriteFailure(t *testing.T) {
	f := newTranslate(t)
	f.tools.section.Set(Defaults())
	f.tools.err = errors.New("the disk is full")
	err := f.translate.SetPinned(context.Background(), true)
	if err == nil {
		t.Fatal("a failed write was reported as success")
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if settings.Pinned {
		t.Error("the switch is on although nothing was written")
	}
}

// 设置窗口索取翻译器列表、每个的配置情况、以及现在能否使用。
func TestProvidersDescribeWhatIsUsable(t *testing.T) {
	f := newTranslate(t)
	f.tools.section.Set(Defaults())
	views, err := f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 4 {
		t.Fatalf("views = %+v, want every translator", views)
	}
	byID := map[string]TranslatorView{}
	for _, view := range views {
		byID[view.ID] = view
	}
	// 免费引擎无需任何存储即可使用，这正是它的意义所在。
	for _, id := range []string{contractfree.MicrosoftEdge, contractfree.Bilibili} {
		view := byID[id]
		if !view.Configured {
			t.Errorf("%s is not reported as configured: %+v", id, view)
		}
		if view.NeedsCredential {
			t.Errorf("%s is reported as needing a key: %+v", id, view)
		}
	}
	// key 已存的翻译器算已配置，key 未存的会显示但不能选。
	deepseek := byID["deepseek"]
	if !deepseek.Configured || !deepseek.NeedsCredential {
		t.Errorf("deepseek = %+v", deepseek)
	}
	// 用户自有服务在有地址前不算已配置。
	if custom := byID["custom"]; custom.Configured || custom.Endpoint != "" {
		t.Errorf("custom = %+v, want it unconfigured before an address", custom)
	}
	if byID["custom"].NeedsCredential {
		t.Error("a local service is reported as needing a key")
	}

	if _, err := f.translate.AddCustomTranslator(context.Background(), CustomSettings{
		BaseURL: "https://api.example.com/v1",
	}); err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	views, err = f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	var custom TranslatorView
	for _, view := range views {
		if view.ID == "custom" {
			custom = view
		}
	}
	if !custom.Configured {
		t.Error("a service with an address is still reported as unconfigured")
	}
	if custom.Endpoint != "https://api.example.com/v1/chat/completions" {
		t.Errorf("endpoint = %q", custom.Endpoint)
	}
}

// 用户可以保留多个自有服务，每个都是独立的翻译器：添加一个不扰动已有的，
// 每个为各自的地址作答。
func TestSeveralOwnServicesLiveSideBySide(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	first, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	second, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "远端网关",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if first != "custom" || second != "custom-2" {
		t.Fatalf("ids = %q / %q, want custom and custom-2", first, second)
	}

	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if len(settings.Custom) != 2 {
		t.Fatalf("services = %+v, want both", settings.Custom)
	}
	// 各自保留自己的地址和名字：第二个没有覆盖第一个。
	if service, _ := settings.CustomService(first); service.DisplayName != "本地 Ollama" {
		t.Errorf("the first service is %+v", service)
	}
	if service, _ := settings.CustomService(second); service.DisplayName != "远端网关" {
		t.Errorf("the second service is %+v", service)
	}

	// 移除其一，另一个原封不动。
	if err := f.translate.RemoveCustomTranslator(ctx, first); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	settings, err = f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if _, still := settings.CustomService(first); still {
		t.Error("the removed service is still there")
	}
	if service, ok := settings.CustomService(second); !ok || service.DisplayName != "远端网关" {
		t.Errorf("removing one disturbed the other: %+v", settings.Custom)
	}
}

// 用户自有服务会被拒绝非法地址，完全没有地址的也会被拒绝而非存储：
// 没有可问的对象，也没有可显示的内容。
func TestAddingAnOwnServiceChecksTheAddress(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	if _, err := f.translate.AddCustomTranslator(ctx, CustomSettings{DisplayName: "没地址"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
	if _, err := f.translate.AddCustomTranslator(ctx, CustomSettings{BaseURL: "file:///etc/passwd"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("error = %v, want ErrInvalidArgument", err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if len(settings.Custom) != 0 {
		t.Errorf("a refused service was stored anyway: %+v", settings.Custom)
	}
}

// 不属于用户自有服务的 id 会被设置器和移除器同时拒绝：内置翻译器的地址不归用户
// 改写，移除它会拿走应用已知的服务。
func TestOwnServiceActionsRefuseABuiltInID(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	if err := f.translate.SetCustomTranslator(ctx, "deepseek", CustomSettings{BaseURL: "https://x.example.com/v1"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("SetCustomTranslator(deepseek) = %v, want ErrInvalidArgument", err)
	}
	if err := f.translate.RemoveCustomTranslator(ctx, "deepseek"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("RemoveCustomTranslator(deepseek) = %v, want ErrInvalidArgument", err)
	}
}

// 删除正在使用的服务会让工具回到默认，而不是停留在已消失的 id 上。
//
// 这正是用户删掉曾选中的服务后看到的：下一次按键必须能翻译，而不是报告一个不存在的服务。
func TestRemovingTheServiceInUseFallsBackToTheDefault(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	id, err := f.translate.AddCustomTranslator(ctx, CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "要删掉的服务",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if err := f.translate.SetProvider(ctx, id); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	if got := providerOf(t, f); got != id {
		t.Fatalf("provider = %q, want the service that was just added", got)
	}

	if err := f.translate.RemoveCustomTranslator(ctx, id); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	if got := providerOf(t, f); got != DefaultProvider {
		t.Errorf("provider = %q after removing the one in use, want %q", got, DefaultProvider)
	}
	// 工具照常作答，而不是报告缺失的服务。
	useDeepSeek(t, f)
	if _, err := f.translate.Translate(ctx, Request{Text: "hello"}); err != nil {
		t.Errorf("Translate failed after the removal: %v", err)
	}
}

// providerOf 是工具被设起使用的翻译器，取设置中的值。
func providerOf(t *testing.T, f *translateFixture) string {
	t.Helper()
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings.ProviderOr()
}

// 指定不存在服务的 provider 会被拉回默认，无论是手工删除所致还是移除所致。
//
// 手工编辑的文件是另一条入口，规则相同：工具必须继续工作，provider 必须指向能作答的东西。
func TestAProviderWithNoServiceFallsBackToTheDefault(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	// 直接写进设置，就像手工编辑的文件那样。
	if err := f.translate.SaveSettings(ctx, func(s *Settings) { s.Provider = "custom-9" }); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if got := providerOf(t, f); got != DefaultProvider {
		t.Errorf("provider = %q, want the default %q", got, DefaultProvider)
	}

	// 内置 id 不动：本版本是否认识它是注册表的问题，不认识时窗口会说明。
	if err := f.translate.SaveSettings(ctx, func(s *Settings) { s.Provider = "nobody" }); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if got := providerOf(t, f); got != "nobody" {
		t.Errorf("provider = %q, want it left as written", got)
	}
}

// 翻译器列表绝不自行重排：相同的 id 无论哪个在用都按相同顺序返回。
//
// 设置窗口用它绘制行，选中一个就移动的行是用户瞄不准的行——按下的位置落在了顶替它的行上。
func TestProviderOrderDoesNotDependOnWhatIsInUse(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	ids := func() []string {
		t.Helper()
		views, err := f.translate.Providers(ctx)
		if err != nil {
			t.Fatalf("Providers failed: %v", err)
		}
		order := make([]string, 0, len(views))
		for _, view := range views {
			order = append(order, view.ID)
		}
		return order
	}

	before := ids()
	if len(before) != 4 {
		t.Fatalf("the list is %v, want every translator", before)
	}
	// 依次选中每一个，顺序每次都相同。
	for _, pick := range append(append([]string{}, before...), DefaultProvider) {
		if err := f.translate.SetProvider(ctx, pick); err != nil {
			t.Fatalf("SetProvider(%s) failed: %v", pick, err)
		}
		after := ids()
		if !sameOrder(before, after) {
			t.Fatalf("picking %s reordered the list: %v then %v", pick, before, after)
		}
	}
}

// sameOrder 报告两个列表是否在相同位置持有相同 id。
func sameOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// 未存储的凭据使翻译器不可用，正是窗口所显示的。
func TestProvidersFollowTheCredential(t *testing.T) {
	f := newTranslate(t)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	views, err := f.translate.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	for _, view := range views {
		if view.ID == "deepseek" && view.Configured {
			t.Error("a translator with no key is reported as configured")
		}
	}
}

// 测试配置会翻译一段短文本：用户输入的会被使用，且不存储任何东西。
func TestTestTranslator(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()

	// 用户什么都没输入时，测试存储中的配置。
	if err := f.translate.TestTranslator(ctx, "deepseek", nil); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	request := f.deepseek.lastRequest()
	if !strings.Contains(request.Text, "connection test") {
		t.Errorf("the test translated %q, want a test sentence", request.Text)
	}
	if request.Annotation.Wants() {
		t.Errorf("the test asked for an annotation: %+v", request.Annotation)
	}

	// 用户确实输入了内容时，测试其输入。
	before := f.deepseek.count()
	if err := f.translate.TestTranslator(ctx, "deepseek", map[string]string{"apiKey": "sk-typed-0002"}); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	if f.deepseek.count() != before+1 {
		t.Errorf("the translator was asked %d times, want one more", f.deepseek.count())
	}

	// 没有地址的服务无从测试：调用者被告知去配置，而不是等待一个不可能的请求。
	if err := f.translate.TestTranslator(ctx, "custom", nil); err == nil {
		t.Error("testing a service with no address succeeded")
	}

	// 没人注册的翻译器无从测试。
	if err := f.translate.TestTranslator(ctx, "nobody", nil); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("error = %v, want ErrProviderNotFound", err)
	}

	// 拒绝该 key 的服务会说明这一点。
	f.deepseek.fail(fmt.Errorf("%w: deepseek.apiKey was refused", domain.ErrProviderUnauthorized))
	if err := f.translate.TestTranslator(ctx, "deepseek", nil); !errors.Is(err, domain.ErrProviderUnauthorized) {
		t.Errorf("error = %v, want ErrProviderUnauthorized", err)
	}
}

// 需要 key、但既未存储也未输入的翻译器是缺少凭据，且绝不会被问。
func TestTestTranslatorWithoutAKey(t *testing.T) {
	f := newTranslate(t)
	if err := f.secrets.Delete(context.Background(), domain.CredentialKey("deepseek", "apiKey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	err := f.translate.TestTranslator(context.Background(), "deepseek", nil)
	if !errors.Is(err, domain.ErrCredentialMissing) {
		t.Fatalf("error = %v, want ErrCredentialMissing", err)
	}
	if f.deepseek.count() != 0 {
		t.Error("the translator was asked without a credential")
	}
}

// 测试从不存储给它的内容：测试 key 不等于保存它。
func TestTestTranslatorDoesNotStore(t *testing.T) {
	f := newTranslate(t)
	if err := f.translate.TestTranslator(context.Background(), "deepseek", map[string]string{"apiKey": "sk-typed-0002"}); err != nil {
		t.Fatalf("TestTranslator failed: %v", err)
	}
	secret, err := f.secrets.Get(context.Background(), domain.CredentialKey("deepseek", "apiKey"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if secret.Text() != "sk-test-0001" {
		t.Errorf("the stored credential is %q, want the original", secret.Text())
	}
}

// 空注册表是一个说「去配置一个」的工具，正是没有任何翻译器的全新安装所到达的状态。
func TestTranslateWithoutAnyTranslator(t *testing.T) {
	tools := newFakeTools()
	service, err := NewTranslate(TranslateOptions{
		Translators: newFakeTranslators(),
		Secrets:     newMemoryStore(),
		Section:     tools.section,
	})
	if err != nil {
		t.Fatalf("NewTranslate failed: %v", err)
	}
	// 默认 provider 是该注册表没有的，与空注册表是同样的答复：工具说明去哪里配置
	// 服务，而不是去伸手要网络。
	tools.section.Set(Defaults())
	if _, err := service.Translate(context.Background(), Request{Text: "hello"}); !errors.Is(err, domain.ErrTranslateUnavailable) {
		t.Errorf("error = %v, want ErrTranslateUnavailable", err)
	}
	views, err := service.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("views = %+v, want none", views)
	}
}

func TestNewTranslateRequiresItsDependencies(t *testing.T) {
	if _, err := NewTranslate(TranslateOptions{Secrets: newMemoryStore()}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("without a registry = %v", err)
	}
	if _, err := NewTranslate(TranslateOptions{Translators: newFakeTranslators()}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("without a secret store = %v", err)
	}
}

func TestDetectCustomImageCapabilityPersistsOnlyTheVerifiedOutcome(t *testing.T) {
	f := newTranslate(t)
	ctx := context.Background()
	if err := f.translate.SaveSettings(ctx, func(s *Settings) {
		s.Custom["custom"] = CustomSettings{BaseURL: "https://api.example.com/v1"}
	}); err != nil {
		t.Fatal(err)
	}
	supported, err := f.translate.DetectCustomImageCapability(ctx, "custom")
	if err != nil || !supported {
		t.Fatalf("DetectCustomImageCapability = %v, %v", supported, err)
	}
	settings, err := f.translate.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Custom["custom"].SupportsImage {
		t.Error("successful probe was not persisted")
	}

	f.custom.fail(domain.ErrProviderUnavailable)
	supported, err = f.translate.DetectCustomImageCapability(ctx, "custom")
	if err == nil || supported {
		t.Fatalf("failed probe = %v, %v", supported, err)
	}
	settings, _ = f.translate.Settings()
	if settings.Custom["custom"].SupportsImage {
		t.Error("failed probe left a stale image capability")
	}
	if _, err := f.translate.DetectCustomImageCapability(ctx, "deepseek"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("non-custom probe = %v, want invalid argument", err)
	}
}
