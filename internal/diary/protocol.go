package diary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrMalformed 表示云同步报文不符合同步协议 v1。
var ErrMalformed = errors.New("sync protocol: malformed message")

const (
	EntityDiaryEntry = "diary_entry"

	OperationUpsert = "upsert"
	OperationDelete = "delete"

	StatusApplied   = "applied"
	StatusDuplicate = "duplicate"
	StatusConflict  = "conflict"

	UploadRequired  = "upload_required"
	AlreadyUploaded = "already_uploaded"

	MaxMutationsPerPush = 100
	MaxPayloadBytes     = 256 * 1024
)

var (
	canonicalID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	dateKeyRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

var protocolMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/heic": true,
}

var protocolPlatforms = map[string]bool{
	"ios":     true,
	"macos":   true,
	"windows": true,
	"linux":   true,
}

// ErrorCodes 定义同步协议 v1 中每个错误码是否可重试。
var ErrorCodes = map[string]bool{
	"invalid_request":      false,
	"unauthenticated":      false,
	"otp_invalid":          false,
	"device_revoked":       false,
	"not_found":            false,
	"mutation_id_reused":   false,
	"attachment_not_ready": true,
	"payload_too_large":    false,
	"rate_limited":         true,
	"unavailable":          true,
	"internal":             true,
}

// IsID 判断是否为小写规范 UUID。
func IsID(v string) bool { return canonicalID.MatchString(v) }

// IsSHA256 判断是否为 64 位小写十六进制 SHA-256。
func IsSHA256(v string) bool { return sha256HexRe.MatchString(v) }

type Mutation struct {
	MutationID  string          `json:"mutationId"`
	EntityType  string          `json:"entityType"`
	EntityID    string          `json:"entityId"`
	Operation   string          `json:"operation"`
	BaseVersion *int64          `json:"baseVersion"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type PushRequest struct {
	Mutations []Mutation `json:"mutations"`
}

type Change struct {
	Cursor     string          `json:"cursor"`
	EntityType string          `json:"entityType"`
	EntityID   string          `json:"entityId"`
	Version    int64           `json:"version"`
	Operation  string          `json:"operation"`
	Payload    json.RawMessage `json:"payload"`
}

type PushResult struct {
	MutationID string  `json:"mutationId"`
	Status     string  `json:"status"`
	Version    *int64  `json:"version,omitempty"`
	Cursor     *string `json:"cursor,omitempty"`
	Current    *Change `json:"current,omitempty"`
}

type PushResponse struct {
	Results []PushResult `json:"results"`
}

type PullResponse struct {
	Changes    []Change `json:"changes"`
	NextCursor *string  `json:"nextCursor"`
	HasMore    *bool    `json:"hasMore"`
}

type AttachmentRef struct {
	AttachmentID string    `json:"attachmentId"`
	OriginalName *string   `json:"originalName,omitempty"`
	Width        *int      `json:"width,omitempty"`
	Height       *int      `json:"height,omitempty"`
	AddedAt      time.Time `json:"addedAt"`
	MimeType     *string   `json:"mimeType"`
	SHA256       *string   `json:"sha256,omitempty"`
	Size         *int64    `json:"size,omitempty"`
}

type DiaryEntryPayload struct {
	Title        *string         `json:"title"`
	Markdown     *string         `json:"markdown"`
	DiaryDateKey string          `json:"diaryDateKey"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Tags         []string        `json:"tags"`
	Attachments  []AttachmentRef `json:"attachments"`
	LinkCards    json.RawMessage `json:"linkCards"`
	Location     json.RawMessage `json:"location"`
	Weather      json.RawMessage `json:"weather"`
}

type AttachmentUploadRequest struct {
	AttachmentID string `json:"attachmentId"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	MimeType     string `json:"mimeType"`
}

type SignedUpload struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

type AttachmentUploadURL struct {
	AttachmentID string        `json:"attachmentId"`
	Status       string        `json:"status"`
	Upload       *SignedUpload `json:"upload,omitempty"`
}

type AttachmentCompleteRequest struct {
	AttachmentID string `json:"attachmentId"`
}

type AttachmentMetadata struct {
	AttachmentID string    `json:"attachmentId"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mimeType"`
	CreatedAt    time.Time `json:"createdAt"`
}

