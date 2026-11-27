package translate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// Translate 是划词翻译工具：读取用户选中的内容，向配置的翻译器索取答案，并保存
// 决定这一切的开关。
//
// 工具规则的唯一所在：用哪个翻译器、什么都没配时如何、文本翻成哪种语言、以及用户
// 自有服务配置了什么。每个方法都可在任意 goroutine 安全调用。
type Translate struct {
	translators domain.TranslatorRegistry
	secrets     domain.SecretStore
	section     *Section
	// saver 是保存 section 所用的存储，设置只留在内存时则为 nil。
	saver     StoreSettings
	clipboard domain.ClipboardDevice
	selection domain.SelectionDevice
	now       func() time.Time
}

// TranslateOptions 是组装服务所需的依赖。除两个读取用户所选内容的设备外都必需：
// 自带文本的翻译不需要它们，二者皆无的构建则回答没有可翻译的内容。
type TranslateOptions struct {
	Translators domain.TranslatorRegistry
	Secrets     domain.SecretStore
	// Section 是本功能的设置，即存储持有的那份：装配时读取，开关变化时经它写入。
	// nil section 让设置只留在内存，这是测试想要的，也是没有设置文件的装配必须能存活的。
	Section *Section
	// Settings 是保存 section 所用的存储。nil 存储让设置只留在内存。
	Settings StoreSettings
	// Selection 读取用户在其他应用里选中的内容，当平台能作答时。它被优先询问：
	// 它关乎用户正看着的东西，而剪贴板关乎他们上次复制的内容。
	Selection domain.SelectionDevice
	// Clipboard 在调用者未自带文本、且平台无法说明选中内容时，读取剪贴板上的内容。
	Clipboard domain.ClipboardDevice
	Clock     domain.Clock
}

// NewTranslate 返回该服务。
func NewTranslate(opts TranslateOptions) (*Translate, error) {
	if opts.Translators == nil {
		return nil, fmt.Errorf("%w: no translator registry", domain.ErrInvalidArgument)
	}
	if opts.Secrets == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	clock := opts.Clock
	if clock == nil {
		clock = domain.SystemClock{}
	}
	section := opts.Section
	if section == nil {
		section = NewSection()
	}
	service := &Translate{
		translators: opts.Translators,
		secrets:     opts.Secrets,
		section:     section,
		saver:       opts.Settings,
		clipboard:   opts.Clipboard,
		selection:   opts.Selection,
		now:         clock.Now,
	}
	return service, nil
}

// Request 是界面索取的一次翻译。
type Request struct {
	// Text 是要翻译的文本，就是文本本身：请求不带「去把它找出来」的暗示。用户面前的
	// 内容由 Source 在发起请求前读取——读取意味着询问用户正在使用的应用，而显示答案
	// 的卡片会夺走该应用的键盘。
	Text string
	// Target 覆盖文本会被翻译成的语言。实际上为空；工具从源文本决定。
	Target domain.Language
	// Annotate 索取文本的词及其读音，完全不要翻译。
	//
	// 它是卡片的第二个请求：翻译发出时不等注释，词在用户展开时才索取。两个请求都是
	// 对翻译器的同一次调用——注释是提示词要求的内容而非段落——区分它们的正是这个字段。
	Annotate bool
}

// Result 是一个答案，按窗口绘制的形状。
type Result struct {
	SourceText     string
	TranslatedText string
	// Segments 是逐段注释，当翻译器给出时。为空表示源文本画作一行。
	Segments   []domain.Segment
	SourceLang domain.Language
	TargetLang domain.Language
	// ProviderID 与 ProviderName 是窗口页脚所说的翻译来源。
	ProviderID   string
	ProviderName string
	// Note 是翻译器注意到的问题：不得不缩短的文本、无法注释的片段。
	Note string
	At   time.Time
}

// TranslatorView 是设置窗口展示的一个翻译器。
type TranslatorView struct {
	ID          string
	DisplayName string
	// DocsURL 是用户获取凭据的地方。
	DocsURL string
	// Endpoint 是设置当前状态下的地址，用户尚未指定时为空。
	Endpoint string
	// CredentialKey 是翻译器索要的凭据，不含 provider 前缀。
	CredentialKey string
	// NeedsCredential 对无需 key 即可工作的翻译器为 false。
	NeedsCredential bool
	// Configured 是翻译器所需的东西此刻是否齐备。
	Configured    bool
	SupportsImage bool
}

