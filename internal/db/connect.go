package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:generate sh -c "sqlc generate"

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(appRoot string) (*sql.DB, error) {
	// TODO: move this db connection and migration code to a dedicated function.
	dbPath := filepath.Join(appRoot, "sand.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	dbDriver, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database driver: %w", err)
	}
	// Enable WAL mode for better concurrency
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Initialize or migrate db schema
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("could not read embedded db migration scripts: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", dbDriver)
	if err != nil {
		return nil, fmt.Errorf("could not create db migration: %w", err)
	}

	if err := m.Up(); errors.Is(err, migrate.ErrNoChange) {
		// no-op
	} else if err != nil {
		return nil, fmt.Errorf("db migration failed: %w", err)
	}

	return sqlDB, nil
}