type AttachmentDownloadURL struct {
	AttachmentID string    `json:"attachmentId"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expiresAt"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
}

type ErrorBody struct {
	Error struct {
		Code              string `json:"code"`
		Message           string `json:"message"`
		Retryable         *bool  `json:"retryable"`
		RetryAfterSeconds *int   `json:"retryAfterSeconds,omitempty"`
	} `json:"error"`
}

type AuthOTPRequest struct {
	Email string `json:"email"`
}

type AuthVerifyRequest struct {
	Email      string `json:"email"`
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

type EnrollRequest struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

type EnrollResponse struct {
	UserID           string `json:"userId"`
	DeviceID         string `json:"deviceId"`
	DeviceCredential string `json:"deviceCredential"`
}

type Device struct {
	DeviceID   string     `json:"deviceId"`
	DeviceName string     `json:"deviceName"`
	Platform   string     `json:"platform"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

func DecodeAuthOTPRequest(data []byte) (AuthOTPRequest, error) {
	var req AuthOTPRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, malformed("auth otp request: %v", err)
	}
	if !isValidEmail(req.Email) {
		return req, malformed("auth otp request: email")
	}
	return req, nil
}

func DecodeAuthVerifyRequest(data []byte) (AuthVerifyRequest, error) {
	var req AuthVerifyRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, malformed("auth verify request: %v", err)
	}
	name := []rune(req.DeviceName)
	if !isValidEmail(req.Email) || !isValidOTPCode(req.Code) || len(name) == 0 || len(name) > 100 || !protocolPlatforms[req.Platform] {
		return req, malformed("auth verify request: email, code, deviceName or platform")
	}
	return req, nil
}

func DecodeEnrollRequest(data []byte) (EnrollRequest, error) {
	var req EnrollRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, malformed("enroll request: %v", err)
	}
	name := []rune(req.DeviceName)
	if len(name) == 0 || len(name) > 100 || !protocolPlatforms[req.Platform] {
		return req, malformed("enroll request: deviceName or platform")
	}
	return req, nil
}

func DecodeEnrollResponse(data []byte) (EnrollResponse, error) {
	var resp EnrollResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("enroll response: %v", err)
	}
	if !canonicalID.MatchString(resp.UserID) || !canonicalID.MatchString(resp.DeviceID) || !strings.HasPrefix(resp.DeviceCredential, "bkd_") {
		return resp, malformed("enroll response: userId, deviceId or deviceCredential")
	}
	return resp, nil
}

func DecodeDevicesResponse(data []byte) (DevicesResponse, error) {
	var resp DevicesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("devices response: %v", err)
	}
	if resp.Devices == nil {
		return resp, malformed("devices response: missing devices")
	}
	for i, d := range resp.Devices {
		if !canonicalID.MatchString(d.DeviceID) || d.DeviceName == "" || !protocolPlatforms[d.Platform] || d.CreatedAt.IsZero() {
			return resp, malformed("device %d: invalid fields", i)
		}
	}
	return resp, nil
}

func DecodePushRequest(data []byte) (PushRequest, error) {
	var req PushRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, malformed("push request: %v", err)
	}
	if len(req.Mutations) == 0 || len(req.Mutations) > MaxMutationsPerPush {
		return req, malformed("push request: %d mutations", len(req.Mutations))
	}
	for i, m := range req.Mutations {
		if err := validateMutation(m); err != nil {
			return req, malformed("mutation %d: %v", i, err)
		}
	}
	return req, nil
}

func DecodePushResponse(data []byte) (PushResponse, error) {
	var resp PushResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("push response: %v", err)
	}
	if resp.Results == nil {
		return resp, malformed("push response: missing results")
	}
	for i, r := range resp.Results {
		if err := validateResult(r); err != nil {
			return resp, malformed("result %d: %v", i, err)
		}
	}
	return resp, nil
}