// Settings 返回当前状态的翻译配置。
func (t *Translate) Settings() (Settings, error) {
	return t.section.Current(), nil
}

// SaveSettings 通过函数修改配置，使每个开关都以同一方式写入：改动本功能设置中的
// 一项，让存储连同其它功能的一起写回。
//
// 它不写整份设置值。文件含所有功能的设置，在此读出的值再写回会带回其它功能期间的
// 改动：翻一个翻译开关会撤销期间翻动的剪贴板开关。存储的 Update 重新读取文件、
// 应用本功能的改动再写入，作为一个步骤，所以落地的只有翻译设置，别无它物。
//
// 无法保存的开关会被报告，应用运行时的设置是磁盘上的，而非本打算写入的。
func (t *Translate) SaveSettings(ctx context.Context, change func(*Settings)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.saver == nil {
		return nil // kept in memory only, which is what a test wants
	}
	return t.saver.Update(ctx, sectionName, func() error {
		settings := t.section.Current()
		change(&settings)
		t.section.Set(settings)
		return nil
	})
}

// SetEnabled 开启或关闭工具。
func (t *Translate) SetEnabled(ctx context.Context, enabled bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Enabled = enabled })
}

// SetAccelerator 记住启动翻译的快捷键，让下次启动注册同一个。
//
// 它不评判组合：平台是否接受是平台的答复，记录它的窗口已经拿到该答复。存下的就是
// 实际绑定的。
func (t *Translate) SetAccelerator(ctx context.Context, accelerator string) error {
	trimmed := strings.TrimSpace(accelerator)
	if trimmed == "" {
		return fmt.Errorf("%w: the shortcut is empty", domain.ErrInvalidArgument)
	}
	return t.SaveSettings(ctx, func(s *Settings) { s.Accelerator = trimmed })
}

// SetScreenshotAccelerator 允许空快捷键，表示禁用。
func (t *Translate) SetScreenshotAccelerator(ctx context.Context, accelerator string) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.ScreenshotAccelerator = strings.TrimSpace(accelerator) })
}

// SetProvider 选择要用的翻译器。没有翻译器响应的 id 在此被拒绝而非存储：
// 设置窗口只提供存在的，指向其它的文件是手工编辑的。
func (t *Translate) SetProvider(ctx context.Context, id string) error {
	if _, err := t.translators.Translator(id); err != nil {
		return err
	}
	return t.SaveSettings(ctx, func(s *Settings) { s.Provider = id })
}

// SetFurigana 开启或关闭日语汉字的读音。
func (t *Translate) SetFurigana(ctx context.Context, on bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Furigana = on })
}

// SetLoanwordGloss 开启或关闭片假名外来词的英语原词注释。
func (t *Translate) SetLoanwordGloss(ctx context.Context, on bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.LoanwordGloss = on })
}

// SetPinned 决定窗口是否以固定状态打开。
func (t *Translate) SetPinned(ctx context.Context, pinned bool) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.Pinned = pinned })
}

// SetCloseDelay 决定窗口失去键盘焦点后等待多久自行关闭。
func (t *Translate) SetCloseDelay(ctx context.Context, delay time.Duration) error {
	return t.SaveSettings(ctx, func(s *Settings) { s.CloseDelay = delay })
}

