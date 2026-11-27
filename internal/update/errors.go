package update

import (
	"errors"
	"fmt"
	"io"

	"boku/internal/domain"
)

// unavailableFrom 在失败来自注入的 updater 时保留调用方需要的区分：坏响应仍是
// 坏响应，其余都是取不到的更新。不引用 URL。
func unavailableFrom(reason string, err error) error {
	if errors.Is(err, domain.ErrProviderBadResponse) {
		return fmt.Errorf("%w: %s: %v", domain.ErrProviderBadResponse, reason, err)
	}
	return fmt.Errorf("%w: %s: %v", domain.ErrUpdateUnavailable, semantic(reason), err)
}

var _ = io.Discard