func DecodePullResponse(data []byte) (PullResponse, error) {
	var resp PullResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("pull response: %v", err)
	}
	if resp.Changes == nil || resp.NextCursor == nil || *resp.NextCursor == "" || resp.HasMore == nil {
		return resp, malformed("pull response: missing changes, nextCursor or hasMore")
	}
	for i, c := range resp.Changes {
		if err := validateChange(c); err != nil {
			return resp, malformed("change %d: %v", i, err)
		}
	}
	return resp, nil
}

func DecodeAttachmentUploadRequest(data []byte) (AttachmentUploadRequest, error) {
	var req AttachmentUploadRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, malformed("upload request: %v", err)
	}
	if err := validateBlob(req.AttachmentID, req.SHA256, req.Size); err != nil {
		return req, malformed("upload request: %v", err)
	}
	if !protocolMimeTypes[req.MimeType] {
		return req, malformed("upload request: mime type %q", req.MimeType)
	}
	return req, nil
}

func DecodeAttachmentUploadURL(data []byte) (AttachmentUploadURL, error) {
	var resp AttachmentUploadURL
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("upload url: %v", err)
	}
	if !canonicalID.MatchString(resp.AttachmentID) {
		return resp, malformed("upload url: attachmentId")
	}
	switch resp.Status {
	case AlreadyUploaded:
		return resp, nil
	case UploadRequired:
		if resp.Upload == nil || resp.Upload.URL == "" || resp.Upload.Method == "" || resp.Upload.ExpiresAt.IsZero() {
			return resp, malformed("upload url: missing signed upload")
		}
		return resp, nil
	default:
		return resp, malformed("upload url: status %q", resp.Status)
	}
}

func DecodeAttachmentMetadata(data []byte) (AttachmentMetadata, error) {
	var resp AttachmentMetadata
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("attachment metadata: %v", err)
	}
	if err := validateBlob(resp.AttachmentID, resp.SHA256, resp.Size); err != nil {
		return resp, malformed("attachment metadata: %v", err)
	}
	if !protocolMimeTypes[resp.MimeType] || resp.CreatedAt.IsZero() {
		return resp, malformed("attachment metadata: mimeType or createdAt")
	}
	return resp, nil
}

func DecodeAttachmentDownloadURL(data []byte) (AttachmentDownloadURL, error) {
	var resp AttachmentDownloadURL
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, malformed("download url: %v", err)
	}
	if resp.URL == "" || resp.ExpiresAt.IsZero() {
		return resp, malformed("download url: missing url or expiresAt")
	}
	if err := validateBlob(resp.AttachmentID, resp.SHA256, resp.Size); err != nil {
		return resp, malformed("download url: %v", err)
	}
	return resp, nil
}

func DecodeError(data []byte) (ErrorBody, error) {
	var body ErrorBody
	if err := json.Unmarshal(data, &body); err != nil {
		return body, malformed("error: %v", err)
	}
	retryable, known := ErrorCodes[body.Error.Code]
	if !known || body.Error.Retryable == nil || *body.Error.Retryable != retryable {
		return body, malformed("error: code %q", body.Error.Code)
	}
	return body, nil
}

func DecodeDiaryEntryPayload(data []byte) (DiaryEntryPayload, error) {
	var p DiaryEntryPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return p, malformed("diary payload: %v", err)
	}
	if p.Title == nil || p.Markdown == nil || p.Tags == nil || p.Attachments == nil {
		return p, malformed("diary payload: missing title, markdown, tags or attachments")
	}
	if _, err := time.Parse(time.DateOnly, p.DiaryDateKey); err != nil || !dateKeyRe.MatchString(p.DiaryDateKey) {
		return p, malformed("diary payload: diaryDateKey %q", p.DiaryDateKey)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return p, malformed("diary payload: missing timestamps")
	}
	for _, a := range p.Attachments {
		if !canonicalID.MatchString(a.AttachmentID) || a.AddedAt.IsZero() {
			return p, malformed("diary payload: attachment %q", a.AttachmentID)
		}
		if a.MimeType == nil || !protocolMimeTypes[*a.MimeType] {
			return p, malformed("diary payload: attachment %q mimeType", a.AttachmentID)
		}
		if a.SHA256 != nil && !sha256HexRe.MatchString(*a.SHA256) {
			return p, malformed("diary payload: attachment %q sha256", a.AttachmentID)
		}
		if a.Size != nil && (*a.Size <= 0 || *a.Size > MaxAttachmentBytes) {
			return p, malformed("diary payload: attachment %q size", a.AttachmentID)
		}
	}
	return p, nil
}

