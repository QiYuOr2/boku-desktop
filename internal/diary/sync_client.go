package diary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	syncResponseLimit = 8 * 1024 * 1024
	syncAPITimeout    = 20 * time.Second
	syncBlobTimeout   = 120 * time.Second
)

// SyncErrorKind 区分同步网络错误来源。
type SyncErrorKind int

const (
	SyncErrAPI SyncErrorKind = iota + 1
	SyncErrMalformed
	SyncErrNetwork
)

// SyncError 承载同步客户端的结构化错误，按 Code 与 Retryable 分支，不向界面透传服务端原文。
type SyncError struct {
	Kind              SyncErrorKind
	Code              string
	Status            int
	Retryable         bool
	RetryAfterSeconds *int
	Cause             error
}

func (e *SyncError) Error() string {
	switch e.Kind {
	case SyncErrAPI:
		return fmt.Sprintf("sync api error: %s (HTTP %d)", e.Code, e.Status)
	case SyncErrMalformed:
		return "sync: malformed response"
	default:
		if e.Cause != nil {
			return fmt.Sprintf("sync network error: %v", e.Cause)
		}
		return "sync network error"
	}
}

func (e *SyncError) Unwrap() error { return e.Cause }

func (e *SyncError) IsDeviceRevoked() bool {
	return e.Kind == SyncErrAPI && e.Code == "device_revoked"
}

// RawRequester 是本包向 platform/httpc 请求原始 HTTP 响应的窄接口。
type RawRequester interface {
	Raw(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, opts httpc.RawOptions) (httpc.RawResponse, error)
}

// SyncClient 封装 Boku 同步协议 v1 的报文收发（含服务端代理邮箱 OTP 登录）。
type SyncClient struct {
	config SyncConfiguration
	http   RawRequester
}

func NewSyncClient(config SyncConfiguration, http RawRequester) *SyncClient {
	return &SyncClient{config: config, http: http}
}

func (c *SyncClient) SendOTP(ctx context.Context, email string) error {
	body, err := json.Marshal(AuthOTPRequest{Email: strings.TrimSpace(email)})
	if err != nil {
		return err
	}
	_, err = c.callAPI(ctx, "POST", c.config.APIBaseURL+"/v1/auth/otp", "", body)
	return err
}

