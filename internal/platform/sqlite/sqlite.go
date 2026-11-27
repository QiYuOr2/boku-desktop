// Package sqlite 打开 MyGo 的 SQLite 库。
//
// 该库通过 purego 加载，因此可在 cgo 关闭时构建。各功能自持 schema 与语句，
// 本包只负责把驱动隔离在外。
package sqlite

import (
	"context"

	msqlite "github.com/egoist/mygo/plugins/sqlite"
)

type (
	DB        = msqlite.DB
	Statement = msqlite.Statement
	Rows      = msqlite.Rows
	Result    = msqlite.Result
)

// Open 打开数据库文件。父目录必须已存在，用毕需关闭连接。
func Open(ctx context.Context, path string) (*DB, error) {
	return msqlite.Open(ctx, path, msqlite.OpenOptions{})
}
