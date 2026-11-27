package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ClipKind 表示一条剪贴板记录的内容类型。前四种是用户复制的文本；
// 后两种表示复制的是文件列表或图片，Boku 只按名称记录、不记录内容。
type ClipKind string

const (
	ClipKindText  ClipKind = "text"
	ClipKindURL   ClipKind = "url"
	ClipKindJSON  ClipKind = "json"
	ClipKindCode  ClipKind = "code"
	ClipKindFile  ClipKind = "file"
	ClipKindImage ClipKind = "image"
)

// Valid 报告该类型是否为应用已知的类型。
func (k ClipKind) Valid() bool {
	switch k {
	case ClipKindText, ClipKindURL, ClipKindJSON, ClipKindCode, ClipKindFile, ClipKindImage:
		return true
	}
	return false
}

// ClipContent 是对系统剪贴板的一次读取，即平台所见的原始数据，
// 不含任何来自具体工具包的痕迹。
//
// Text、Files、HasImage 与 Image 可能同时被设置（例如从文件管理器复制文件时，
// 路径也会以文本形式放进剪贴板），Kind 指明用户实际想要哪一种。
type ClipContent struct {
	Kind ClipKind
	// Display 是界面对该内容显示的单行文本。
	Display string
	// Text 是剪贴板上的纯文本，截断到 MaxClipTextBytes。
	Text string
	// Files 是剪贴板上的路径，按平台给出的顺序。
	Files []string
	// HasImage 表示剪贴板上有图片。
	HasImage bool
	// ImageBytes 是剪贴板上位图的大小（当初读到图片时）。
	ImageBytes int
	// Image 是位图本身，以 PNG 字节保存，上限 MaxClipImageBytes。保留它而非
	// 仅记录尺寸，是因为图片要能放回原处：点击历史中的图片会把它写回剪贴板，
	// 划词工具也会借用剪贴板后再还回去。
	Image []byte
	// ImageRef 标识该位图：上述字节的哈希，历史存储与比较都靠它。
	// 同尺寸的两张截图是两张图，只有哈希能分辨。
	ImageRef string
	// HTML 是复制的富文本形式（平台提供时）。应用不保留标记，只读取用于
	// 展示的文本。
	HTML string
	// Observation 是本次读取中值得用户知晓的情况：读不到的内容，或不得不
	// 妥协的地方。用应用自己的措辞，读取完整时为空。
	Observation string
	// CapturedAt 是读取平台的时间。
	CapturedAt time.Time
}

// WithObservation 返回带有一条关于本次读取所察觉情况的说明的读取结果。
func (c ClipContent) WithObservation(note string) ClipContent {
	c.Observation = note
	return c
}

// Valid 报告该读取的类型是否为应用已知的类型。
func (c ClipContent) Valid() bool { return c.Kind.Valid() }

// Unchanged 判断两次读取是否为相同内容，用于避免轮询重复记录同一次复制。
//
// 两次读取都带哈希时，位图按字节哈希比较；只比尺寸会把同尺寸的不同截图
// 当成同一张，导致第二张不被记录，点击第一张时也放回错误的图片。
// 没有哈希的读取（测试中的，或字节读不到的）退回按尺寸比较。
func (c ClipContent) Unchanged(other ClipContent) bool {
	if c.Kind != other.Kind || c.HasImage != other.HasImage ||
		c.Display != other.Display || c.Text != other.Text {
		return false
	}
	if c.ImageRef != "" || other.ImageRef != "" {
		if c.ImageRef != other.ImageRef {
			return false
		}
	} else if c.ImageBytes != other.ImageBytes {
		return false
	}
	if len(c.Files) != len(other.Files) {
		return false
	}
	for i := range c.Files {
		if c.Files[i] != other.Files[i] {
			return false
		}
	}
	return true
}

// ClipRecord 是应用保存的一次读取：内容、标识它的哈希，以及界面绘制的单行文本。
//
// 它不同于 ClipContent（平台的原始答复）：记录已分类、截短并收敛为一行，
// 因此绘制一帧时无需再做这些工作。
type ClipRecord struct {
	Kind    ClipKind
	Content string
	Display string
	Hash    string
	// Files 是复制所带的路径（当其为文件列表时）。
	Files []string
	// ImageBytes 是复制所带位图的大小（平台提供图片时）。
	ImageBytes int
	// ImageRef 标识该位图：其字节的哈希。点击该条目时据此从图片存储加载，
	// 把图片放回剪贴板。复制不含图片或字节读不到时为空。
	ImageRef string
	// Note 是本次读取自我察觉的情况，供界面展示：读不到的内容或不得不妥协的
	// 地方。读取完整时为空。
	Note string
	// CapturedAt 是读取平台的时间。
	CapturedAt time.Time
}