// VerifyOTP 向同步服务端提交邮箱验证码并直接换取设备凭据；外部身份令牌不经过客户端。
func (c *SyncClient) VerifyOTP(ctx context.Context, email, code, deviceName, platform string) (EnrollResponse, error) {
	body, err := json.Marshal(AuthVerifyRequest{
		Email:      strings.TrimSpace(email),
		Code:       strings.TrimSpace(code),
		DeviceName: deviceName,
		Platform:   platform,
	})
	if err != nil {
		return EnrollResponse{}, err
	}
	data, err := c.callAPI(ctx, "POST", c.config.APIBaseURL+"/v1/auth/verify", "", body)
	if err != nil {
		return EnrollResponse{}, err
	}
	resp, err := DecodeEnrollResponse(data)
	if err != nil {
		return EnrollResponse{}, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp, nil
}

func (c *SyncClient) ListDevices(ctx context.Context, credential string) ([]Device, error) {
	data, err := c.callAPI(ctx, "GET", c.config.APIBaseURL+"/v1/devices", credential, nil)
	if err != nil {
		return nil, err
	}
	resp, err := DecodeDevicesResponse(data)
	if err != nil {
		return nil, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp.Devices, nil
}

func (c *SyncClient) RevokeDevice(ctx context.Context, credential, deviceID string) error {
	_, err := c.callAPI(ctx, "DELETE", c.config.APIBaseURL+"/v1/devices/"+url.PathEscape(strings.ToLower(deviceID)), credential, nil)
	return err
}

func (c *SyncClient) Push(ctx context.Context, credential string, mutations []Mutation) ([]PushResult, error) {
	body, err := json.Marshal(PushRequest{Mutations: mutations})
	if err != nil {
		return nil, err
	}
	data, err := c.callAPI(ctx, "POST", c.config.APIBaseURL+"/v1/sync/push", credential, body)
	if err != nil {
		return nil, err
	}
	resp, err := DecodePushResponse(data)
	if err != nil {
		return nil, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp.Results, nil
}

func (c *SyncClient) Pull(ctx context.Context, credential, cursor string, limit int) (PullResponse, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	endpoint := c.config.APIBaseURL + "/v1/sync/pull?" + q.Encode()
	data, err := c.callAPI(ctx, "GET", endpoint, credential, nil)
	if err != nil {
		return PullResponse{}, err
	}
	resp, err := DecodePullResponse(data)
	if err != nil {
		return PullResponse{}, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp, nil
}

func (c *SyncClient) AttachmentUploadURL(ctx context.Context, credential string, req AttachmentUploadRequest) (AttachmentUploadURL, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return AttachmentUploadURL{}, err
	}
	data, err := c.callAPI(ctx, "POST", c.config.APIBaseURL+"/v1/attachments/upload-url", credential, body)
	if err != nil {
		return AttachmentUploadURL{}, err
	}
	resp, err := DecodeAttachmentUploadURL(data)
	if err != nil {
		return AttachmentUploadURL{}, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp, nil
}

func (c *SyncClient) AttachmentComplete(ctx context.Context, credential, attachmentID string) (AttachmentMetadata, error) {
	body, err := json.Marshal(AttachmentCompleteRequest{AttachmentID: strings.ToLower(attachmentID)})
	if err != nil {
		return AttachmentMetadata{}, err
	}
	data, err := c.callAPI(ctx, "POST", c.config.APIBaseURL+"/v1/attachments/complete", credential, body)
	if err != nil {
		return AttachmentMetadata{}, err
	}
	resp, err := DecodeAttachmentMetadata(data)
	if err != nil {
		return AttachmentMetadata{}, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp, nil
}

func (c *SyncClient) AttachmentDownloadURL(ctx context.Context, credential, attachmentID string) (AttachmentDownloadURL, error) {
	endpoint := c.config.APIBaseURL + "/v1/attachments/" + url.PathEscape(strings.ToLower(attachmentID)) + "/download-url"
	data, err := c.callAPI(ctx, "GET", endpoint, credential, nil)
	if err != nil {
		return AttachmentDownloadURL{}, err
	}
	resp, err := DecodeAttachmentDownloadURL(data)
	if err != nil {
		return AttachmentDownloadURL{}, &SyncError{Kind: SyncErrMalformed, Cause: err}
	}
	return resp, nil
}

func (c *SyncClient) UploadBlob(ctx context.Context, upload SignedUpload, data []byte) error {
	resp, err := c.raw(ctx, upload.Method, upload.URL, upload.Headers, data, httpc.RawOptions{
		Timeout:      syncBlobTimeout,
		MaxBytes:     64 * 1024,
		ExactHeaders: true,
	})
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return &SyncError{
			Kind:      SyncErrNetwork,
			Status:    resp.Status,
			Retryable: resp.Status == 429 || resp.Status >= 500,
		}
	}
	return nil
}

func (c *SyncClient) DownloadBlob(ctx context.Context, signedURL string) ([]byte, error) {
	resp, err := c.raw(ctx, "GET", signedURL, nil, nil, httpc.RawOptions{
		Timeout:      syncBlobTimeout,
		MaxBytes:     MaxAttachmentBytes + 1,
		ExactHeaders: true,
	})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return nil, &SyncError{
			Kind:      SyncErrNetwork,
			Status:    resp.Status,
			Retryable: resp.Status == 429 || resp.Status >= 500,
		}
	}
	return resp.Body, nil
}

func (c *SyncClient) callAPI(ctx context.Context, method, endpoint, bearer string, body []byte) ([]byte, error) {
	headers := map[string]string{
		"Accept": "application/json",
	}
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
	}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	resp, err := c.raw(ctx, method, endpoint, headers, body, httpc.RawOptions{
		Timeout:  syncAPITimeout,
		MaxBytes: syncResponseLimit,
	})
	if err != nil {
		slog.Warn("diary sync api request failed", "method", method, "error", err)
		return nil, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		errBody, decErr := DecodeError(resp.Body)
		if decErr != nil {
			slog.Warn("diary sync api malformed error", "method", method, "status", resp.Status)
			return nil, &SyncError{Kind: SyncErrMalformed, Status: resp.Status, Cause: decErr}
		}
		slog.Warn("diary sync api error", "method", method, "status", resp.Status, "code", errBody.Error.Code)
		return nil, &SyncError{
			Kind:              SyncErrAPI,
			Code:              errBody.Error.Code,
			Status:            resp.Status,
			Retryable:         *errBody.Error.Retryable,
			RetryAfterSeconds: errBody.Error.RetryAfterSeconds,
		}
	}
	return resp.Body, nil
}

func (c *SyncClient) raw(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, opts httpc.RawOptions) (httpc.RawResponse, error) {
	resp, err := c.http.Raw(ctx, method, endpoint, headers, body, opts)
	if err != nil {
		retryable := errors.Is(err, domain.ErrProviderTimeout) ||
			errors.Is(err, domain.ErrProviderUnavailable) ||
			errors.Is(err, domain.ErrProviderRateLimited)
		return httpc.RawResponse{}, &SyncError{
			Kind:      SyncErrNetwork,
			Retryable: retryable,
			Cause:     err,
		}
	}
	return resp, nil
}
