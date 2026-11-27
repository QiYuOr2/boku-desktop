package diary

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	MaxTags              = 20
	MaxURLLength         = 8192
	MaxAttachmentBytes   = 24 * 1024 * 1024
	MaxMetadataHTMLBytes = 512 * 1024
	MaxRemoteImageBytes  = 4 * 1024 * 1024
)

var (
	ErrFutureDate         = errors.New("diary: cannot write an entry for a future date")
	ErrInvalidAttachment  = errors.New("diary: invalid attachment")
	ErrAttachmentTooLarge = errors.New("diary: attachment exceeds 24 MiB")
	ErrMissingEntry       = errors.New("diary: entry does not exist or is deleted")
	ErrUnreadableDB       = errors.New("diary: database is unreadable or has an unsupported schema version")
	ErrSyncNotConfigured  = errors.New("diary: cloud sync is not configured")
	ErrNotLoggedIn        = errors.New("diary: not logged in to cloud sync")
)

var anyCaseUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Attachment 是日记本地图片附件元数据，与 Apple 端字段保持一致。
type Attachment struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	OriginalName *string   `json:"originalName,omitempty"`
	Width        *int      `json:"width,omitempty"`
	Height       *int      `json:"height,omitempty"`
	AddedAt      time.Time `json:"addedAt"`
	SHA256       *string   `json:"sha256,omitempty"`
	Size         *int64    `json:"size,omitempty"`
	MimeType     *string   `json:"mimeType,omitempty"`
}

// AttachmentUpload 携带尚未落盘的附件字节。
type AttachmentUpload struct {
	ID           string
	Filename     string
	Data         []byte
	OriginalName *string
	Width        *int
	Height       *int
	AddedAt      time.Time
}

func (u AttachmentUpload) Attachment() Attachment {
	return Attachment{
		ID:           u.ID,
		Filename:     u.Filename,
		OriginalName: u.OriginalName,
		Width:        u.Width,
		Height:       u.Height,
		AddedAt:      u.AddedAt.UTC(),
	}
}

// LocationSnapshot 只保存地名与采集时间，不保存经纬度坐标。
type LocationSnapshot struct {
	Place      string    `json:"place"`
	CapturedAt time.Time `json:"capturedAt"`
}

// WeatherSnapshot 是日记创建时采集的天气快照，不随之后天气变化回写。
type WeatherSnapshot struct {
	Place              string    `json:"place"`
	Description        string    `json:"description"`
	TemperatureCelsius float64   `json:"temperatureCelsius"`
	Unit               string    `json:"unit"`
	SymbolName         string    `json:"symbolName"`
	Kind               string    `json:"kind"`
	ObservedAt         time.Time `json:"observedAt"`
	FetchedAt          time.Time `json:"fetchedAt"`
}

// LinkCard 是日记附带的外链元数据卡片。
type LinkCard struct {
	ID           string     `json:"id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	Domain       string     `json:"domain"`
	ImageURL     *string    `json:"imageURL,omitempty"`
	FetchedAt    *time.Time `json:"fetchedAt,omitempty"`
	ShowsPreview bool       `json:"showsPreview"`
	FetchFailed  bool       `json:"fetchFailed"`
}

// Entry 是一篇个人日记条目。
type Entry struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Markdown     string            `json:"markdown"`
	DiaryDateKey DateKey           `json:"diaryDateKey"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	DeletedAt    *time.Time        `json:"deletedAt,omitempty"`
	Tags         []string          `json:"tags"`
	Attachments  []Attachment      `json:"attachments"`
	LinkCards    []LinkCard        `json:"linkCards"`
	Location     *LocationSnapshot `json:"location,omitempty"`
	Weather      *WeatherSnapshot  `json:"weather,omitempty"`
	SyncVersion  int64             `json:"-"`
}

func (e Entry) IsBlank() bool {
	return strings.TrimSpace(e.Title) == "" &&
		strings.TrimSpace(e.Markdown) == "" &&
		len(e.Tags) == 0 &&
		len(e.Attachments) == 0 &&
		len(e.LinkCards) == 0
}

