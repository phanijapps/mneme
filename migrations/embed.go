// Package migrations embeds the goose SQL migrations for mneme.
//
// The embed directive must live in this package because //go:embed cannot
// reference parent directories; internal/adapter/db consumes FS from here.
package migrations

import "embed"

// FS holds all goose migration files (001–018) at the archive root.
//
//go:embed *.sql
var FS embed.FS
