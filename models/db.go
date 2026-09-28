package models

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"time"

	"tablescore-api/config"
	"tablescore-api/migrations"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// InitDB opens the database described by cfg with the pool settings the
// server uses.
func InitDB(cfg *config.Config) (*gorm.DB, error) {
	if cfg.DatabaseURL != "" {
		return OpenDSN(cfg.DatabaseURL)
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.DB.Host,
		cfg.DB.Port,
		cfg.DB.User,
		cfg.DB.Password,
		cfg.DB.DBName,
		cfg.DB.SSLMode,
	)
	return OpenDSN(dsn)
}

// OpenDSN opens a GORM connection from a raw DSN (key=value form or a
// postgres:// URL) with the standard pool settings. Tests use it directly.
func OpenDSN(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// newProvider wraps GORM's connection pool in a goose provider over the
// embedded migrations. The provider shares the pool with GORM, so callers
// must never Close it. It takes a database-wide advisory lock for the
// duration of each Up/Down, so concurrent callers (two server replicas, or
// two test binaries migrating the same fresh database) wait rather than
// race on CREATE TABLE.
func newProvider(db *gorm.DB) (*goose.Provider, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("failed to create migration locker: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("failed to create migration provider: %w", err)
	}
	return p, nil
}

// Migrate applies every pending migration in version order. It is safe to
// call on an up-to-date database, where it does nothing.
func Migrate(db *gorm.DB) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}
	results, err := p.Up(context.Background())
	for _, r := range results {
		slog.Info("applied migration", "version", r.Source.Version, "file", path.Base(r.Source.Path))
	}
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recently applied migration and returns its
// version. When nothing is applied the error wraps goose.ErrNoNextVersion.
func MigrateDown(db *gorm.DB) (int64, error) {
	p, err := newProvider(db)
	if err != nil {
		return 0, err
	}
	res, err := p.Down(context.Background())
	if err != nil {
		return 0, fmt.Errorf("failed to roll back migration: %w", err)
	}
	slog.Info("rolled back migration", "version", res.Source.Version, "file", path.Base(res.Source.Path))
	return res.Source.Version, nil
}

// Reset rolls back every migration and applies them all again, leaving an
// empty schema at the current version. It exercises every Down section.
func Reset(db *gorm.DB) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if _, err := p.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("failed to roll back migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("failed to re-run migrations: %w", err)
	}
	return nil
}

// MigrationStatus reports each embedded migration and whether the database
// has applied it.
func MigrationStatus(db *gorm.DB) ([]*goose.MigrationStatus, error) {
	p, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	statuses, err := p.Status(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to read migration status: %w", err)
	}
	return statuses, nil
}
