// Package migrations exposes the SQL migration files embedded into the
// universe binary so `universe db migrate` works from any working directory.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