// EntryToPayload 把本地日记条目编码为同步协议完整状态。
func EntryToPayload(entry Entry) (DiaryEntryPayload, error) {
	title := entry.Title
	markdown := entry.Markdown
	tags := entry.Tags
	if tags == nil {
		tags = []string{}
	}
	refs := make([]AttachmentRef, 0, len(entry.Attachments))
	for _, att := range entry.Attachments {
		mime := ""
		if att.MimeType != nil && protocolMimeTypes[*att.MimeType] {
			mime = *att.MimeType
		} else if inferred := MimeTypeForFilename(att.Filename); inferred != "" {
			mime = inferred
		} else {
			mime = "image/jpeg"
		}
		mimeCopy := mime
		refs = append(refs, AttachmentRef{
			AttachmentID: strings.ToLower(att.ID),
			OriginalName: att.OriginalName,
			Width:        att.Width,
			Height:       att.Height,
			AddedAt:      att.AddedAt.UTC(),
			MimeType:     &mimeCopy,
			SHA256:       att.SHA256,
			Size:         att.Size,
		})
	}
	cards := entry.LinkCards
	if cards == nil {
		cards = []LinkCard{}
	}
	linkCardsRaw, err := json.Marshal(cards)
	if err != nil {
		return DiaryEntryPayload{}, err
	}
	locationRaw := json.RawMessage("null")
	if entry.Location != nil {
		locationRaw, err = json.Marshal(entry.Location)
		if err != nil {
			return DiaryEntryPayload{}, err
		}
	}
	weatherRaw := json.RawMessage("null")
	if entry.Weather != nil {
		weatherRaw, err = json.Marshal(entry.Weather)
		if err != nil {
			return DiaryEntryPayload{}, err
		}
	}
	return DiaryEntryPayload{
		Title:        &title,
		Markdown:     &markdown,
		DiaryDateKey: entry.DiaryDateKey.String(),
		CreatedAt:    entry.CreatedAt.UTC(),
		UpdatedAt:    entry.UpdatedAt.UTC(),
		Tags:         tags,
		Attachments:  refs,
		LinkCards:    linkCardsRaw,
		Location:     locationRaw,
		Weather:      weatherRaw,
	}, nil
}

// EntryFromPayload 把已校验的同步协议 payload 转换为本地日记条目。
func EntryFromPayload(entityID string, payload DiaryEntryPayload) (Entry, error) {
	key, err := ParseDateKey(payload.DiaryDateKey)
	if err != nil {
		return Entry{}, err
	}
	attachments := make([]Attachment, 0, len(payload.Attachments))
	for _, ref := range payload.Attachments {
		mime := "image/jpeg"
		if ref.MimeType != nil {
			mime = *ref.MimeType
		}
		mimeCopy := mime
		attachments = append(attachments, Attachment{
			ID:           ref.AttachmentID,
			Filename:     ref.AttachmentID + "." + FileExtForMimeType(mime),
			OriginalName: ref.OriginalName,
			Width:        ref.Width,
			Height:       ref.Height,
			AddedAt:      ref.AddedAt.UTC(),
			SHA256:       ref.SHA256,
			Size:         ref.Size,
			MimeType:     &mimeCopy,
		})
	}
	cards := []LinkCard{}
	if len(payload.LinkCards) > 0 && !bytes.Equal(bytes.TrimSpace(payload.LinkCards), []byte("null")) {
		var decoded []LinkCard
		if err := json.Unmarshal(payload.LinkCards, &decoded); err == nil {
			for _, card := range decoded {
				if clean, ok := ValidateWebURL(card.URL); ok {
					card.URL = clean
					cards = append(cards, card)
				}
			}
		}
	}
	var location *LocationSnapshot
	if len(payload.Location) > 0 && !bytes.Equal(bytes.TrimSpace(payload.Location), []byte("null")) {
		var decoded LocationSnapshot
		if err := json.Unmarshal(payload.Location, &decoded); err == nil && strings.TrimSpace(decoded.Place) != "" {
			location = &decoded
		}
	}
	var weather *WeatherSnapshot
	if len(payload.Weather) > 0 && !bytes.Equal(bytes.TrimSpace(payload.Weather), []byte("null")) {
		var decoded WeatherSnapshot
		if err := json.Unmarshal(payload.Weather, &decoded); err == nil && strings.TrimSpace(decoded.Description) != "" {
			weather = &decoded
		}
	}
	tags := payload.Tags
	if tags == nil {
		tags = []string{}
	}
	return Entry{
		ID:           strings.ToLower(entityID),
		Title:        *payload.Title,
		Markdown:     *payload.Markdown,
		DiaryDateKey: key,
		CreatedAt:    payload.CreatedAt.UTC(),
		UpdatedAt:    payload.UpdatedAt.UTC(),
		Tags:         UniqueTags(tags),
		Attachments:  attachments,
		LinkCards:    cards,
		Location:     location,
		Weather:      weather,
	}, nil
}

