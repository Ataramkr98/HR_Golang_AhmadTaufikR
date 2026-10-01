package integration_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/simpul/hr-backend/migrations"
)

func TestPostgresMigrationsUpDownAndAuditGuard(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.Contains(strings.ToLower(databaseName), "test") {
		t.Fatalf("refusing destructive migration test against database %q; name must contain 'test'", databaseName)
	}
	// Migrate from the same embedded source the binary uses, so this test exercises the
	// exact migration set that ships — and no longer depends on the package's working
	// directory to locate the .sql files.
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	connection, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(context.Background())
	var rlsEnabled bool
	if err := connection.QueryRow(context.Background(), `SELECT relrowsecurity FROM pg_class WHERE relname='employees'`).Scan(&rlsEnabled); err != nil {
		t.Fatal(err)
	}
	if !rlsEnabled {
		t.Fatal("employees RLS is not enabled")
	}
	if _, err := connection.Exec(context.Background(), `
		INSERT INTO organizations(id,slug,name) VALUES ('00000000-0000-0000-0000-000000000001','audit-test','Audit Test');
		INSERT INTO audit_logs(id,organization_id,action,entity_type,entity_id) VALUES ('00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001','create','organization','00000000-0000-0000-0000-000000000001');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(context.Background(), `UPDATE audit_logs SET action='tampered' WHERE id='00000000-0000-0000-0000-000000000002'`); err == nil {
		t.Fatal("append-only audit trigger allowed an update")
	}
	if err := m.Down(); err != nil {
		t.Fatal(err)
	}
}
