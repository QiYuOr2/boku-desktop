// 本文件提供每个存储访问文件前都必须做的一件事：用 context 的错误回应已取消的 context。
// 集中一处而非复制进每个存储，是因为这是条规则——
// “已取消的 context 用 context.Canceled 回应，而不是写出半份数据”——
// 多个存储遵循它，就该遵循同一份实现。
package kit

import "context"

// ContextErr 返回已结束 context 的错误，否则返回 nil。
// nil context 视为未结束：存储是通过总会传入 context 的接口调用的，
// 测试不传时不应被拒绝。
func ContextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