// AddCustomTranslator 记录一个用户自有服务，返回它存储时用的 id，即现在去问它的
// 翻译器 id。
//
// id 在此处选择而非由调用者给出，因为它是本功能的存储决定：第一个服务取
// "custom"，之后编号，而可以有多于一个之前写下的文件仍读作其中第一个。
//
// 地址按本功能的规则清理，使存下的就是会被问的。没有地址的服务被拒绝而非存储：
// 没有可问的名字，也没有可显示的内容。
func (t *Translate) AddCustomTranslator(ctx context.Context, settings CustomSettings) (string, error) {
	settings = settings.Normalize()
	if settings.BaseURL == "" {
		return "", fmt.Errorf("%w: 自定义服务至少需要一个 Base URL", domain.ErrInvalidArgument)
	}
	id := ""
	err := t.SaveSettings(ctx, func(s *Settings) {
		if s.Custom == nil {
			s.Custom = map[string]CustomSettings{}
		}
		id = s.NextCustomServiceID()
		s.Custom[id] = settings
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// SetCustomTranslator 记录用户自有服务之一的地址和称呼，其余的保持不变。
func (t *Translate) SetCustomTranslator(ctx context.Context, id string, settings CustomSettings) error {
	settings = settings.Normalize()
	if !IsCustomService(id) {
		return fmt.Errorf("%w: %s is not one of the user's own services", domain.ErrInvalidArgument, id)
	}
	return t.SaveSettings(ctx, func(s *Settings) {
		if s.Custom == nil {
			s.Custom = map[string]CustomSettings{}
		}
		s.Custom[id] = settings
	})
}

// DetectCustomImageCapability 对新配置的自定义服务发起一次小型真实多模态请求，
// 只记住其布尔结果。API 模型目录对视觉能力的描述并不一致，所以成功的请求是产品
// 唯一可信的能力信号。
func (t *Translate) DetectCustomImageCapability(ctx context.Context, id string) (bool, error) {
	if !IsCustomService(id) {
		return false, fmt.Errorf("%w: %s is not one of the user's own services", domain.ErrInvalidArgument, id)
	}
	reply, err := t.translators.Translator(id)
	if err != nil {
		return false, err
	}
	descriptor := reply.Descriptor()
	imageReply, ok := reply.(domain.ImageTranslator)
	if !ok {
		return false, nil
	}
	creds, err := t.credentials(ctx, descriptor)
	if err != nil {
		return false, err
	}
	_, err = imageReply.TranslateImage(ctx, creds, imageCapabilityProbePNG(), domain.LangChinese)
	supported := err == nil
	if saveErr := t.SaveSettings(ctx, func(s *Settings) {
		service, exists := s.Custom[id]
		if !exists {
			return
		}
		service.SupportsImage = supported
		s.Custom[id] = service
	}); saveErr != nil {
		return false, saveErr
	}
	return supported, err
}

// imageCapabilityProbePNG 绘制一个小而高对比的 A，让真正收到图像的模型有可读的
// 源文本可返回。每次探测在内存中生成，从不记日志也不保存。
func imageCapabilityProbePNG() []byte {
	const scale = 8
	pattern := []string{
		"01110",
		"10001",
		"10001",
		"11111",
		"10001",
		"10001",
		"10001",
	}
	canvas := image.NewRGBA(image.Rect(0, 0, len(pattern[0])*scale, len(pattern)*scale))
	for y := 0; y < canvas.Bounds().Dy(); y++ {
		for x := 0; x < canvas.Bounds().Dx(); x++ {
			canvas.Set(x, y, color.White)
		}
	}
	for y, row := range pattern {
		for x, bit := range row {
			if bit != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					canvas.Set(x*scale+dx, y*scale+dy, color.Black)
				}
			}
		}
	}
	var encoded bytes.Buffer
	// 编码到内存通常不会失败，也无处返回错误：这是一个探测生成器。失败仍值得
	// 记一行，因为空结果会被当作图像发给模型。
	if err := png.Encode(&encoded, canvas); err != nil {
		slog.Warn("the image capability probe could not be encoded", "event", "image_probe_encode_failed", "error", err)
	}
	return encoded.Bytes()
}

// RemoveCustomTranslator 忘记用户自有服务之一。
//
// 翻译器设置保持不动：若被删的服务正在使用，它所指的 id 不再作答，Translate 会说明。
// 擅自把用户切到另一个服务是在改变用户做出的选择——窗口会问他们改选什么。
func (t *Translate) RemoveCustomTranslator(ctx context.Context, id string) error {
	if !IsCustomService(id) {
		return fmt.Errorf("%w: %s is not one of the user's own services", domain.ErrInvalidArgument, id)
	}
	return t.SaveSettings(ctx, func(s *Settings) { delete(s.Custom, id) })
}

// Providers 列出设置窗口提供的翻译器，按它展示的顺序，附带每个的配置情况以及
// 此刻能否使用。
func (t *Translate) Providers(ctx context.Context) ([]TranslatorView, error) {
	settings, err := t.Settings()
	if err != nil {
		return nil, err
	}
	ids := t.translators.TranslatorIDs()
	views := make([]TranslatorView, 0, len(ids))
	for _, id := range ids {
		reply, err := t.translators.Translator(id)
		if err != nil {
			continue
		}
		descriptor := reply.Descriptor()
		view := TranslatorView{
			ID:              descriptor.ID,
			DisplayName:     descriptor.DisplayName,
			DocsURL:         descriptor.DocsURL,
			Endpoint:        descriptor.Endpoint,
			NeedsCredential: !descriptor.AllowEmptyCredential && len(descriptor.CredentialKeys) > 0,
			SupportsImage:   descriptor.SupportsImage,
		}
		if len(descriptor.CredentialKeys) > 0 {
			view.CredentialKey = descriptor.CredentialKeys[0]
		}
		// 指向用户自有服务的翻译器在该服务的地址存在时才算已配置；地址存在设置中
		// 而非凭据里，按翻译器自己的 id 查找，让用户的每个服务各自作答。
		hasAddress := true
		if descriptor.BaseURLSetting != "" {
			service, _ := settings.CustomService(descriptor.ID)
			hasAddress = service.BaseURL != ""
		}
		view.Configured = hasAddress && (!view.NeedsCredential || t.credentialStored(ctx, descriptor))
		views = append(views, view)
	}
	// 顺序即注册表自身的——排好序的 id——每次相同，无论用户选了什么。
	//
	// 它曾把正在使用的翻译器放在最前，使列表在指针下自行重排：点击一行它便移动，
	// 顶替它的行成了光标下的东西。选择列表必须在用户选择时保持原位。
	return views, nil
}

// TranslateImage 直接询问选中的支持图像的翻译器。provider 接收图片并在一个响应中
// 返回源文本与译文；Boku 从不在本地做 OCR 也不存储它。
func (t *Translate) TranslateImage(ctx context.Context, image []byte) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(image) == 0 {
		return Result{}, fmt.Errorf("%w: no image to translate", domain.ErrInvalidArgument)
	}
	settings, err := t.Settings()
	if err != nil {
		return Result{}, err
	}
	if !settings.Enabled {
		return Result{}, fmt.Errorf("%w: 图片翻译已关闭", domain.ErrTranslateDisabled)
	}
	reply, err := t.translators.Translator(settings.ProviderOr())
	if err != nil {
		return Result{}, fmt.Errorf("%w: 找不到当前翻译服务", domain.ErrTranslateUnavailable)
	}
	descriptor := reply.Descriptor()
	imageReply, ok := reply.(domain.ImageTranslator)
	if !ok || !descriptor.SupportsImage {
		return Result{}, fmt.Errorf("%w: %s 不支持图片翻译", domain.ErrImageTranslateUnavailable, descriptor.DisplayName)
	}
	creds, err := t.credentials(ctx, descriptor)
	if err != nil {
		return Result{}, err
	}
	translation, err := imageReply.TranslateImage(ctx, creds, image, domain.LangChinese)
	if err != nil {
		return Result{}, err
	}
	if err := translation.Validate(); err != nil {
		return Result{}, err
	}
	return Result{SourceText: translation.SourceText, TranslatedText: translation.TranslatedText, SourceLang: translation.SourceLang, TargetLang: translation.TargetLang, ProviderID: descriptor.ID, ProviderName: descriptor.DisplayName, Note: translation.Note, At: translation.CreatedAt}, nil
}

