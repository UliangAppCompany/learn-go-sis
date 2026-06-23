// Package migrations embeds the ordered SQL migration files so the server binary
// can apply them itself (goose reads them via SetBaseFS). They are also sqlc's
// schema source — one set of files feeds the app, the tests, and codegen.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
