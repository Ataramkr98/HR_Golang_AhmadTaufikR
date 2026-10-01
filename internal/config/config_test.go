package config

import (
	"net/http"
	"strings"
	"testing"
)

func TestCookieSameSite(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		secure  bool
		want    http.SameSite
		wantErr string
	}{
		{name: "default is lax", value: "", secure: false, want: http.SameSiteLaxMode},
		{name: "explicit lax", value: "lax", secure: false, want: http.SameSiteLaxMode},
		{name: "strict", value: "strict", secure: true, want: http.SameSiteStrictMode},
		{name: "none with secure", value: "none", secure: true, want: http.SameSiteNoneMode},

		// Case and surrounding whitespace come from hand-edited .env files, so they must
		// not turn a valid value into a startup failure.
		{name: "mixed case", value: "None", secure: true, want: http.SameSiteNoneMode},
		{name: "padded", value: "  lax  ", secure: false, want: http.SameSiteLaxMode},

		// The combination browsers silently discard. Failing at startup is the point:
		// shipped, it presents as "login works, then the session vanishes".
		{name: "none without secure", value: "none", secure: false, wantErr: "COOKIE_SECURE=true"},
		{name: "typo", value: "laxx", secure: false, wantErr: "lax, strict or none"},
		{name: "invalid", value: "false", secure: false, wantErr: "lax, strict or none"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := cookieSameSite(test.value, test.secure)
			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("cookieSameSite(%q, %t) returned no error, want one containing %q", test.value, test.secure, test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("cookieSameSite(%q, %t) returned unexpected error: %v", test.value, test.secure, err)
			}
			if got != test.want {
				t.Errorf("cookieSameSite(%q, %t) = %v, want %v", test.value, test.secure, got, test.want)
			}
		})
	}
}

// TestSameSiteDefaultsAreNotNone guards the default itself. Defaulting to None would
// quietly weaken every deployment, and it is the kind of change that looks harmless in
// a diff.
func TestSameSiteDefaultsAreNotNone(t *testing.T) {
	got, err := cookieSameSite("", false)
	if err != nil {
		t.Fatalf("the empty value must be accepted as the default: %v", err)
	}
	if got == http.SameSiteNoneMode {
		t.Error("the default SameSite policy must not be None; cross-site must be opted into explicitly")
	}
}