// credentialStored 报告翻译器索要的东西是否在凭据存储中。它绝不返回值：
// 视图只需知道有一个在那里。
func (t *Translate) credentialStored(ctx context.Context, descriptor domain.TranslatorDescriptor) bool {
	for _, key := range descriptor.CredentialKeys {
		secret, err := t.secrets.Get(ctx, domain.CredentialKey(descriptor.ID, key))
		if err != nil || strings.TrimSpace(secret.Text()) == "" {
			return false
		}
	}
	return true
}

// Descriptor 返回某个翻译器的称呼和询问地址，让设置窗口无需知道它是哪个翻译器
// 也能展示。
func (t *Translate) Descriptor(id string) (domain.TranslatorDescriptor, error) {
	reply, err := t.translators.Translator(id)
	if err != nil {
		return domain.TranslatorDescriptor{}, err
	}
	return reply.Descriptor(), nil
}

// TestTranslator 检查配置而不存储它：翻译一段短文本并报告结果。设置窗口用它
// 在保存前告诉用户某个 key 是否可用。
//
// key 在用户输入时取自 override，否则取自凭据存储，让「测试存储的」与「测试刚输入
// 的」是同一个调用。
func (t *Translate) TestTranslator(ctx context.Context, providerID string, override map[string]string) error {
	reply, err := t.translators.Translator(providerID)
	if err != nil {
		return err
	}
	descriptor := reply.Descriptor()
	// 指向用户自有服务的翻译器在用户指定前无从测试。在此说明，而非让翻译器作答，
	// 使用户读到的信息对每种实现都一致。
	if descriptor.BaseURLSetting != "" {
		settings, err := t.Settings()
		if err != nil {
			return err
		}
		service, _ := settings.CustomService(descriptor.ID)
		if strings.TrimSpace(service.BaseURL) == "" {
			return fmt.Errorf("%w: 还没有填写自定义服务的地址", domain.ErrTranslateUnavailable)
		}
	}
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    map[string]string{},
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		if value := strings.TrimSpace(override[key]); value != "" {
			creds.Secrets[key] = value
			continue
		}
		if descriptor.AllowEmptyCredential {
			continue
		}
		secret, err := t.secrets.Get(ctx, domain.CredentialKey(descriptor.ID, key))
		if errors.Is(err, domain.ErrSecretNotFound) {
			return fmt.Errorf("%w: %s", domain.ErrCredentialMissing, domain.CredentialKey(descriptor.ID, key))
		}
		if err != nil {
			return err
		}
		creds.Secrets[key] = secret.Text()
	}
	// 测试就是翻译一个短句：能作答这句的服务就能作答真正的请求。它不是用户的文本，
	// 也不索取注释。
	request := domain.TranslationRequest{
		Text:       testText,
		SourceLang: domain.LangEnglish,
		TargetLang: domain.LangChinese,
	}
	if _, err := reply.Translate(ctx, creds, request); err != nil {
		return err
	}
	return nil
}

