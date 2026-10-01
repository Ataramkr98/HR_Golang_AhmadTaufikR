// Package migrations embeds the SQL migration files so the compiled binary can
// migrate without the .sql files being present on disk.
//
// The previous implementation opened `file://migrations`, a path relative to the
// process working directory. That works with `go run ./cmd/migrate` from the
// repository root and fails everywhere else — in a container, under systemd, or
// whenever the binary is invoked from another directory — with a misleading
// "no such file or directory" that reads like a database problem.
//
// Embedding removes the working-directory dependency entirely: a deployment that
// ships only the binary can still migrate.
package migrations

import (
	"embed"

	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Files holds every migration in this directory. The glob is intentionally
// narrow: only .sql files belong here, so a stray editor backup cannot be
// picked up and executed as a migration.
//
//go:embed *.sql
var Files embed.FS

// Source returns a golang-migrate source driver reading the embedded files.
//
// The "." root is required: the embedded FS is rooted at this directory, while
// the driver expects the migration files to be at the root of the FS it is
// given.
func Source() (source.Driver, error) {
	return iofs.New(Files, ".")
}
