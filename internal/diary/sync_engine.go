package diary

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

const (
	SyncProviderID      = "bokuSync"
	DeviceCredentialKey = "bokuSync.deviceCredential"
	DeviceIDKey         = "bokuSync.deviceId"
	AccountEmailKey     = "bokuSync.accountEmail"

	syncPullLimit  = 200
	maxSyncRetries = 4
)

// CurrentPlatform 返回当前桌面运行环境对应的同步协议 platform 字段值。
func CurrentPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// DefaultDeviceName 返回登录注册设备时的默认设备名（主机名兜底为 "Boku Desktop"）。
func DefaultDeviceName() string {
	name, err := os.Hostname()
	if err != nil {
		return "Boku Desktop"
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, ".local"))
	if name == "" {
		return "Boku Desktop"
	}
	runes := []rune(name)
	if len(runes) > 100 {
		return string(runes[:100])
	}
	return name
}

// SyncEngine 负责桌面端日记云同步：OTP 登录换设备凭据、push / pull、附件上传下载与校验、
// 指数退避重试及冲突存档与解决。所有同步变更操作在互斥锁下序列化执行。
type SyncEngine struct {
	mu        sync.Mutex
	store     *Store
	getConfig func() (SyncConfiguration, bool)
	http      RawRequester
	secrets   domain.SecretStore
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
	jitter    func() float64
}

type SyncEngineOption func(*SyncEngine)

func WithSleep(fn func(context.Context, time.Duration) error) SyncEngineOption {
	return func(e *SyncEngine) {
		if fn != nil {
			e.sleep = fn
		}
	}
}

func WithJitter(fn func() float64) SyncEngineOption {
	return func(e *SyncEngine) {
		if fn != nil {
			e.jitter = fn
		}
	}
}

