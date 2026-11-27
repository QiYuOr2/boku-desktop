package free

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	contract "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/httpc"
)

// Microsoft 是微软 Edge 引擎：Edge 自带翻译功能所用的端点。
//
// 每次只答一句，适合普通翻译而非日语读音。请求读音时返回译文与「没有读音」的
// 提示而非失败。
//
// 其协议有两处不直观：请求体是字符串的裸 JSON 数组而非对象，答复是与请求对齐的
// 数组；语言码也不是工具用的两字母码（中文用 "zh-Hans" 而非 "zh"），故下面要转换。
type Microsoft struct {
	client *httpc.Client
	now    func() time.Time
	// endpoint 是格式字符串，唯一的占位符是目标语言，便于测试指向 httptest 服务。
	endpoint string
}

// microsoftEndpoint 是引擎请求的地址。查询串与 Edge 自身发送的一致；空的 `from`
// 让端点自行检测源语言。
const microsoftEndpoint = "https://edge.microsoft.com/translate/translatetext?from=&to=%s&isEnterpriseClient=false"

func NewMicrosoft(client *httpc.Client) *Microsoft {
	return newMicrosoft(client, microsoftEndpoint)
}

func newMicrosoft(client *httpc.Client, endpoint string) *Microsoft {
	return &Microsoft{
		client:   client,
		now:      func() time.Time { return time.Now().UTC() },
		endpoint: endpoint,
	}
}

func (m *Microsoft) ID() string { return contract.MicrosoftEdge }

// Descriptor 说明引擎名称与无需 key；Endpoint 为文本去向，设置窗口按产品设计
// §12 要求展示给用户。
func (m *Microsoft) Descriptor() domain.TranslatorDescriptor {
	return domain.TranslatorDescriptor{
		ID:                   contract.MicrosoftEdge,
		DisplayName:          "微软 Edge 翻译",
		CredentialKeys:       []string{contract.APIKeyCredential},
		DocsURL:              contract.DocsURL,
		Endpoint:             "edge.microsoft.com/translate/translatetext",
		AllowEmptyCredential: true,
	}
}

// Translate 不读取凭据，也不要求凭据：免费引擎若向用户索要 key，就成了整个免费层
// 旨在消除的「先填 Key」门槛。
func (m *Microsoft) Translate(ctx context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if err := req.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	text := prepare(req)
	if strings.TrimSpace(text.text) == "" {
		return domain.TranslationResult{}, fmt.Errorf("%w: there is no text to translate", domain.ErrInvalidArgument)
	}
	body, err := json.Marshal([]string{text.text})
	if err != nil {
		return domain.TranslationResult{}, fmt.Errorf("free: encode the request: %w", err)
	}
	endpoint := fmt.Sprintf(m.endpoint, microsoftLanguage(req.TargetLang))
	result, err := m.client.PostJSON(ctx, endpoint, map[string]string{
		"Content-Type": "application/json",
	}, body)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	translated, err := readMicrosoftAnswer(result)
	if err != nil {
		return domain.TranslationResult{}, err
	}
	answer := plainResult(translated, req, text.source, text.text, m.now())
	answer.Note = firstNote(truncationNote(text.cut), answer.Note)
	if err := answer.Validate(); err != nil {
		return domain.TranslationResult{}, err
	}
	return answer, nil
}

// readMicrosoftAnswer 从端点答复中取出译文。
//
// 形状是与请求对齐的数组（每个发送的字符串对应一个对象），每个对象带检测语言与
// translations 数组。不符合此形状的答复视为坏响应而非空译文：调用方绝不能把空白
// 当作答案画出。
func readMicrosoftAnswer(result domain.FetchResult) (string, error) {
	snippet := func() string { return kit.RedactText(domain.Snippet(result.Payload)) }
	var parsed []struct {
		DetectedLanguage struct {
			Language string  `json:"language"`
			Score    float64 `json:"score"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
			To   string `json:"to"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(result.Payload, &parsed); err != nil {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer is not the Edge translation shape",
			Snippet:    snippet(),
		}
	}
	if len(parsed) == 0 || len(parsed[0].Translations) == 0 {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries no translation",
			Snippet:    snippet(),
		}
	}
	translated := strings.TrimSpace(parsed[0].Translations[0].Text)
	if translated == "" {
		return "", &domain.BadResponseError{
			Endpoint:   result.Endpoint,
			HTTPStatus: result.HTTPStatus,
			Reason:     "the answer carries an empty translation",
			Snippet:    snippet(),
		}
	}
	return translated, nil
}

// microsoftLanguage 把工具的语言码转成该端点的写法。
//
// 关键的是中文：工具用 "zh"，Edge 用 "zh-Hans"（简体）与 "zh-Hant"（繁体），
// "zh" 不是它的合法码。其余语言原样透传。
func microsoftLanguage(language domain.Language) string {
	switch language {
	case domain.LangChinese:
		return "zh-Hans"
	default:
		return string(language)
	}
}

// firstNote 返回第一条非空提示：结果只携带一条缺失说明，而文本被截断是更应优先
// 告知的。
func firstNote(notes ...string) string {
	for _, note := range notes {
		if strings.TrimSpace(note) != "" {
			return note
		}
	}
	return ""
}
