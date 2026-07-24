// Package migrations embeds the SQL files applied by the migration runner.
package migrations

import "embed"

// FS contains every versioned .sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
