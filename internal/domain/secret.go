package domain

import (
	"context"
	"time"
)

// SecretMeta 描述一个已存储的密钥而不透露其内容。存储把它与值放在一起，
// 好让窗口能列出存在哪些 key、属于谁，而不读取任何值。
type SecretMeta struct {
	ProviderID string
	// Account 是非敏感标签，例如密钥属于哪个账户；它会显示，因此绝不含凭据。
	Account   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Secret 是一个凭据，仅在使用期间存在于内存。
//
// Value 是明文，只留在这里：Secret 故意没有 String 方法，也没有任何东西格式化
// 它——用 %v 打印结构体会显示其值，而值恰恰绝不能进入日志。
type Secret struct {
	Name  string
	Value []byte
	Meta  SecretMeta
}

// Text 以请求所需的文本形式返回值。
func (s Secret) Text() string { return string(s.Value) }

// SecretStore 保存凭据。数据库只存这些名称，绝不存值。只有这个接口横在应用
// 与值的保护方式之间，所以其背后的文件可以更换而不触及上层。
//
// 锁定状态属于接口而非本构建：需要口令才能读取的存储会返回 ErrVaultLocked
// ——因此 Put、Get 等方法都写成可据此分支——但本应用装配的存储把值放在明
// 文文件里，Unlocked 始终返回 true。安全设计 §5 说明原因以及用什么保护值。
type SecretStore interface {
	// Unlock 使值可读。无需解锁的存储接受任意口令——对明文文件而言，这
	// 正是诚实的答案。
	Unlock(ctx context.Context, passphrase string) error
	// Lock 尽力遗忘，使值在下次 Unlock 前不再可读。
	Lock()
	// Unlocked 报告当前值是否可读。
	Unlocked() bool
	// Put 在某个名称下存值，替换旧值并保留首个版本的创建时间。
	Put(ctx context.Context, name string, value []byte, meta SecretMeta) error
	// Get 返回一个密钥，或 ErrSecretNotFound。
	Get(ctx context.Context, name string) (Secret, error)
	// Delete 删除一个名称，或返回 ErrSecretNotFound。
	Delete(ctx context.Context, name string) error
	// List 返回所有名称（已排序），不含值。
	List(ctx context.Context) ([]string, error)
	// Meta 返回某个名称的元数据，或 ErrSecretNotFound。
	Meta(ctx context.Context, name string) (SecretMeta, error)
}
