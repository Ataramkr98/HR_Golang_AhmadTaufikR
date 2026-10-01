package migrations_test

import (
	"bytes"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/simpul/hr-backend/migrations"
)

// TestEmbeddedFilesArePresent guards the property that makes the binary
// self-contained: the SQL must be compiled in, not read from disk.
//
// The failure this prevents is subtle. If the embed directive is removed, or the
// files are moved, the package still compiles and `go build ./...` still passes —
// the migration command only fails at deploy time, in a container, with a message
// that looks like a database problem.
func TestEmbeddedFilesArePresent(t *testing.T) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations are embedded; the embed directive is not matching any files")
	}

	var up, down bool
	for _, entry := range entries {
		switch {
		case strings.HasSuffix(entry.Name(), ".up.sql"):
			up = true
		case strings.HasSuffix(entry.Name(), ".down.sql"):
			down = true
		}
	}
	if !up {
		t.Error("no *.up.sql migration is embedded")
	}
	if !down {
		// A missing down migration is not fatal for deployment, but it means the
		// integration test's Down()/Up() round-trip cannot clean up.
		t.Error("no *.down.sql migration is embedded")
	}
}

// TestSourceResolvesEmbeddedMigrations checks the driver itself, not just the FS:
// a wrong root argument in Source() would leave the FS populated but expose no
// migrations to golang-migrate.
func TestSourceResolvesEmbeddedMigrations(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatalf("opening embedded migration source: %v", err)
	}
	defer source.Close()

	first, err := source.First()
	if err != nil {
		t.Fatalf("reading the first migration version: %v", err)
	}
	if first != 1 {
		t.Errorf("first migration version = %d, want 1", first)
	}

	// The source must also be able to hand the driver the actual SQL, which is what
	// fails when the FS root and the driver root disagree.
	reader, identifier, err := source.ReadUp(first)
	if err != nil {
		t.Fatalf("reading the up migration for version %d: %v", first, err)
	}
	defer reader.Close()
	if identifier == "" {
		t.Error("the up migration has an empty identifier")
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the up migration body: %v", err)
	}
	if !bytes.Contains(bytes.ToUpper(content), []byte("CREATE TABLE")) {
		t.Error("the up migration does not look like schema SQL")
	}
}