// testText 是连接测试翻译的内容。它短，明显是测试而非用户自己的文本，
// 且对它的回答几乎不花钱。
const testText = "Hello, this is a connection test."

// Source 是一次翻译所针对的文本，按工具读取它的方式：平台能说明时用用户选中的内容，
// 不能时用剪贴板上的内容。
//
// 它是独立的一步而非 Translate 做的第一件事，因为顺序对索取它的窗口很重要：读取
// 选中内容意味着询问用户正在使用的应用，而显示翻译的卡片会夺走该应用的键盘。先读、
// 再显示卡片、后翻译——快捷键索取的翻译针对的正是这里回答的文本（工具设计 §2.2）。
//
// 两处都无可读内容时是空答案而非错误：平台说「没有选中」与剪贴板是文件列表，都是
// 「用户得先选中或复制文本」。唯一的错误是完全无法读取的剪贴板，那是值得显示的故障。
func (t *Translate) Source(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 先问选中内容：它关乎用户正看着的东西，而剪贴板关乎他们上次复制的内容。不发布
	// 文本的控件、没有该接口的平台、超时的调用，在这里都是同一答案——没有——于是
	// 改问剪贴板。
	if text := t.selectionText(ctx); strings.TrimSpace(text) != "" {
		return text, nil
	}
	return t.clipboardText(ctx)
}

// Translate 回答一次请求。
//
// 下面的顺序就是工具的规则，也是窗口期望的顺序：关闭的开关、未配置的翻译器和
// 不存在的文本是三种不同答案，没有一种会向服务发请求。
func (t *Translate) Translate(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	settings, err := t.Settings()
	if err != nil {
		return Result{}, err
	}
	if !settings.Enabled {
		return Result{}, fmt.Errorf("%w: 划词翻译已关闭", domain.ErrTranslateDisabled)
	}
	providerID := settings.ProviderOr()
	reply, err := t.translators.Translator(providerID)
	if err != nil {
		// 设置所指的服务未注册。那是设置文件命名了本版本没有的翻译器——手工编辑的
		// 文件，或由发布过某个服务的另一版本写下的文件。
		return Result{}, fmt.Errorf("%w: 找不到 %s，请在设置里重新选择一个翻译服务",
			domain.ErrTranslateUnavailable, providerID)
	}
	descriptor := reply.Descriptor()

	text := normalizeText(req.Text)
	if text == "" {
		// 没有可翻译的内容：Source 既没从选中内容也没从剪贴板读出可用内容——空的、
		// 文件列表、图片——所以用户得先选中或复制文本。
		return Result{}, fmt.Errorf("%w: 先选中文本并复制，再按快捷键", domain.ErrInvalidArgument)
	}

	creds, err := t.credentials(ctx, descriptor)
	if err != nil {
		return Result{}, err
	}
	source := domain.Language(kit.DetectLanguage(text))
	target := req.Target
	if target == "" || !target.Valid() {
		target = domain.Language(kit.TargetLanguage(string(source)))
	}
	// 注释只由需要它的请求索取并付费：无注释的翻译提示更短、答案也更短，
	// 这正是卡片发两个请求的全部原因。
	annotation := domain.AnnotationOptions{}
	if req.Annotate {
		annotation = domain.AnnotationOptions{
			Furigana:      settings.Furigana,
			LoanwordGloss: settings.LoanwordGloss,
		}
	}
	translation, err := reply.Translate(ctx, creds, domain.TranslationRequest{
		Text:       text,
		SourceLang: source,
		TargetLang: target,
		Annotation: annotation,
	})
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SourceText:     translation.SourceText,
		TranslatedText: translation.TranslatedText,
		Segments:       translation.Segments,
		SourceLang:     translation.SourceLang,
		TargetLang:     translation.TargetLang,
		ProviderID:     descriptor.ID,
		ProviderName:   descriptor.DisplayName,
		Note:           translation.Note,
		At:             translation.CreatedAt,
	}
	if result.At.IsZero() {
		result.At = t.now()
	}
	return result, nil
}

