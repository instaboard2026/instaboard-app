// Package migrations embeds the SQLite schema files (NNN_name.sql), applied in name order on startup.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
