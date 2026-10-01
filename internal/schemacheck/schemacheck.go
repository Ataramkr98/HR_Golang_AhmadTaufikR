// Package schemacheck answers one question before any command touches data: does the
// database this process is pointed at actually contain Simpul HR's schema?
//
// It exists because of a failure that is otherwise very hard to read. golang-migrate tracks
// a single version number in `schema_migrations`. If DATABASE_URL points at a database that
// some *other* application already migrated — or at a database where migration 000001 was
// applied and then edited in place — `migrate up` correctly reports "no change" and exits 0,
// because the version it cares about really is current. The schema underneath can still be
// someone else's entirely.
//
// The symptom then surfaces much later as a bare driver error such as:
//
//	ERROR: column "password_hash" of relation "users" does not exist (SQLSTATE 42703)
//
// which reads like a bug in the seeder or a broken migration. It is neither: the migration
// never ran here. This package turns that into one explicit sentence naming the mismatch and
// the command that fixes it.
package schemacheck

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Sentinel identifies a table/column pair the application cannot run without. The first one
// that is missing decides the message, so they are ordered most-fundamental first: `users`
// is read by every login, and `users.password_hash` is the column whose absence produced the
// original confusing error.
var sentinels = []struct{ table, column, why string }{
	{"users", "password_hash", "every login reads it"},
	{"users", "email", "every login looks the account up by email"},
	{"organizations", "slug", "the workspace is resolved by slug"},
	{"employees", "employee_code", "the directory and CSV export read it"},
	{"memberships", "organization_id", "every query is scoped by tenant"},
}

// Verify reports whether the connected database carries Simpul HR's schema.
//
// It returns nil when the schema is present, and an error that names the offending table and
// the exact remedy otherwise. A nil *gorm.DB is reported as an error rather than panicking so
// callers can treat every failure the same way.
//
// This is a guard rail, not a migration runner: it never writes, so it is safe to call before
// anything else in a command's startup path.
func Verify(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("schemacheck: no database connection was provided")
	}

	for _, sentinel := range sentinels {
		var count int64
		// to_regclass/column lookup rather than querying the table itself: selecting from a
		// table that does not exist raises an error we would have to interpret, whereas this
		// returns a plain count. Parameterised, so the identifiers cannot be injected.
		err := db.Raw(
			`SELECT count(*) FROM information_schema.columns
			   WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			sentinel.table, sentinel.column,
		).Scan(&count).Error
		if err != nil {
			return fmt.Errorf("schemacheck: could not inspect the schema: %w", err)
		}
		if count == 0 {
			return fmt.Errorf(
				"the database in DATABASE_URL does not carry Simpul HR's schema: table %q is missing the column %q (%s)\n"+
					"Either DATABASE_URL points at a database belonging to another application, or migration 000001 was applied before it was edited.\n"+
					"Migrations cannot detect this: schema_migrations already reports the latest version, so `migrate up` reports no change.\n"+
					"Point DATABASE_URL at an empty database and run `go run ./cmd/migrate up`, or drop and recreate the database first.",
				sentinel.table, sentinel.column, sentinel.why,
			)
		}
	}
	return nil
}

// Describe renders a short human summary of what the check found, used in the seed log so a
// successful run states that the schema was verified rather than leaving it implied.
func Describe(db *gorm.DB) string {
	var version uint
	var dirty bool
	if err := db.Raw(`SELECT version, dirty FROM schema_migrations LIMIT 1`).Row().Scan(&version, &dirty); err != nil {
		return "migration state unknown"
	}
	state := "clean"
	if dirty {
		state = "DIRTY — a previous migration failed halfway and must be repaired before use"
	}
	return strings.TrimSpace(fmt.Sprintf("schema verified (migration %d, %s)", version, state))
}