func NewSyncEngine(
	store *Store,
	getConfig func() (SyncConfiguration, bool),
	http RawRequester,
	secrets domain.SecretStore,
	clock domain.Clock,
	opts ...SyncEngineOption,
) *SyncEngine {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	e := &SyncEngine{
		store:     store,
		getConfig: getConfig,
		http:      http,
		secrets:   secrets,
		now:       clock.Now,
		sleep:     sleepContext,
		jitter:    defaultJitter,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

func (e *SyncEngine) IsConfigured() bool {
	if e == nil || e.getConfig == nil || e.http == nil {
		return false
	}
	_, ok := e.getConfig()
	return ok
}

func (e *SyncEngine) IsLoggedIn(ctx context.Context) bool {
	_, err := e.getCredential(ctx)
	return err == nil
}

func (e *SyncEngine) SendLoginCode(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	if !isValidEmail(email) {
		return domain.ErrInvalidArgument
	}
	client, err := e.client()
	if err != nil {
		return err
	}
	return client.SendOTP(ctx, email)
}

// Login 向同步服务端提交 6 位邮箱验证码并注册本机，只保存设备凭据；切换账号时清掉旧游标。
func (e *SyncEngine) Login(ctx context.Context, email, code, deviceName string) error {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		deviceName = DefaultDeviceName()
	}
	if !isValidEmail(email) || !isValidOTPCode(code) {
		return domain.ErrInvalidArgument
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	client, err := e.client()
	if err != nil {
		return err
	}
	enrolled, err := client.VerifyOTP(ctx, email, code, deviceName, CurrentPlatform())
	if err != nil {
		return err
	}
	prevUserID, _, _, err := e.store.SyncState()
	if err != nil {
		return err
	}
	if prevUserID != enrolled.UserID {
		if err := e.store.SetSyncCursor(""); err != nil {
			return err
		}
	}
	if err := e.store.SetSyncUserID(enrolled.UserID); err != nil {
		return err
	}
	meta := domain.SecretMeta{ProviderID: SyncProviderID, Account: email}
	if err := e.secrets.Put(ctx, DeviceCredentialKey, []byte(enrolled.DeviceCredential), meta); err != nil {
		return err
	}
	if err := e.secrets.Put(ctx, DeviceIDKey, []byte(enrolled.DeviceID), meta); err != nil {
		return err
	}
	if err := e.secrets.Put(ctx, AccountEmailKey, []byte(email), meta); err != nil {
		return err
	}
	return nil
}

// Logout 尽力撤销本机设备并清除本地同步凭据，保留全部本地日记与附件。
func (e *SyncEngine) Logout(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.logoutLocked(ctx)
}

func (e *SyncEngine) logoutLocked(ctx context.Context) error {
	if client, err := e.client(); err == nil {
		if cred, credErr := e.getCredential(ctx); credErr == nil {
			if ownID := e.ownDeviceID(ctx); ownID != "" {
				_ = client.RevokeDevice(ctx, cred, ownID)
			}
		}
	}
	if err := e.clearCredentials(ctx); err != nil {
		return err
	}
	return e.store.SetSyncUserID("")
}

// SyncNow 执行一轮完整的同步：push 待发送变更 → 分页 pull 远端变更 → 补下载缺失附件。
// 未登录时直接返回空汇总，不报错。
func (e *SyncEngine) SyncNow(ctx context.Context) (SyncSummary, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cred, err := e.getCredential(ctx)
	if errors.Is(err, domain.ErrSecretNotFound) || errors.Is(err, ErrNotLoggedIn) {
		return SyncSummary{}, nil
	}
	if err != nil {
		return SyncSummary{}, err
	}
	client, err := e.client()
	if err != nil {
		return SyncSummary{}, err
	}

	now := e.now().UTC()
	var summary SyncSummary
	pushed, conflicts, err := e.pushPendingLocked(ctx, client, cred, now)
	if err != nil {
		e.handleRevokedIfNeeded(ctx, err)
		return SyncSummary{}, err
	}
	summary.Pushed = pushed
	summary.Conflicts = conflicts

	pulled, err := e.pullAllLocked(ctx, client, cred, now)
	if err != nil {
		e.handleRevokedIfNeeded(ctx, err)
		return summary, err
	}
	summary.Pulled = pulled

	downloaded, err := e.repairAttachmentsLocked(ctx, client, cred)
	if err != nil {
		e.handleRevokedIfNeeded(ctx, err)
		return summary, err
	}
	summary.DownloadedAttachments = downloaded
	return summary, nil
}

func (e *SyncEngine) Status(ctx context.Context) (SyncStatus, error) {
	status := SyncStatus{
		Configured: e.IsConfigured(),
		LoggedIn:   e.IsLoggedIn(ctx),
	}
	pending, conflicts, err := e.store.OutboxCounts()
	if err != nil {
		return SyncStatus{}, err
	}
	_, _, lastSyncedAt, err := e.store.SyncState()
	if err != nil {
		return SyncStatus{}, err
	}
	status.PendingCount = pending
	status.ConflictCount = conflicts
	status.LastSyncedAt = lastSyncedAt
	if status.LoggedIn {
		status.AccountEmail = e.secretText(ctx, AccountEmailKey)
		status.OwnDeviceID = e.ownDeviceID(ctx)
	}
	return status, nil
}

func (e *SyncEngine) ListDevices(ctx context.Context) ([]Device, error) {
	cred, err := e.getCredential(ctx)
	if err != nil {
		return nil, ErrNotLoggedIn
	}
	client, err := e.client()
	if err != nil {
		return nil, err
	}
	devices, err := client.ListDevices(ctx, cred)
	if err != nil {
		e.handleRevokedIfNeeded(ctx, err)
		return nil, err
	}
	return devices, nil
}

// RevokeDevice 撤销指定设备；撤销本机等同退出登录。
func (e *SyncEngine) RevokeDevice(ctx context.Context, deviceID string) error {
	deviceID = strings.ToLower(strings.TrimSpace(deviceID))
	if !IsID(deviceID) {
		return domain.ErrInvalidArgument
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ownDeviceID(ctx) == deviceID {
		return e.logoutLocked(ctx)
	}
	cred, err := e.getCredential(ctx)
	if err != nil {
		return ErrNotLoggedIn
	}
	client, err := e.client()
	if err != nil {
		return err
	}
	err = client.RevokeDevice(ctx, cred, deviceID)
	if err != nil {
		e.handleRevokedIfNeeded(ctx, err)
	}
	return err
}

func (e *SyncEngine) Conflicts(ctx context.Context) ([]SyncConflict, error) {
	_ = ctx
	rows, err := e.store.ConflictedOutbox()
	if err != nil {
		return nil, err
	}
	currents := e.loadConflictCurrents()
	out := make([]SyncConflict, 0, len(rows))
	for _, row := range rows {
		at := row.CreatedAt
		if row.ConflictAt != nil {
			at = *row.ConflictAt
		}
		var serverChange *Change
		if c, ok := currents[row.EntityID]; ok {
			copyChange := c
			serverChange = &copyChange
		}
		title := ""
		date := ""
		if entry, err := e.store.Entry(row.EntityID, true); err == nil && entry != nil {
			title = strings.TrimSpace(entry.Title)
			if title == "" {
				title = Summary(entry.Markdown, 24)
			}
			date = entry.DiaryDateKey.String()
		}
		out = append(out, SyncConflict{
			EntityID:     row.EntityID,
			Operation:    row.Operation,
			ConflictAt:   at,
			ServerChange: serverChange,
			LocalTitle:   title,
			DiaryDate:    date,
		})
	}
	return out, nil
}

// ResolveConflictKeepCloud 丢弃本地冲突修改并应用云端当前版本；若本地未存档服务端版本则清空游标留待下次重拉。
func (e *SyncEngine) ResolveConflictKeepCloud(ctx context.Context, entityID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resolveKeepCloudLocked(ctx, entityID)
}

func (e *SyncEngine) resolveKeepCloudLocked(ctx context.Context, entityID string) error {
	entityID = strings.ToLower(strings.TrimSpace(entityID))
	if !IsID(entityID) {
		return domain.ErrInvalidArgument
	}
	currents := e.loadConflictCurrents()
	var current *Change
	if c, ok := currents[entityID]; ok {
		copyChange := c
		current = &copyChange
		delete(currents, entityID)
	}
	if err := e.store.ResolveConflict(entityID, current, e.now().UTC()); err != nil {
		return err
	}
	if current == nil {
		if err := e.store.SetSyncCursor(""); err != nil {
			return err
		}
	} else if current.Operation == OperationUpsert {
		if payload, err := DecodeDiaryEntryPayload(current.Payload); err == nil {
			if client, cErr := e.client(); cErr == nil {
				if cred, credErr := e.getCredential(ctx); credErr == nil {
					e.downloadMissingAttachmentsLocked(ctx, client, cred, payload)
				}
			}
		}
	}
	e.persistConflictCurrents(currents)
	return nil
}

// ResolveConflictKeepLocalAsNew 把本地版本另存为一条新 ID 的日记，再对原条目执行保留云端。
func (e *SyncEngine) ResolveConflictKeepLocalAsNew(ctx context.Context, entityID string) (string, error) {
	entityID = strings.ToLower(strings.TrimSpace(entityID))
	if !IsID(entityID) {
		return "", domain.ErrInvalidArgument
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	local, err := e.store.Entry(entityID, true)
	if err != nil {
		return "", err
	}
	if local == nil {
		return "", ErrMissingEntry
	}
	newID, err := domain.NewID()
	if err != nil {
		return "", err
	}
	copyEntry := *local
	copyEntry.ID = newID
	copyEntry.DeletedAt = nil
	copyEntry.SyncVersion = 0
	saved, err := e.store.Save(copyEntry, nil, nil, e.now())
	if err != nil {
		return "", err
	}
	if err := e.resolveKeepCloudLocked(ctx, entityID); err != nil {
		return "", err
	}
	return saved.ID, nil
}

func (e *SyncEngine) pushPendingLocked(ctx context.Context, client *SyncClient, cred string, now time.Time) (int, int, error) {
	pushed := 0
	conflicts := 0
	for {
		rows, err := e.store.PendingOutbox(MaxMutationsPerPush)
		if err != nil {
			return pushed, conflicts, err
		}
		if len(rows) == 0 {
			break
		}
		mutations := make([]Mutation, 0, len(rows))
		for _, row := range rows {
			var wirePayload json.RawMessage
			if row.SentAt == nil {
				p, err := e.prepareWirePayloadLocked(ctx, client, cred, row)
				if err != nil {
					return pushed, conflicts, err
				}
				if err := e.store.FreezeOutbox(row.Seq, now); err != nil {
					return pushed, conflicts, err
				}
				wirePayload = p
			} else if row.Operation == OperationUpsert && row.Payload != nil {
				wirePayload = json.RawMessage(*row.Payload)
			}
			m, err := mutationFromOutbox(row, wirePayload)
			if err != nil {
				return pushed, conflicts, err
			}
			mutations = append(mutations, m)
		}

		results, err := retrySync(ctx, e, func() ([]PushResult, error) {
			return client.Push(ctx, cred, mutations)
		})
		if err != nil {
			return pushed, conflicts, err
		}
		byID := make(map[string]PushResult, len(results))
		for _, res := range results {
			if _, dup := byID[res.MutationID]; dup {
				return pushed, conflicts, &SyncError{Kind: SyncErrMalformed}
			}
			byID[res.MutationID] = res
		}
		for _, row := range rows {
			res, ok := byID[row.MutationID]
			if !ok {
				return pushed, conflicts, &SyncError{Kind: SyncErrMalformed}
			}
			switch res.Status {
			case StatusApplied, StatusDuplicate:
				if res.Version == nil {
					return pushed, conflicts, &SyncError{Kind: SyncErrMalformed}
				}
				if err := e.store.ConfirmOutboxApplied(row.MutationID, *res.Version); err != nil {
					return pushed, conflicts, err
				}
				pushed++
			case StatusConflict:
				if res.Current == nil {
					return pushed, conflicts, &SyncError{Kind: SyncErrMalformed}
				}
				if err := e.store.MarkOutboxConflict(row.MutationID, now); err != nil {
					return pushed, conflicts, err
				}
				e.persistConflictCurrent(*res.Current)
				conflicts++
			default:
				return pushed, conflicts, &SyncError{Kind: SyncErrMalformed}
			}
		}
		if len(rows) < MaxMutationsPerPush {
			break
		}
	}
	return pushed, conflicts, nil
}

// prepareWirePayloadLocked 为未发送的 upsert 行补传附件；本地文件已丢失的附件引用
// 不写入线上 payload（避免服务端永远返回 attachment_not_ready），本地数据库保持不变。
func (e *SyncEngine) prepareWirePayloadLocked(ctx context.Context, client *SyncClient, cred string, row OutboxRow) (json.RawMessage, error) {
	if row.Operation != OperationUpsert || row.Payload == nil {
		return nil, nil
	}
	payload, err := DecodeDiaryEntryPayload([]byte(*row.Payload))
	if err != nil {
		return nil, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	if len(payload.Attachments) == 0 {
		return json.RawMessage(*row.Payload), nil
	}
	entry, err := e.store.Entry(row.EntityID, false)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return json.RawMessage(*row.Payload), nil
	}
	byID := make(map[string]Attachment, len(entry.Attachments))
	for _, att := range entry.Attachments {
		byID[att.ID] = att
	}
	kept := make([]AttachmentRef, 0, len(payload.Attachments))
	for _, ref := range payload.Attachments {
		local, ok := byID[ref.AttachmentID]
		if !ok || !e.store.AttachmentExists(local.Filename) {
			continue
		}
		if err := e.ensureUploadedLocked(ctx, client, cred, local); err != nil {
			return nil, err
		}
		kept = append(kept, ref)
	}
	payload.Attachments = kept
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func (e *SyncEngine) ensureUploadedLocked(ctx context.Context, client *SyncClient, cred string, att Attachment) error {
	data, err := e.store.ReadAttachment(att.Filename)
	if err != nil {
		return err
	}
	sum := SHA256Hex(data)
	if att.SHA256 != nil && *att.SHA256 != "" {
		sum = *att.SHA256
	}
	size := int64(len(data))
	if att.Size != nil && *att.Size > 0 {
		size = *att.Size
	}
	mime := MimeTypeForFilename(att.Filename)
	if att.MimeType != nil && *att.MimeType != "" {
		mime = *att.MimeType
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	req := AttachmentUploadRequest{
		AttachmentID: att.ID,
		SHA256:       sum,
		Size:         size,
		MimeType:     mime,
	}
	resp, err := retrySync(ctx, e, func() (AttachmentUploadURL, error) {
		return client.AttachmentUploadURL(ctx, cred, req)
	})
	if err != nil {
		return err
	}
	if resp.Status != UploadRequired || resp.Upload == nil {
		return nil
	}
	if _, err := retrySync(ctx, e, func() (struct{}, error) {
		return struct{}{}, client.UploadBlob(ctx, *resp.Upload, data)
	}); err != nil {
		return err
	}
	_, err = retrySync(ctx, e, func() (AttachmentMetadata, error) {
		return client.AttachmentComplete(ctx, cred, att.ID)
	})
	return err
}

func (e *SyncEngine) pullAllLocked(ctx context.Context, client *SyncClient, cred string, now time.Time) (int, error) {
	_, cursor, _, err := e.store.SyncState()
	if err != nil {
		return 0, err
	}
	pulled := 0
	for {
		page, err := retrySync(ctx, e, func() (PullResponse, error) {
			return client.Pull(ctx, cred, cursor, syncPullLimit)
		})
		if err != nil {
			return pulled, err
		}
		applied, err := e.store.ApplyPullPage(page.Changes, *page.NextCursor, now)
		if err != nil {
			return pulled, err
		}
		pulled += len(applied)
		for _, payload := range applied {
			e.downloadMissingAttachmentsLocked(ctx, client, cred, payload)
		}
		cursor = *page.NextCursor
		if page.HasMore == nil || !*page.HasMore {
			break
		}
	}
	return pulled, nil
}

func (e *SyncEngine) repairAttachmentsLocked(ctx context.Context, client *SyncClient, cred string) (int, error) {
	missing, err := e.store.MissingAttachmentFiles()
	if err != nil {
		return 0, err
	}
	downloaded := 0
	for _, att := range missing {
		if err := e.downloadAttachmentLocked(ctx, client, cred, att.ID, att.Filename); err != nil {
			var syncErr *SyncError
			if errors.As(err, &syncErr) && syncErr.IsDeviceRevoked() {
				return downloaded, err
			}
			continue
		}
		downloaded++
	}
	return downloaded, nil
}

func (e *SyncEngine) downloadMissingAttachmentsLocked(ctx context.Context, client *SyncClient, cred string, payload DiaryEntryPayload) {
	for _, ref := range payload.Attachments {
		mime := "image/jpeg"
		if ref.MimeType != nil {
			mime = *ref.MimeType
		}
		filename := ref.AttachmentID + "." + FileExtForMimeType(mime)
		if !e.store.AttachmentExists(filename) {
			_ = e.downloadAttachmentLocked(ctx, client, cred, ref.AttachmentID, filename)
		}
	}
}

func (e *SyncEngine) downloadAttachmentLocked(ctx context.Context, client *SyncClient, cred, attachmentID, filename string) error {
	info, err := retrySync(ctx, e, func() (AttachmentDownloadURL, error) {
		return client.AttachmentDownloadURL(ctx, cred, attachmentID)
	})
	if err != nil {
		return err
	}
	data, err := retrySync(ctx, e, func() ([]byte, error) {
		return client.DownloadBlob(ctx, info.URL)
	})
	if err != nil {
		return err
	}
	if int64(len(data)) != info.Size || SHA256Hex(data) != info.SHA256 {
		return &SyncError{Kind: SyncErrMalformed}
	}
	return e.store.WriteAttachment(data, filename)
}

// retrySync 按指数退避策略重试可恢复错误：5s 起步、翻倍、±20% 抖动、上限 1 小时，
// 若服务端给了 retryAfterSeconds 则不早于该时长；最多尝试 4 次。
func retrySync[T any](ctx context.Context, e *SyncEngine, op func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		res, err := op()
		if err == nil {
			return res, nil
		}
		if attempt >= maxSyncRetries || !isRetryableSyncError(err) {
			return zero, err
		}
		delay := BackoffDelay(attempt, e.jitter())
		var syncErr *SyncError
		if errors.As(err, &syncErr) && syncErr.RetryAfterSeconds != nil && *syncErr.RetryAfterSeconds > 0 {
			after := time.Duration(*syncErr.RetryAfterSeconds) * time.Second
			if after > delay {
				delay = after
			}
		}
		if sleepErr := e.sleep(ctx, delay); sleepErr != nil {
			return zero, sleepErr
		}
	}
}

// BackoffDelay 返回第 attempt 次重试（从 1 开始）在给定抖动系数下的等待时长。
func BackoffDelay(attempt int, jitter float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	baseSec := math.Min(3600.0, 5.0*math.Pow(2.0, float64(attempt-1)))
	return time.Duration(baseSec * jitter * float64(time.Second))
}

func isRetryableSyncError(err error) bool {
	var syncErr *SyncError
	if errors.As(err, &syncErr) {
		return syncErr.Retryable
	}
	return errors.Is(err, domain.ErrProviderTimeout) ||
		errors.Is(err, domain.ErrProviderUnavailable) ||
		errors.Is(err, domain.ErrProviderRateLimited)
}

func (e *SyncEngine) client() (*SyncClient, error) {
	if e == nil || e.getConfig == nil || e.http == nil {
		return nil, ErrSyncNotConfigured
	}
	cfg, ok := e.getConfig()
	if !ok {
		return nil, ErrSyncNotConfigured
	}
	return NewSyncClient(cfg, e.http), nil
}

func (e *SyncEngine) getCredential(ctx context.Context) (string, error) {
	if e == nil || e.secrets == nil {
		return "", ErrNotLoggedIn
	}
	sec, err := e.secrets.Get(ctx, DeviceCredentialKey)
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(sec.Text())
	if val == "" {
		return "", ErrNotLoggedIn
	}
	return val, nil
}

func (e *SyncEngine) secretText(ctx context.Context, key string) string {
	if e == nil || e.secrets == nil {
		return ""
	}
	sec, err := e.secrets.Get(ctx, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(sec.Text())
}

func (e *SyncEngine) ownDeviceID(ctx context.Context) string {
	raw := strings.ToLower(e.secretText(ctx, DeviceIDKey))
	if !IsID(raw) {
		return ""
	}
	return raw
}

func (e *SyncEngine) clearCredentials(ctx context.Context) error {
	if e == nil || e.secrets == nil {
		return nil
	}
	for _, key := range []string{DeviceCredentialKey, DeviceIDKey, AccountEmailKey} {
		if err := e.secrets.Delete(ctx, key); err != nil && !errors.Is(err, domain.ErrSecretNotFound) {
			return err
		}
	}
	return nil
}

func (e *SyncEngine) handleRevokedIfNeeded(ctx context.Context, err error) {
	var syncErr *SyncError
	if errors.As(err, &syncErr) && syncErr.IsDeviceRevoked() {
		_ = e.clearCredentials(ctx)
	}
}

func (e *SyncEngine) loadConflictCurrents() map[string]Change {
	data, err := os.ReadFile(e.store.ConflictsPath())
	if err != nil {
		return map[string]Change{}
	}
	var decoded map[string]Change
	if err := json.Unmarshal(data, &decoded); err != nil || decoded == nil {
		return map[string]Change{}
	}
	return decoded
}

func (e *SyncEngine) persistConflictCurrent(current Change) {
	currents := e.loadConflictCurrents()
	currents[strings.ToLower(current.EntityID)] = current
	e.persistConflictCurrents(currents)
}

func (e *SyncEngine) persistConflictCurrents(currents map[string]Change) {
	if len(currents) == 0 {
		_ = os.Remove(e.store.ConflictsPath())
		return
	}
	data, err := json.Marshal(currents)
	if err != nil {
		return
	}
	_ = atomicfile.Write(e.store.ConflictsPath(), data)
}

func mutationFromOutbox(row OutboxRow, wirePayload json.RawMessage) (Mutation, error) {
	if row.Operation != OperationUpsert && row.Operation != OperationDelete {
		return Mutation{}, &SyncError{Kind: SyncErrMalformed}
	}
	baseVer := row.BaseVersion
	m := Mutation{
		MutationID:  row.MutationID,
		EntityType:  EntityDiaryEntry,
		EntityID:    row.EntityID,
		Operation:   row.Operation,
		BaseVersion: &baseVer,
	}
	if row.Operation == OperationUpsert {
		m.Payload = wirePayload
	}
	return m, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func defaultJitter() float64 {
	return 0.8 + rand.Float64()*0.4
}

// SyncUserMessage 只按错误码返回面向用户的中文提示，绝不透传服务端 message、凭据或签名 URL。
func SyncUserMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrSyncNotConfigured) {
		return "请先配置同步服务地址。"
	}
	if errors.Is(err, ErrNotLoggedIn) || errors.Is(err, domain.ErrSecretNotFound) || errors.Is(err, domain.ErrCredentialMissing) {
		return "尚未登录。"
	}
	if errors.Is(err, domain.ErrInvalidArgument) {
		return "输入内容不合法，请检查后重试。"
	}
	var syncErr *SyncError
	if errors.As(err, &syncErr) {
		switch syncErr.Kind {
		case SyncErrAPI:
			switch syncErr.Code {
			case "otp_invalid":
				return "验证码不正确或已过期，请重新发送。"
			case "invalid_request":
				return "输入内容不合法，请检查后重试。"
			case "device_revoked":
				return "本机已被撤销，请重新登录。"
			case "unauthenticated":
				return "凭据无效，请重新登录。"
			case "rate_limited":
				return "请求太频繁，请稍后再试。"
			case "attachment_not_ready":
				return "附件尚未传完，请稍后重试。"
			case "payload_too_large":
				return "单篇内容超出云同步上限。"
			case "unavailable":
				return "同步服务暂时不可用，请稍后再试。"
			default:
				return "同步失败，请稍后再试。"
			}
		case SyncErrMalformed:
			return "服务器响应异常，请稍后再试。"
		case SyncErrNetwork:
			if errors.Is(syncErr.Cause, domain.ErrProviderTimeout) {
				return "连接超时，请稍后再试。"
			}
			return "网络不可用，请检查连接后重试。"
		}
	}
	if errors.Is(err, domain.ErrProviderTimeout) {
		return "连接超时，请稍后再试。"
	}
	if errors.Is(err, domain.ErrProviderUnavailable) {
		return "网络不可用，请检查连接后重试。"
	}
	if errors.Is(err, context.Canceled) {
		return "操作已取消。"
	}
	return "同步失败，请稍后再试。"
}
