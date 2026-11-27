package diary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

type memSecretStore struct {
	mu   sync.Mutex
	data map[string]domain.Secret
}

func newMemSecretStore() *memSecretStore {
	return &memSecretStore{data: map[string]domain.Secret{}}
}

func (m *memSecretStore) Unlock(context.Context, string) error { return nil }
func (m *memSecretStore) Lock()                                {}
func (m *memSecretStore) Unlocked() bool                       { return true }
func (m *memSecretStore) Put(_ context.Context, name string, value []byte, meta domain.SecretMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[name] = domain.Secret{Name: name, Value: append([]byte(nil), value...), Meta: meta}
	return nil
}
func (m *memSecretStore) Get(_ context.Context, name string) (domain.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[name]
	if !ok {
		return domain.Secret{}, domain.ErrSecretNotFound
	}
	return s, nil
}
func (m *memSecretStore) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[name]; !ok {
		return domain.ErrSecretNotFound
	}
	delete(m.data, name)
	return nil
}
func (m *memSecretStore) List(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}
func (m *memSecretStore) Meta(_ context.Context, name string) (domain.SecretMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[name]
	if !ok {
		return domain.SecretMeta{}, domain.ErrSecretNotFound
	}
	return s.Meta, nil
}

type fakeSyncServer struct {
	mu              sync.Mutex
	srv             *httptest.Server
	userID          string
	devices         map[string]*Device
	creds           map[string]string // cred -> deviceID
	entities        map[string]*Change
	changelog       []Change
	appliedMuts     map[string]PushResult
	blobs           map[string][]byte
	blobMeta        map[string]AttachmentMetadata
	corruptNextBlob bool
	failNextPushes  int
}

func newFakeSyncServer(t *testing.T) *fakeSyncServer {
	t.Helper()
	f := &fakeSyncServer{
		userID:      "019655c0-0000-7000-8000-000000000900",
		devices:     map[string]*Device{},
		creds:       map[string]string{},
		entities:    map[string]*Change{},
		appliedMuts: map[string]PushResult{},
		blobs:       map[string][]byte{},
		blobMeta:    map[string]AttachmentMetadata{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSyncServer) config() SyncConfiguration {
	return SyncConfiguration{
		APIBaseURL: f.srv.URL,
	}
}

func (f *fakeSyncServer) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeSyncServer) writeError(w http.ResponseWriter, status int, code string, retryable bool) {
	f.writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":      code,
			"message":   "secret internal server detail that must never appear in UI",
			"retryable": retryable,
		},
	})
}

func (f *fakeSyncServer) authDevice(r *http.Request) (*Device, string) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	devID, ok := f.creds[token]
	if !ok {
		return nil, "unauthenticated"
	}
	dev := f.devices[devID]
	if dev == nil || dev.RevokedAt != nil {
		return nil, "device_revoked"
	}
	return dev, ""
}