func validateMutation(m Mutation) error {
	if !canonicalID.MatchString(m.MutationID) || !canonicalID.MatchString(m.EntityID) {
		return errors.New("ids must be lowercase canonical UUIDs")
	}
	if m.EntityType != EntityDiaryEntry {
		return fmt.Errorf("entity type %q", m.EntityType)
	}
	if m.BaseVersion == nil || *m.BaseVersion < 0 {
		return errors.New("baseVersion must be >= 0")
	}
	if len(m.Payload) > MaxPayloadBytes {
		return errors.New("payload too large")
	}
	return validatePayload(m.Operation, m.Payload)
}

func validateResult(r PushResult) error {
	if !canonicalID.MatchString(r.MutationID) {
		return errors.New("mutationId")
	}
	switch r.Status {
	case StatusApplied, StatusDuplicate:
		if r.Version == nil || *r.Version < 1 || r.Cursor == nil || *r.Cursor == "" || r.Current != nil {
			return fmt.Errorf("%s needs version and cursor only", r.Status)
		}
	case StatusConflict:
		if r.Current == nil || r.Version != nil || r.Cursor != nil {
			return errors.New("conflict needs current only")
		}
		return validateChange(*r.Current)
	default:
		return fmt.Errorf("status %q", r.Status)
	}
	return nil
}

func validateChange(c Change) error {
	if c.Cursor == "" || !canonicalID.MatchString(c.EntityID) || c.Version < 1 {
		return errors.New("cursor, entityId or version")
	}
	if c.EntityType != EntityDiaryEntry {
		return fmt.Errorf("entity type %q", c.EntityType)
	}
	return validatePayload(c.Operation, c.Payload)
}

func validatePayload(operation string, payload json.RawMessage) error {
	empty := len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null"))
	switch operation {
	case OperationUpsert:
		if empty {
			return errors.New("upsert without payload")
		}
		_, err := DecodeDiaryEntryPayload(payload)
		return err
	case OperationDelete:
		if !empty {
			return errors.New("delete with payload")
		}
		return nil
	default:
		return fmt.Errorf("operation %q", operation)
	}
}

func validateBlob(id, sum string, size int64) error {
	if !canonicalID.MatchString(id) || !sha256HexRe.MatchString(sum) || size <= 0 || size > MaxAttachmentBytes {
		return errors.New("attachmentId, sha256 or size")
	}
	return nil
}

func isValidEmail(raw string) bool {
	if raw == "" || len([]rune(raw)) > 254 {
		return false
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	local, domain, ok := strings.Cut(raw, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") || !strings.Contains(domain, ".") {
		return false
	}
	return true
}

func isValidOTPCode(raw string) bool {
	if len(raw) < 6 || len(raw) > 12 {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}