// normalizeText 是发送和绘制时所用的文本：用户复制的换行保留，并被统一成一种换行。
//
// 选中内容或剪贴板在 Windows 上携带 \r\n，而窗口绘制的段落按 \n 切分：留在其中的
// 回车符是没人要的字符，而用户复制的格式——诗的行、列表的行——正是他们期望再见到的。
func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

// clipboardText 读取剪贴板上的文本。剪贴板是文件列表或图片时不是可翻译的文本：
// 它与空剪贴板报同一错误，因为那都是用户要处理的事。
func (t *Translate) clipboardText(ctx context.Context) (string, error) {
	if t.clipboard == nil {
		return "", fmt.Errorf("%w: 剪贴板不可用", domain.ErrUnsupportedClipboardFormat)
	}
	content, err := t.clipboard.Read(ctx)
	if err != nil {
		return "", err
	}
	switch content.Kind {
	case domain.ClipKindFile, domain.ClipKindImage:
		return "", nil
	}
	return content.Text, nil
}

// RequestSelectionAccess 请平台显示其权限提示，在有该提示的平台上。应用从设置页
// 和第一次读取前调用它：macOS 在应用主动索取前不会把应用列进辅助功能权限。
func (t *Translate) RequestSelectionAccess() bool {
	if t.selection == nil {
		return false
	}
	authorizer, ok := t.selection.(domain.SelectionAuthorizer)
	if !ok {
		return false
	}
	return authorizer.RequestSelectionAccess()
}

// SelectionAccessGranted 报告平台是否已允许读取选中内容。没有权限模型的平台返回
// true，让设置页只在有事可索取的地方显示请求。
func (t *Translate) SelectionAccessGranted() bool {
	if t.selection == nil {
		return true
	}
	state, ok := t.selection.(domain.SelectionAccessState)
	if !ok {
		return true
	}
	return state.SelectionAccessGranted()
}

// selectionText 询问平台选中了什么，无法作答时返回空。
//
// 这里的任何情况都不是错误。没有该 API 的平台、不发布文本的控件、空选中、超时的
// 调用，对用户都是同一件事：改问那个更早的问题——剪贴板上是什么——它总有答案。
func (t *Translate) selectionText(ctx context.Context) string {
	if t.selection == nil {
		return ""
	}
	text, err := t.selection.SelectedText(ctx)
	if err != nil {
		return ""
	}
	return text
}

// credentials 收集翻译器索要的凭据。缺一个即为 ErrCredentialMissing，在调用翻译器之前：
// 没有 key 的请求只会告诉服务这个 key 是错的。
func (t *Translate) credentials(ctx context.Context, descriptor domain.TranslatorDescriptor) (domain.Credentials, error) {
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    make(map[string]string, len(descriptor.CredentialKeys)),
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		ref := domain.CredentialKey(descriptor.ID, key)
		secret, err := t.secrets.Get(ctx, ref)
		if errors.Is(err, domain.ErrSecretNotFound) {
			if descriptor.AllowEmptyCredential {
				continue
			}
			return creds, fmt.Errorf("%w: %s is not configured", domain.ErrCredentialMissing, ref)
		}
		if err != nil {
			return creds, err
		}
		value := strings.TrimSpace(secret.Text())
		if value == "" {
			if descriptor.AllowEmptyCredential {
				continue
			}
			return creds, fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, ref)
		}
		creds.Secrets[key] = value
	}
	return creds, nil
}
