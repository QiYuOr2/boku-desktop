package credential

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// Secrets 存取厂商所需的凭据。
// 它持有命名规则：凭据按 "<providerId>.<key>" 存储，其余代码不自行拼名字。
type Secrets struct {
	store domain.SecretStore
	now   func() time.Time
}

func NewSecrets(store domain.SecretStore) (*Secrets, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	return &Secrets{store: store, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Path 返回凭据文件路径供界面展示；空路径表示存储端没有具名文件。
func (s *Secrets) Path() string {
	named, ok := s.store.(interface{ Path() string })
	if !ok {
		return ""
	}
	return named.Path()
}

// Names 返回某厂商已存凭据的名称，已排序。
func (s *Secrets) Names(ctx context.Context, providerID string) ([]string, error) {
	all, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(providerID) + "."
	names := make([]string, 0, len(all))
	for _, name := range all {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, nil
}

// Set 写入一个凭据；更新时保留首次创建的 createdAt，由存储端维护。
func (s *Secrets) Set(ctx context.Context, providerID, credentialKey, value string) error {
	providerID = strings.TrimSpace(providerID)
	credentialKey = strings.TrimSpace(credentialKey)
	if providerID == "" || credentialKey == "" {
		return fmt.Errorf("%w: a credential needs a provider and a name", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: credential %s is empty", domain.ErrInvalidArgument, domain.CredentialKey(providerID, credentialKey))
	}
	return s.store.Put(ctx, domain.CredentialKey(providerID, credentialKey), []byte(value), domain.SecretMeta{
		ProviderID: providerID,
		UpdatedAt:  s.now(),
	})
}

// Get 返回一个凭据的明文值，仅供发请求使用，不得展示到界面。
func (s *Secrets) Get(ctx context.Context, providerID, credentialKey string) (string, error) {
	secret, err := s.store.Get(ctx, domain.CredentialKey(providerID, credentialKey))
	if err != nil {
		return "", err
	}
	return secret.Text(), nil
}

func (s *Secrets) Delete(ctx context.Context, providerID, credentialKey string) error {
	return s.store.Delete(ctx, domain.CredentialKey(providerID, credentialKey))
}

// Masked 返回界面可展示的掩码（末四位）。未存储凭据不算错误——用户尚未配置时
// 就是这种正常状态——返回 "", false。
func (s *Secrets) Masked(ctx context.Context, providerID, credentialKey string) (string, bool, error) {
	secret, err := s.store.Get(ctx, domain.CredentialKey(providerID, credentialKey))
	if errors.Is(err, domain.ErrSecretNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(secret.Text()) == "" {
		return "", false, nil
	}
	return kit.MaskSecret(secret.Text()), true, nil
}
