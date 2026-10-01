package schemacheck_test

import (
	"os"
	"strings"
	"testing"

	"github.com/simpul/hr-backend/internal/schemacheck"
)

// TestSentinelsCoverTheSchema pins the contract the check exists to enforce. If migration
// 000001 is renamed or a sentinel table is dropped from it, this fails loudly instead of the
// guard quietly protecting less than it appears to.
func TestSentinelsCoverTheSchema(t *testing.T) {
	content, err := os.ReadFile("../../migrations/000001_initial.up.sql")
	if err != nil {
		t.Fatalf("reading the initial migration: %v", err)
	}
	migration := string(content)

	// Spot-check the pair that produced the original confusing failure. `users` and
	// `password_hash` are asserted explicitly rather than derived, so that changing the
	// sentinel list cannot quietly drop the exact regression this package was written for.
	for _, required := range []string{"CREATE TABLE users", "password_hash text NOT NULL"} {
		if !strings.Contains(migration, required) {
			t.Errorf("migration no longer contains %q; update the sentinels in schemacheck to match the real schema", required)
		}
	}
}

// TestVerifyNilDatabase ensures a programming mistake upstream surfaces as an error the
// caller can handle, rather than a nil-pointer panic inside the guard itself.
func TestVerifyNilDatabase(t *testing.T) {
	err := schemacheck.Verify(nil)
	if err == nil {
		t.Fatal("Verify(nil) returned no error, want one")
	}
	if !strings.Contains(err.Error(), "no database connection") {
		t.Errorf("error = %q, want it to explain that no connection was provided", err.Error())
	}
}
