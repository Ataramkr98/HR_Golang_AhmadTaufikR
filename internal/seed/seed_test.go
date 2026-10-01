package seed_test

import (
	"os"
	"strings"
	"testing"

	"github.com/simpul/hr-backend/internal/seed"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		password    string
		wantErr     string
	}{
		{
			name:        "development accepts the example password",
			environment: "development",
			password:    seed.PlaceholderPassword,
		},
		{
			name:        "test accepts the example password",
			environment: "test",
			password:    seed.PlaceholderPassword,
		},
		{
			name:        "a real password is accepted anywhere",
			environment: "production",
			password:    "correct-horse-battery-staple",
		},

		// The point of the guard: the placeholder is published in .env.example, so a
		// production deployment that kept it would hand out an administrator account to
		// anyone who reads the repository.
		{
			name:        "production rejects the published placeholder",
			environment: "production",
			password:    seed.PlaceholderPassword,
			wantErr:     "example value from .env.example",
		},
		{
			name:        "staging rejects the published placeholder",
			environment: "staging",
			password:    seed.PlaceholderPassword,
			wantErr:     "example value from .env.example",
		},
		{
			name:        "empty is rejected",
			environment: "development",
			password:    "",
			wantErr:     "required",
		},
		{
			name:        "whitespace is not a password",
			environment: "development",
			password:    "     ",
			wantErr:     "required",
		},
		{
			name:        "too short is rejected",
			environment: "production",
			password:    "short",
			wantErr:     "at least 12 characters",
		},
		{
			name:        "exactly the minimum is accepted",
			environment: "production",
			password:    strings.Repeat("a", 12),
		},
		{
			// Length is counted in runes, so a passphrase of accented characters is not
			// rejected for being "too short" in bytes.
			name:        "length is counted in characters, not bytes",
			environment: "production",
			password:    "café-crème-1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := seed.Validate(test.environment, test.password)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%q, <password>) returned an unexpected error: %v", test.environment, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%q, <password>) returned no error, want one containing %q", test.environment, test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), test.wantErr)
			}
		})
	}
}

// TestPlaceholderPasswordMatchesEnvExample keeps the constant and the shipped example file
// from drifting apart. If someone edits one without the other, the guard silently stops
// protecting anything — the failure would be invisible, which is the worst kind.
func TestPlaceholderPasswordMatchesEnvExample(t *testing.T) {
	content, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("reading .env.example: %v", err)
	}
	if !strings.Contains(string(content), "SEED_ADMIN_PASSWORD="+seed.PlaceholderPassword) {
		t.Errorf("seed.PlaceholderPassword (%q) is not the value in .env.example; update whichever is stale", seed.PlaceholderPassword)
	}
}