func (f *fakeSyncServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/auth/otp":
		body, _ := io.ReadAll(r.Body)
		if _, err := DecodeAuthOTPRequest(body); err != nil {
			f.writeError(w, http.StatusBadRequest, "invalid_request", false)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == "POST" && r.URL.Path == "/v1/auth/verify":
		body, _ := io.ReadAll(r.Body)
		req, err := DecodeAuthVerifyRequest(body)
		if err != nil {
			f.writeError(w, http.StatusBadRequest, "invalid_request", false)
			return
		}
		if req.Code != "123456" {
			f.writeError(w, http.StatusUnauthorized, "otp_invalid", false)
			return
		}
		devID, _ := domain.NewID()
		cred := "bkd_synth_" + devID
		now := time.Now().UTC()
		f.devices[devID] = &Device{DeviceID: devID, DeviceName: req.DeviceName, Platform: req.Platform, CreatedAt: now, LastSeenAt: &now}
		f.creds[cred] = devID
		f.writeJSON(w, http.StatusCreated, EnrollResponse{UserID: f.userID, DeviceID: devID, DeviceCredential: cred})
		return
	case strings.HasPrefix(r.URL.Path, "/r2/"):
		attID := strings.TrimPrefix(r.URL.Path, "/r2/")
		if r.Method == "PUT" {
			data, _ := io.ReadAll(r.Body)
			f.blobs[attID] = data
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == "GET" {
			data, ok := f.blobs[attID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if f.corruptNextBlob {
				f.corruptNextBlob = false
				_, _ = w.Write([]byte("corrupted-bytes"))
				return
			}
			_, _ = w.Write(data)
			return
		}
	}

	_, errCode := f.authDevice(r)
	if errCode != "" {
		f.writeError(w, http.StatusUnauthorized, errCode, false)
		return
	}

	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/devices":
		list := make([]Device, 0, len(f.devices))
		for _, d := range f.devices {
			list = append(list, *d)
		}
		f.writeJSON(w, http.StatusOK, DevicesResponse{Devices: list})
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1/devices/"):
		devID := strings.TrimPrefix(r.URL.Path, "/v1/devices/")
		if d, ok := f.devices[devID]; ok {
			now := time.Now().UTC()
			d.RevokedAt = &now
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && r.URL.Path == "/v1/attachments/upload-url":
		body, _ := io.ReadAll(r.Body)
		req, err := DecodeAttachmentUploadRequest(body)
		if err != nil {
			f.writeError(w, http.StatusBadRequest, "invalid_request", false)
			return
		}
		if _, ok := f.blobMeta[req.AttachmentID]; ok {
			f.writeJSON(w, http.StatusOK, AttachmentUploadURL{AttachmentID: req.AttachmentID, Status: AlreadyUploaded})
			return
		}
		f.blobMeta[req.AttachmentID] = AttachmentMetadata{
			AttachmentID: req.AttachmentID,
			SHA256:       req.SHA256,
			Size:         req.Size,
			MimeType:     req.MimeType,
			CreatedAt:    time.Now().UTC(),
		}
		f.writeJSON(w, http.StatusOK, AttachmentUploadURL{
			AttachmentID: req.AttachmentID,
			Status:       UploadRequired,
			Upload: &SignedUpload{
				URL:       f.srv.URL + "/r2/" + req.AttachmentID,
				Method:    "PUT",
				Headers:   map[string]string{"Content-Type": req.MimeType},
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			},
		})
	case r.Method == "POST" && r.URL.Path == "/v1/attachments/complete":
		var req AttachmentCompleteRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		meta, ok := f.blobMeta[req.AttachmentID]
		if !ok {
			f.writeError(w, http.StatusNotFound, "not_found", false)
			return
		}
		f.writeJSON(w, http.StatusOK, meta)
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/download-url"):
		attID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/attachments/"), "/download-url")
		meta, ok := f.blobMeta[attID]
		if !ok {
			f.writeError(w, http.StatusNotFound, "not_found", false)
			return
		}
		f.writeJSON(w, http.StatusOK, AttachmentDownloadURL{
			AttachmentID: attID,
			URL:          f.srv.URL + "/r2/" + attID,
			ExpiresAt:    time.Now().UTC().Add(10 * time.Minute),
			SHA256:       meta.SHA256,
			Size:         meta.Size,
		})
	case r.Method == "POST" && r.URL.Path == "/v1/sync/push":
		if f.failNextPushes > 0 {
			f.failNextPushes--
			f.writeError(w, http.StatusServiceUnavailable, "unavailable", true)
			return
		}
		body, _ := io.ReadAll(r.Body)
		req, err := DecodePushRequest(body)
		if err != nil {
			f.writeError(w, http.StatusBadRequest, "invalid_request", false)
			return
		}
		results := make([]PushResult, 0, len(req.Mutations))
		for _, m := range req.Mutations {
			if prev, ok := f.appliedMuts[m.MutationID]; ok {
				dup := prev
				dup.Status = StatusDuplicate
				results = append(results, dup)
				continue
			}
			cur := f.entities[m.EntityID]
			curVer := int64(0)
			if cur != nil {
				curVer = cur.Version
			}
			if *m.BaseVersion != curVer {
				copyCur := *cur
				results = append(results, PushResult{MutationID: m.MutationID, Status: StatusConflict, Current: &copyCur})
				continue
			}
			nextVer := curVer + 1
			cursor := strconv.Itoa(len(f.changelog) + 1)
			change := Change{
				Cursor:     cursor,
				EntityType: EntityDiaryEntry,
				EntityID:   m.EntityID,
				Version:    nextVer,
				Operation:  m.Operation,
				Payload:    m.Payload,
			}
			f.entities[m.EntityID] = &change
			f.changelog = append(f.changelog, change)
			res := PushResult{MutationID: m.MutationID, Status: StatusApplied, Version: &nextVer, Cursor: &cursor}
			f.appliedMuts[m.MutationID] = res
			results = append(results, res)
		}
		f.writeJSON(w, http.StatusOK, PushResponse{Results: results})
	case r.Method == "GET" && r.URL.Path == "/v1/sync/pull":
		after := 0
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			after, _ = strconv.Atoi(raw)
		}
		var out []Change
		for _, ch := range f.changelog {
			n, _ := strconv.Atoi(ch.Cursor)
			if n > after {
				out = append(out, ch)
			}
		}
		if out == nil {
			out = []Change{}
		}
		next := strconv.Itoa(len(f.changelog))
		hasMore := false
		f.writeJSON(w, http.StatusOK, PullResponse{Changes: out, NextCursor: &next, HasMore: &hasMore})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func TestSyncEngineEndToEndTwoDevicesConflictsAndRevocation(t *testing.T) {
	srv := newFakeSyncServer(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{at: now}
	cfgFn := func() (SyncConfiguration, bool) { return srv.config(), true }
	noSleep := func(context.Context, time.Duration) error { return nil }
	fixedJitter := func() float64 { return 1.0 }

	storeA, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	secretsA := newMemSecretStore()
	engineA := NewSyncEngine(storeA, cfgFn, httpc.New(httpc.Options{HTTPClient: srv.srv.Client(), Timeout: 5 * time.Second}), secretsA, clock, WithSleep(noSleep), WithJitter(fixedJitter))

	// 未登录时同步必须是安静空操作。
	if sum, err := engineA.SyncNow(ctx); err != nil || sum != (SyncSummary{}) {
		t.Fatalf("unauthenticated SyncNow should no-op: %+v err=%v", sum, err)
	}

	// 设备 A 登录。
	if err := engineA.SendLoginCode(ctx, "user@example.com"); err != nil {
		t.Fatalf("SendLoginCode: %v", err)
	}
	if err := engineA.Login(ctx, "user@example.com", "123456", "Mac Studio"); err != nil {
		t.Fatalf("Login A: %v", err)
	}

	// 设备 A 写一篇含附件的日记并推送（附带一次瞬时 503 测试重试）。
	entryID, _ := domain.NewID()
	attID, _ := domain.NewID()
	attBytes := []byte("real-attachment-payload-bytes")
	up := AttachmentUpload{ID: attID, Filename: attID + ".png", Data: attBytes, AddedAt: now}
	_, err = storeA.Save(Entry{
		ID:           entryID,
		Title:        "第一篇云同步日记",
		Markdown:     "来自设备 A",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 20},
		Attachments:  []Attachment{up.Attachment()},
	}, []AttachmentUpload{up}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	srv.failNextPushes = 1
	sumA, err := engineA.SyncNow(ctx)
	if err != nil || sumA.Pushed != 1 {
		t.Fatalf("SyncNow A: %+v err=%v", sumA, err)
	}

	// 设备 B 登录并拉取（第一次故意损坏附件字节以验证哈希校验 + 下次自动修复）。
	storeB, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	secretsB := newMemSecretStore()
	engineB := NewSyncEngine(storeB, cfgFn, httpc.New(httpc.Options{HTTPClient: srv.srv.Client(), Timeout: 5 * time.Second}), secretsB, clock, WithSleep(noSleep), WithJitter(fixedJitter))
	if err := engineB.Login(ctx, "user@example.com", "123456", "Windows PC"); err != nil {
		t.Fatalf("Login B: %v", err)
	}

	srv.corruptNextBlob = true
	sumB1, err := engineB.SyncNow(ctx)
	if err != nil || sumB1.Pulled != 1 {
		t.Fatalf("SyncNow B1: %+v err=%v", sumB1, err)
	}
	// 损坏的字节不能落盘，所以应进入修复环节自动重新下载成功！
	if !storeB.AttachmentExists(attID + ".png") {
		t.Fatal("attachment should be repaired after corrupt download was discarded")
	}
	gotBytes, _ := storeB.ReadAttachment(attID + ".png")
	if string(gotBytes) != string(attBytes) {
		t.Fatalf("repaired attachment bytes mismatch")
	}

	// 制造冲突：A 先推 v2，B 在 v1 上修改并推送触发 conflict。
	entryA, _ := storeA.Entry(entryID, false)
	entryA.Title = "A 的 v2 版本"
	_, _ = storeA.Save(*entryA, nil, nil, now.Add(time.Minute))
	if _, err := engineA.SyncNow(ctx); err != nil {
		t.Fatalf("SyncNow A v2: %v", err)
	}

	entryB, _ := storeB.Entry(entryID, false)
	entryB.Title = "B 的并发修改"
	_, _ = storeB.Save(*entryB, nil, nil, now.Add(2*time.Minute))
	sumB2, err := engineB.SyncNow(ctx)
	if err != nil || sumB2.Conflicts != 1 {
		t.Fatalf("expected 1 conflict on B, got %+v err=%v", sumB2, err)
	}
	conflicts, err := engineB.Conflicts(ctx)
	if err != nil || len(conflicts) != 1 || conflicts[0].EntityID != entryID || conflicts[0].ServerChange == nil {
		t.Fatalf("Conflicts B: %+v err=%v", conflicts, err)
	}

	// B 选择「本地另存为新日记」解决冲突。
	copyID, err := engineB.ResolveConflictKeepLocalAsNew(ctx, entryID)
	if err != nil || copyID == "" || copyID == entryID {
		t.Fatalf("ResolveConflictKeepLocalAsNew: id=%q err=%v", copyID, err)
	}
	resolvedOrig, _ := storeB.Entry(entryID, false)
	resolvedCopy, _ := storeB.Entry(copyID, false)
	if resolvedOrig == nil || resolvedOrig.Title != "A 的 v2 版本" {
		t.Fatalf("original entry should now match cloud version: %+v", resolvedOrig)
	}
	if resolvedCopy == nil || resolvedCopy.Title != "B 的并发修改" {
		t.Fatalf("copied entry should preserve local content: %+v", resolvedCopy)
	}

	// 设备 A 撤销设备 B，随后设备 B 同步时遇到 device_revoked 必须清空本地同步凭据但保留本地日记。
	statusB, _ := engineB.Status(ctx)
	if err := engineA.RevokeDevice(ctx, statusB.OwnDeviceID); err != nil {
		t.Fatalf("RevokeDevice B from A: %v", err)
	}
	_, err = engineB.SyncNow(ctx)
	var syncErr *SyncError
	if !errors.As(err, &syncErr) || !syncErr.IsDeviceRevoked() {
		t.Fatalf("expected device_revoked error, got %v", err)
	}
	if engineB.IsLoggedIn(ctx) {
		t.Fatal("revoked device must clear local sync credentials")
	}
	if stillThere, _ := storeB.Entry(entryID, false); stillThere == nil {
		t.Fatal("revoked device must keep local diary entries")
	}
	msg := SyncUserMessage(err)
	if !strings.Contains(msg, "已被撤销") || strings.Contains(msg, "secret internal") {
		t.Fatalf("unexpected SyncUserMessage: %q", msg)
	}
}

func TestBackoffDelayCurve(t *testing.T) {
	if got := BackoffDelay(1, 1.0); got != 5*time.Second {
		t.Fatalf("attempt 1: got %v, want 5s", got)
	}
	if got := BackoffDelay(2, 1.0); got != 10*time.Second {
		t.Fatalf("attempt 2: got %v, want 10s", got)
	}
	if got := BackoffDelay(3, 1.0); got != 20*time.Second {
		t.Fatalf("attempt 3: got %v, want 20s", got)
	}
	if got := BackoffDelay(20, 1.0); got != time.Hour {
		t.Fatalf("attempt 20 cap: got %v, want 1h", got)
	}
}

func TestSyncAuthErrorsAndUserMessages(t *testing.T) {
	srv := newFakeSyncServer(t)
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := NewSyncEngine(
		store,
		func() (SyncConfiguration, bool) { return srv.config(), true },
		httpc.New(httpc.Options{HTTPClient: srv.srv.Client(), Timeout: 5 * time.Second}),
		newMemSecretStore(),
		fixedClock{at: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)},
	)

	err = engine.Login(context.Background(), "user@example.invalid", "000000", "Mac")
	var syncErr *SyncError
	if !errors.As(err, &syncErr) || syncErr.Code != "otp_invalid" {
		t.Fatalf("wrong OTP should return otp_invalid, got %v", err)
	}
	if msg := SyncUserMessage(err); !strings.Contains(msg, "验证码不正确或已过期") || strings.Contains(msg, "secret internal") {
		t.Fatalf("otp_invalid message mismatch: %q", msg)
	}

	rateErr := &SyncError{Kind: SyncErrAPI, Code: "rate_limited", Status: 429, Retryable: true}
	if msg := SyncUserMessage(rateErr); !strings.Contains(msg, "频繁") {
		t.Fatalf("rate_limited message mismatch: %q", msg)
	}
	unavailErr := &SyncError{Kind: SyncErrAPI, Code: "unavailable", Status: 503, Retryable: true}
	if msg := SyncUserMessage(unavailErr); !strings.Contains(msg, "暂时不可用") {
		t.Fatalf("unavailable message mismatch: %q", msg)
	}
}