// ClipboardEntry 是一条已记录的复制，是剪贴板历史的单位。
type ClipboardEntry struct {
	ID string
	// Content 是该条目要写回剪贴板的内容：文本复制为文本，文件列表为路径，
	// 图片为单行文本。
	Content string
	// Display 是界面为该条目显示的单行文本。它被存储而非每次重算，
	// 以便绘制复制列表是读取而非计算。
	Display string
	// Files 是复制所带的路径（当其为文件列表时）：点击条目时写回的内容，
	// 使粘贴文件列表真的粘贴文件。
	Files []string
	// ImageBytes 是复制所带位图的大小（剪贴板上有图片并读到它时）。
	ImageBytes int
	// ImageRef 是该复制持有的图片，存放在图片存储而非历史文件中：
	// 点击条目时加载它并放回剪贴板。复制不含图片或字节读不到时为空。
	ImageRef string
	// Note 是本次读取自我察觉的情况，供界面展示。读取完整时为空。
	Note string
	// ContentHash 用于去重标识内容：哈希相同的条目即同一条目。
	ContentHash string
	Kind        ClipKind
	// Pinned 条目在非置顶条目被淘汰时保留。
	Pinned bool
	// CopiedAt 是读取平台的时间；CreatedAt 是条目写入的时间。两者分开，
	// 使得把一条复制重新置顶到剪贴板时可以只改 CopiedAt 而不改写历史。
	CopiedAt  time.Time
	CreatedAt time.Time
	// DeletedAt 是文档模型中的软删除。当前应用的存储还不会保留已删除条目；
	// 该字段存在是为了让形状与领域模型 §7 一致。
	DeletedAt *time.Time
}

// Line 是列表为该条目显示的内容：有存储的 Display 就用它；
// 来自旧文件的条目则显示其首行。
func (e ClipboardEntry) Line() string {
	if e.Display != "" {
		return e.Display
	}
	if index := strings.IndexByte(e.Content, '\n'); index >= 0 {
		return e.Content[:index] + " …"
	}
	return e.Content
}

// Validate 报告该条目存在什么问题。
func (e ClipboardEntry) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("%w: clipboard entry has no id", ErrInvalidArgument)
	}
	if strings.TrimSpace(e.ContentHash) == "" {
		return fmt.Errorf("%w: clipboard entry %s has no content hash", ErrInvalidArgument, e.ID)
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: clipboard entry %s has kind %q", ErrInvalidArgument, e.ID, e.Kind)
	}
	if e.Content == "" {
		return fmt.Errorf("%w: clipboard entry %s has no content", ErrInvalidArgument, e.ID)
	}
	if e.CopiedAt.IsZero() {
		return fmt.Errorf("%w: clipboard entry %s has no copiedAt", ErrInvalidArgument, e.ID)
	}
	return nil
}

// MaxClipTextBytes 是单次复制保留的文本量，更长会截断：
// 剪贴板里装着一整个文件时不能整份写进历史。
//
// 它是对实体的约束而非设置，因此放在这里而不在剪贴板功能的配置里：
// 一条记录能装什么取决于 ClipboardEntry，而负责截断的 internal/kit
// 只允许依赖本包。
const MaxClipTextBytes = 64 << 10

// MaxClipImageBytes 是读取剪贴板上位图的上限。更大的位图完全不读：
// 条目仍会表示复制了图片，但应用不持有它。
const MaxClipImageBytes = 16 << 20

// ClipboardStore 读写剪贴板历史。条目整体存储，新的在前：
// 列表足够短，读取几乎无成本，替换即整个写入。
type ClipboardStore interface {
	LoadEntries(ctx context.Context) ([]ClipboardEntry, error)
	SaveEntries(ctx context.Context, entries []ClipboardEntry) error
	// SaveImage 把复制的图片字节按 ref 保存，以便点击其条目时能放回图片。
	// 重复保存同一 ref 不算错误，也不是第二份拷贝。
	SaveImage(ctx context.Context, ref string, png []byte) error
	// LoadImage 返回 SaveImage 保存的内容。
	LoadImage(ctx context.Context, ref string) ([]byte, error)
	// DeleteImage 遗忘它，删除其条目时即调用。
	DeleteImage(ctx context.Context, ref string) error
}

// ClipboardDevice 是平台剪贴板。Read 须在平台要求的线程上运行，
// 由实现自行负责，因此调用方可以从 goroutine 中读取。
type ClipboardDevice interface {
	// Read 返回当前剪贴板上的内容。ErrUnsupportedClipboardFormat
	// 表示其中的内容本应用无法展示。
	Read(ctx context.Context) (ClipContent, error)
	// WriteText 把文本写回剪贴板。
	WriteText(ctx context.Context, text string) error
	// WriteFiles 把文件列表写回剪贴板：点击一条已记录的文件复制，
	// 应再次复制文件本身，而非其路径的文本。
	WriteFiles(ctx context.Context, paths []string) error
	// WriteImage 把位图以其读入时的 PNG 字节写回剪贴板。
	// 点击历史中的图片与划词工具还回借用的内容都会用到它。
	WriteImage(ctx context.Context, png []byte) error
}

// ClipboardChangeCounter 是平台剪贴板的可选能力。只要其它应用写入剪贴板，
// 它的值就会改变。当「发生了一次新复制」这一事实重要、即使字节恰好等于上一次
// 复制时，调用方会用到它。
//
// 零值表示平台无法提供计数器。
type ClipboardChangeCounter interface {
	ChangeCount() uint32
}

// ScreenshotDevice 打开平台区域选择器并返回用户选中的 PNG。
// 它属于平台边界；翻译功能只接收临时字节。
type ScreenshotDevice interface {
	Capture(ctx context.Context) ([]byte, error)
}

// ErrClipboardChanged 表示内容在读取过程中发生了变化，
// 因此该次读取被丢弃，而不是记录一半。
var ErrClipboardChanged = errors.New("clipboard changed while reading")