func (e Entry) Normalize(now time.Time) (Entry, error) {
	out := e
	out.ID = strings.ToLower(strings.TrimSpace(out.ID))
	if !IsID(out.ID) {
		return Entry{}, fmt.Errorf("diary: invalid entry id %q", e.ID)
	}
	if out.DiaryDateKey.IsZero() {
		return Entry{}, fmt.Errorf("diary: missing diary date key")
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Tags = UniqueTags(out.Tags)
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now.UTC()
	} else {
		out.CreatedAt = out.CreatedAt.UTC()
	}
	out.UpdatedAt = now.UTC()

	attachments := make([]Attachment, 0, len(out.Attachments))
	for _, att := range out.Attachments {
		if !IsSafeAttachmentFilename(att.Filename) {
			return Entry{}, ErrInvalidAttachment
		}
		att.ID = strings.ToLower(strings.TrimSpace(att.ID))
		if !IsID(att.ID) {
			return Entry{}, ErrInvalidAttachment
		}
		if att.AddedAt.IsZero() {
			att.AddedAt = out.UpdatedAt
		} else {
			att.AddedAt = att.AddedAt.UTC()
		}
		attachments = append(attachments, att)
	}
	out.Attachments = attachments

	cards := make([]LinkCard, 0, len(out.LinkCards))
	for _, card := range out.LinkCards {
		cleanURL, ok := ValidateWebURL(card.URL)
		if !ok {
			continue
		}
		card.URL = cleanURL
		if card.ImageURL != nil {
			if cleanImg, ok := ValidateImageURL(*card.ImageURL); ok {
				card.ImageURL = &cleanImg
			} else {
				card.ImageURL = nil
			}
		}
		cards = append(cards, card)
	}
	out.LinkCards = cards

	if out.Location != nil {
		place := strings.TrimSpace(out.Location.Place)
		if place == "" {
			out.Location = nil
		} else {
			loc := *out.Location
			loc.Place = place
			if loc.CapturedAt.IsZero() {
				loc.CapturedAt = out.UpdatedAt
			} else {
				loc.CapturedAt = loc.CapturedAt.UTC()
			}
			out.Location = &loc
		}
	}
	if out.Weather != nil {
		w := *out.Weather
		w.Place = strings.TrimSpace(w.Place)
		w.ObservedAt = w.ObservedAt.UTC()
		w.FetchedAt = w.FetchedAt.UTC()
		out.Weather = &w
	}
	return out, nil
}

func UniqueTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	result := make([]string, 0, len(tags))
	for _, raw := range tags {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == MaxTags {
			break
		}
	}
	return result
}

// IsSafeAttachmentFilename 校验本地附件文件名：严格为 <UUID>.<jpg|jpeg|png|heic>，拒绝路径穿越。
func IsSafeAttachmentFilename(filename string) bool {
	if len(filename) == 0 || len(filename) > 64 {
		return false
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, `\`) || strings.Contains(filename, "..") {
		return false
	}
	parts := strings.Split(filename, ".")
	if len(parts) != 2 {
		return false
	}
	if !anyCaseUUID.MatchString(parts[0]) {
		return false
	}
	switch strings.ToLower(parts[1]) {
	case "jpg", "jpeg", "png", "heic":
		return true
	default:
		return false
	}
}

func MimeTypeForFilename(filename string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), ".")) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "heic":
		return "image/heic"
	default:
		return ""
	}
}

func FileExtForMimeType(mimeType string) string {
	switch mimeType {
	case "image/png":
		return "png"
	case "image/heic":
		return "heic"
	default:
		return "jpg"
	}
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ValidateWebURL 只接受无用户名密码的 HTTP/HTTPS 链接，长度上限 8192。
func ValidateWebURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxURLLength || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	return parsed.String(), true
}

// ValidateImageURL 外链图片只接受 HTTPS，不含用户名密码，长度上限 8192。
func ValidateImageURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxURLLength || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if strings.ToLower(parsed.Scheme) != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	return parsed.String(), true
}

func ImageCacheKey(raw string) (string, bool) {
	clean, ok := ValidateImageURL(raw)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String(), true
}

func DomainOfURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

// OutboxRow 是本地 sync_outbox 表的一行。
type OutboxRow struct {
	Seq         int64
	MutationID  string
	EntityID    string
	Operation   string
	CreatedAt   time.Time
	BaseVersion int64
	Payload     *string
	SentAt      *time.Time
	ConflictAt  *time.Time
}

// SyncConflict 是一条待用户处理的同步冲突。
type SyncConflict struct {
	EntityID     string
	Operation    string
	ConflictAt   time.Time
	ServerChange *Change
	LocalTitle   string
	DiaryDate    string
}

// SyncSummary 是一次同步的汇总计数。
type SyncSummary struct {
	Pushed                int
	Pulled                int
	Conflicts             int
	DownloadedAttachments int
}

// SyncStatus 描述当前云同步的汇总状态。
type SyncStatus struct {
	Configured    bool
	LoggedIn      bool
	AccountEmail  string
	OwnDeviceID   string
	PendingCount  int
	ConflictCount int
	LastSyncedAt  *time.Time
}

// MetadataResult 是新建日记自动采集地点 / 天气的结果。
type MetadataResult struct {
	Location        *LocationSnapshot
	Weather         *WeatherSnapshot
	LocationMessage string
	WeatherMessage  string
}
