package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/simpul/hr-backend/internal/config"
	"github.com/simpul/hr-backend/internal/platform/database"
	"github.com/simpul/hr-backend/internal/schemacheck"
	"github.com/simpul/hr-backend/internal/seed"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	// Validated before the database is touched, so a rejected password cannot leave a
	// half-created workspace behind.
	if err := seed.Validate(cfg.Environment, cfg.SeedAdminPassword); err != nil {
		logger.Error("refusing to seed", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(context.Background(), cfg.DatabaseURL, cfg.Environment == "development")
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	// Checked before any write. `migrate up` exits 0 when schema_migrations is already at the
	// latest version, which it also is in a database that belongs to a different application —
	// so without this the operator gets a raw "column password_hash does not exist" from deep
	// inside the seed transaction instead of an explanation they can act on.
	if err := schemacheck.Verify(db); err != nil {
		logger.Error("schema check failed", "error", err)
		os.Exit(1)
	}
	if err := seed.Run(context.Background(), db, cfg.SeedAdminPassword); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
	logger.Info("seed complete", "organization", "simpul-demo", "admin", "sarah@simpul.co.id", "schema", schemacheck.Describe(db))
}
