package diary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func syncFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sync", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestValidSyncFixturesDecode(t *testing.T) {
	if _, err := DecodeAuthOTPRequest(syncFixture(t, "auth_otp_request.json")); err != nil {
		t.Fatalf("auth_otp_request: %v", err)
	}
	if _, err := DecodeAuthVerifyRequest(syncFixture(t, "auth_verify_request.json")); err != nil {
		t.Fatalf("auth_verify_request: %v", err)
	}
	if _, err := DecodeEnrollRequest(syncFixture(t, "enroll_request.json")); err != nil {
		t.Fatalf("enroll_request: %v", err)
	}
	if resp, err := DecodeEnrollResponse(syncFixture(t, "enroll_response.json")); err != nil || !strings.HasPrefix(resp.DeviceCredential, "bkd_") {
		t.Fatalf("enroll_response: resp=%+v err=%v", resp, err)
	}
	if resp, err := DecodeDevicesResponse(syncFixture(t, "devices_response.json")); err != nil || len(resp.Devices) == 0 {
		t.Fatalf("devices_response: resp=%+v err=%v", resp, err)
	}
	if req, err := DecodePushRequest(syncFixture(t, "push_request.json")); err != nil || len(req.Mutations) != 3 {
		t.Fatalf("push_request: len=%d err=%v", len(req.Mutations), err)
	}
	if resp, err := DecodePushResponse(syncFixture(t, "push_response_mixed.json")); err != nil || len(resp.Results) != 3 {
		t.Fatalf("push_response_mixed: len=%d err=%v", len(resp.Results), err)
	}
	if resp, err := DecodePushResponse(syncFixture(t, "push_response_conflict_tombstone.json")); err != nil || len(resp.Results) != 1 || resp.Results[0].Current == nil || resp.Results[0].Current.Operation != OperationDelete {
		t.Fatalf("push_response_conflict_tombstone: resp=%+v err=%v", resp, err)
	}
	if resp, err := DecodePullResponse(syncFixture(t, "pull_response_page.json")); err != nil || len(resp.Changes) != 2 || resp.HasMore == nil || !*resp.HasMore {
		t.Fatalf("pull_response_page: resp=%+v err=%v", resp, err)
	}
	if resp, err := DecodePullResponse(syncFixture(t, "pull_response_empty.json")); err != nil || len(resp.Changes) != 0 || resp.HasMore == nil || *resp.HasMore {
		t.Fatalf("pull_response_empty: resp=%+v err=%v", resp, err)
	}
	if resp, err := DecodePullResponse(syncFixture(t, "pull_response_tombstone.json")); err != nil || len(resp.Changes) != 1 || resp.Changes[0].Operation != OperationDelete {
		t.Fatalf("pull_response_tombstone: resp=%+v err=%v", resp, err)
	}
	if _, err := DecodeAttachmentUploadRequest(syncFixture(t, "attachment_upload_url_request.json")); err != nil {
		t.Fatalf("attachment_upload_url_request: %v", err)
	}
	if resp, err := DecodeAttachmentUploadURL(syncFixture(t, "attachment_upload_url_required.json")); err != nil || resp.Status != UploadRequired || resp.Upload == nil {
		t.Fatalf("attachment_upload_url_required: resp=%+v err=%v", resp, err)
	}
	if resp, err := DecodeAttachmentUploadURL(syncFixture(t, "attachment_upload_url_existing.json")); err != nil || resp.Status != AlreadyUploaded || resp.Upload != nil {
		t.Fatalf("attachment_upload_url_existing: resp=%+v err=%v", resp, err)
	}
	if _, err := DecodeAttachmentMetadata(syncFixture(t, "attachment_complete_response.json")); err != nil {
		t.Fatalf("attachment_complete_response: %v", err)
	}
	if _, err := DecodeAttachmentDownloadURL(syncFixture(t, "attachment_download_url.json")); err != nil {
		t.Fatalf("attachment_download_url: %v", err)
	}
	for _, name := range []string{
		"error_device_revoked.json",
		"error_otp_invalid.json",
		"error_mutation_id_reused.json",
		"error_rate_limited.json",
		"error_unavailable.json",
	} {
		if _, err := DecodeError(syncFixture(t, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestMalformedSyncFixturesRejected(t *testing.T) {
	cases := []struct {
		file   string
		decode func([]byte) error
	}{
		{"malformed_auth_otp_bad_email.json", func(b []byte) error { _, err := DecodeAuthOTPRequest(b); return err }},
		{"malformed_auth_verify_short_code.json", func(b []byte) error { _, err := DecodeAuthVerifyRequest(b); return err }},
		{"malformed_pull_missing_next_cursor.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_pull_unknown_entity_type.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_pull_zero_version.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_pull_delete_with_payload.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_pull_upsert_without_payload.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_pull_uppercase_id.json", func(b []byte) error { _, err := DecodePullResponse(b); return err }},
		{"malformed_push_applied_without_version.json", func(b []byte) error { _, err := DecodePushResponse(b); return err }},
		{"malformed_push_conflict_without_current.json", func(b []byte) error { _, err := DecodePushResponse(b); return err }},
		{"malformed_push_unknown_status.json", func(b []byte) error { _, err := DecodePushResponse(b); return err }},
		{"malformed_push_attachment_missing_mime.json", func(b []byte) error { _, err := DecodePushRequest(b); return err }},
		{"malformed_attachment_bad_sha256.json", func(b []byte) error { _, err := DecodeAttachmentUploadRequest(b); return err }},
		{"malformed_error_missing_code.json", func(b []byte) error { _, err := DecodeError(b); return err }},
	}

	registered := make(map[string]bool, len(cases))
	for _, tc := range cases {
		registered[tc.file] = true
		t.Run(tc.file, func(t *testing.T) {
			err := tc.decode(syncFixture(t, tc.file))
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("expected ErrMalformed, got %v", err)
			}
		})
	}

	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "sync"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "malformed_") && !registered[e.Name()] {
			t.Errorf("unregistered malformed fixture: %s", e.Name())
		}
	}
}

func TestEntryPayloadRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	attID := "019655c0-0000-7000-8000-000000000001"
	entryID := "019655c0-0000-7000-8000-000000000002"
	orig := "sunset.png"
	mime := "image/png"
	sha := strings.Repeat("a", 64)
	size := int64(2048)
	imgURL := "https://example.com/cover.jpg"

	entry := Entry{
		ID:           entryID,
		Title:        "傍晚散步",
		Markdown:     "<c blue>风很轻</c>",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 20},
		CreatedAt:    now,
		UpdatedAt:    now,
		Tags:         []string{"散步", "散步", " 晚霞 "},
		Attachments: []Attachment{{
			ID:           attID,
			Filename:     attID + ".png",
			OriginalName: &orig,
			AddedAt:      now,
			SHA256:       &sha,
			Size:         &size,
			MimeType:     &mime,
		}},
		LinkCards: []LinkCard{{
			ID:           "019655c0-0000-7000-8000-000000000003",
			URL:          "https://example.com/post",
			Title:        "Example",
			Summary:      "Summary",
			Domain:       "example.com",
			ImageURL:     &imgURL,
			FetchedAt:    &now,
			ShowsPreview: true,
		}},
		Location: &LocationSnapshot{Place: "杭州 · 西湖", CapturedAt: now},
		Weather: &WeatherSnapshot{
			Place:              "杭州",
			Description:        "晴",
			TemperatureCelsius: 22.5,
			Unit:               "°C",
			SymbolName:         "sun.max.fill",
			Kind:               "current",
			ObservedAt:         now,
			FetchedAt:          now,
		},
	}

	payload, err := EntryToPayload(entry)
	if err != nil {
		t.Fatalf("EntryToPayload: %v", err)
	}
	round, err := EntryFromPayload(entryID, payload)
	if err != nil {
		t.Fatalf("EntryFromPayload: %v", err)
	}
	if round.Title != entry.Title || round.Markdown != entry.Markdown || round.DiaryDateKey != entry.DiaryDateKey {
		t.Fatalf("basic fields mismatch: %+v", round)
	}
	if len(round.Tags) != 2 || round.Tags[0] != "散步" || round.Tags[1] != "晚霞" {
		t.Fatalf("tags mismatch: %+v", round.Tags)
	}
	if len(round.Attachments) != 1 || round.Attachments[0].Filename != attID+".png" {
		t.Fatalf("attachments mismatch: %+v", round.Attachments)
	}
	if len(round.LinkCards) != 1 || round.LinkCards[0].URL != "https://example.com/post" {
		t.Fatalf("linkCards mismatch: %+v", round.LinkCards)
	}
	if round.Location == nil || round.Location.Place != "杭州 · 西湖" {
		t.Fatalf("location mismatch: %+v", round.Location)
	}
	if round.Weather == nil || round.Weather.Description != "晴" || round.Weather.TemperatureCelsius != 22.5 {
		t.Fatalf("weather mismatch: %+v", round.Weather)
	}

	crlfEntry := entry
	crlfEntry.Markdown = "第一行\r\n第二行\r第三行\n第四行"
	crlfPayload, err := EntryToPayload(crlfEntry)
	if err != nil {
		t.Fatalf("EntryToPayload crlf: %v", err)
	}
	if crlfPayload.Markdown == nil || *crlfPayload.Markdown != "第一行\n第二行\n第三行\n第四行" {
		t.Fatalf("EntryToPayload did not normalize line endings: %v", crlfPayload.Markdown)
	}
	rawCRLF := "云端第一行\r\n云端第二行\r云端第三行"
	crlfPayload.Markdown = &rawCRLF
	pulled, err := EntryFromPayload(entryID, crlfPayload)
	if err != nil {
		t.Fatalf("EntryFromPayload crlf: %v", err)
	}
	if pulled.Markdown != "云端第一行\n云端第二行\n云端第三行" {
		t.Fatalf("EntryFromPayload did not normalize line endings: %q", pulled.Markdown)
	}
}
