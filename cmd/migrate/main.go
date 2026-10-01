package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/simpul/hr-backend/internal/config"
	"github.com/simpul/hr-backend/migrations"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	direction := "up"
	if len(os.Args) > 1 {
		direction = os.Args[1]
	}
	// The migrations are embedded in the binary rather than read from a relative
	// directory, so this works from any working directory — which is what a
	// container or a systemd unit will do.
	source, err := migrations.Source()
	if err != nil {
		panic(err)
	}
	migration, err := migrate.NewWithSourceInstance("iofs", source, cfg.DatabaseURL)
	if err != nil {
		// NewWithSourceInstance only takes ownership of the source on success, so a
		// failure here would otherwise leak it.
		_ = source.Close()
		panic(err)
	}
	defer migration.Close()
	switch direction {
	case "up":
		if len(os.Args) > 2 {
			steps, parseErr := strconv.Atoi(os.Args[2])
			if parseErr != nil || steps < 1 {
				err = fmt.Errorf("up steps must be a positive integer")
			} else {
				err = migration.Steps(steps)
			}
		} else {
			err = migration.Up()
		}
	case "down":
		if len(os.Args) > 2 {
			steps, parseErr := strconv.Atoi(os.Args[2])
			if parseErr != nil || steps < 1 {
				err = fmt.Errorf("down steps must be a positive integer")
			} else {
				err = migration.Steps(-steps)
			}
		} else {
			err = migration.Down()
		}
	case "version":
		var version uint
		var dirty bool
		version, dirty, err = migration.Version()
		if err == nil {
			fmt.Printf("version %d dirty=%t\n", version, dirty)
			return
		}
	default:
		err = fmt.Errorf("unknown direction %q", direction)
	}
	if err != nil && err != migrate.ErrNoChange {
		panic(err)
	}
	fmt.Println("migration", direction, "complete")
}
