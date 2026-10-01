package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/spf13/viper"
)

type Config struct {
	Environment        string
	HTTPAddr           string
	PublicAPIURL       string
	FrontendURL        string
	DatabaseURL        string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	AllowedOrigins     []string
	AccessTokenSecret  string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	CookieSecure       bool
	CookieSameSite     http.SameSite
	EncryptionKey      []byte
	SeedAdminPassword  string
	MFARequiredRoles   map[string]bool
	GoogleClientID     string
	GoogleClientSecret string
	EntraClientID      string
	EntraClientSecret  string
	EntraTenantID      string
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AutomaticEnv()
	_ = v.ReadInConfig()

	v.SetDefault("APP_ENV", "development")
	v.SetDefault("HTTP_ADDR", ":8080")
	v.SetDefault("PUBLIC_API_URL", "http://localhost:8080")
	v.SetDefault("FRONTEND_URL", "http://localhost:5173")
	// Deliberately NOT defaulted. Redis is optional, and an empty REDIS_ADDR has to mean
	// "this deployment does not use Redis" — defaulting it to localhost:6379 produces a
	// client that always dials something, so omitting the variable yields a service that
	// reports redis as degraded and spends every readiness probe waiting for a server that
	// was never meant to exist. Development sets it explicitly in .env, as the example does.
	v.SetDefault("REDIS_DB", 0)
	v.SetDefault("ALLOWED_ORIGINS", "http://localhost:5173")
	v.SetDefault("ACCESS_TOKEN_TTL", "15m")
	v.SetDefault("REFRESH_TOKEN_TTL", "720h")
	v.SetDefault("COOKIE_SECURE", false)
	v.SetDefault("COOKIE_SAME_SITE", "lax")
	v.SetDefault("MFA_REQUIRED_ROLES", "hr_admin")
	v.SetDefault("ENTRA_TENANT_ID", "common")

	accessTTL, err := time.ParseDuration(v.GetString("ACCESS_TOKEN_TTL"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ACCESS_TOKEN_TTL: %w", err)
	}
	refreshTTL, err := time.ParseDuration(v.GetString("REFRESH_TOKEN_TTL"))
	if err != nil {
		return Config{}, fmt.Errorf("parse REFRESH_TOKEN_TTL: %w", err)
	}

	key, err := encryptionKey(v.GetString("FIELD_ENCRYPTION_KEY"), v.GetString("APP_ENV"))
	if err != nil {
		return Config{}, err
	}

	roles := map[string]bool{}
	for _, role := range strings.Split(v.GetString("MFA_REQUIRED_ROLES"), ",") {
		role = strings.TrimSpace(role)
		if role != "" {
			roles[role] = true
		}
	}

	dbURL := v.GetString("DATABASE_URL")
	if dbURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	secret := v.GetString("ACCESS_TOKEN_SECRET")
	if len(secret) < 32 {
		return Config{}, errors.New("ACCESS_TOKEN_SECRET must contain at least 32 characters")
	}
	sameSite, err := cookieSameSite(v.GetString("COOKIE_SAME_SITE"), v.GetBool("COOKIE_SECURE"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment:        v.GetString("APP_ENV"),
		HTTPAddr:           v.GetString("HTTP_ADDR"),
		PublicAPIURL:       strings.TrimRight(v.GetString("PUBLIC_API_URL"), "/"),
		FrontendURL:        strings.TrimRight(v.GetString("FRONTEND_URL"), "/"),
		DatabaseURL:        dbURL,
		RedisAddr:          v.GetString("REDIS_ADDR"),
		RedisPassword:      v.GetString("REDIS_PASSWORD"),
		RedisDB:            v.GetInt("REDIS_DB"),
		AllowedOrigins:     splitCSV(v.GetString("ALLOWED_ORIGINS")),
		AccessTokenSecret:  secret,
		AccessTokenTTL:     accessTTL,
		RefreshTokenTTL:    refreshTTL,
		CookieSecure:       v.GetBool("COOKIE_SECURE"),
		CookieSameSite:     sameSite,
		EncryptionKey:      key,
		SeedAdminPassword:  v.GetString("SEED_ADMIN_PASSWORD"),
		MFARequiredRoles:   roles,
		GoogleClientID:     v.GetString("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: v.GetString("GOOGLE_CLIENT_SECRET"),
		EntraClientID:      v.GetString("ENTRA_CLIENT_ID"),
		EntraClientSecret:  v.GetString("ENTRA_CLIENT_SECRET"),
		EntraTenantID:      v.GetString("ENTRA_TENANT_ID"),
	}, nil
}

// cookieSameSite maps COOKIE_SAME_SITE onto the net/http enum.
//
// The default is Lax, which is right for the common deployment where the SPA and the API
// share a registrable domain (app.example.com + api.example.com). "Same site" is about
// the registrable domain, not the origin, so a Lax refresh cookie is still sent there.
//
// It has to be `none` when they do not share one — a SPA on *.vercel.app calling an API
// on *.onrender.com, for instance. Those are genuinely different sites, and a Lax cookie
// is withheld from the cross-site refresh call, so the session dies silently the moment
// the 15-minute access token expires. That failure is invisible in development, where
// everything is on localhost.
//
// Browsers reject SameSite=None without Secure, so that combination is refused at
// startup rather than shipped as a cookie the browser discards without complaint.
func cookieSameSite(value string, secure bool) (http.SameSite, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		if !secure {
			return 0, errors.New("COOKIE_SAME_SITE=none requires COOKIE_SECURE=true, because browsers reject SameSite=None without Secure")
		}
		return http.SameSiteNoneMode, nil
	default:
		return 0, fmt.Errorf("COOKIE_SAME_SITE must be one of lax, strict or none (got %q)", value)
	}
}

func encryptionKey(encoded, environment string) ([]byte, error) {
	if encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, errors.New("FIELD_ENCRYPTION_KEY must be base64 for exactly 32 bytes")
		}
		return key, nil
	}
	if environment == "development" || environment == "test" {
		// Deterministic development-only key. Production is rejected below.
		return []byte("simpul-local-development-key-32b"), nil
	}
	return nil, errors.New("FIELD_ENCRYPTION_KEY is required outside development")
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func BoolEnv(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}
