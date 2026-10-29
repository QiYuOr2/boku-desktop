// Package sqlite opens MyGo's SQLite library.
//
// The library is loaded with purego, so this package builds with cgo
// disabled. A feature keeps its own schema and statements; this package
// only keeps the driver out of that feature.
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

// Open opens a database file. The parent directory must already exist.
// Close the connection when finished.
func Open(ctx context.Context, path string) (*DB, error) {
	return msqlite.Open(ctx, path, msqlite.OpenOptions{})
}
