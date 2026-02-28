// Package db handles database connection and migrations.
//
// This package is part of the "infrastructure" layer — it knows about the
// specific database technology (SQLite + GORM) but nothing about business logic.
// Other layers receive a *gorm.DB and use it without caring how it was created.
package db

import (
	"context"

	"gorm.io/driver/sqlite" // GORM's SQLite driver adapter
	"gorm.io/gorm"          // GORM is an ORM (Object-Relational Mapper) for Go
	"gorm.io/gorm/schema"   // Schema configuration (table naming rules, etc.)

	// The underscore "_" import means we import this package only for its side
	// effects (it registers itself as a database/sql driver named "sqlite").
	// This is a pure-Go SQLite implementation — no C compiler (CGO) needed.
	_ "modernc.org/sqlite"

	"notes-api/internal/model" // Our GORM model structs (used for migrations)
)

// Config holds database connection pool settings.
// These are operational tuning knobs — not business rules — so they live here
// in the infrastructure layer rather than in the service layer.
type Config struct {
	MaxOpenConns int // Maximum number of open connections to the database
	MaxIdleConns int // Maximum number of idle connections kept in the pool
}

// DefaultConfig returns a Config with sensible defaults.
// main.go can start from these and override individual fields from env vars.
func DefaultConfig() Config {
	return Config{
		MaxOpenConns: 10,
		MaxIdleConns: 10,
	}
}

// OpenSQLite creates and returns a GORM database connection to a SQLite file.
//
// Parameters:
//   - ctx: a context for cancellation (e.g., if the app is shutting down)
//   - path: filesystem path to the SQLite database file (e.g. "./notes.db")
//   - cfg: connection pool settings (use DefaultConfig() for sensible defaults)
//
// Returns:
//   - *gorm.DB: the GORM database handle you use to run queries
//   - error: non-nil if anything went wrong (bad path, corrupt DB, etc.)
func OpenSQLite(ctx context.Context, path string, cfg Config) (*gorm.DB, error) {
	// gorm.Open() connects to the database. We pass:
	// 1. A Dialector — tells GORM which database engine to use (here SQLite)
	//    - DriverName: "sqlite" matches the driver registered by modernc.org/sqlite
	//    - DSN: "file:./notes.db" is the Data Source Name — where the DB file is
	// 2. A Config — settings that customize GORM's behavior
	database, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite",
		DSN:        "file:" + path,
	}, &gorm.Config{
		// NamingStrategy controls how GORM names tables from struct names.
		// SingularTable: false means struct "Note" → table "notes" (pluralized).
		// If true, it would be "note" (singular).
		NamingStrategy: schema.NamingStrategy{
			SingularTable: false,
		},
	})
	if err != nil {
		return nil, err
	}

	// database.DB() gives us the underlying *sql.DB (Go's standard database handle).
	// We need it to configure connection pooling and to ping the database.
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}

	// Connection pool limits prevent the app from opening too many connections.
	// For SQLite this matters less (it's a file, not a server), but it's still
	// good practice. In a PostgreSQL/MySQL app, this would be critical.
	// Values come from cfg, which is populated from env vars in main.go.
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// PingContext verifies the database is reachable and the file is valid.
	// If it fails, we close the connection and return the error.
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close() // Best-effort cleanup; we ignore the Close error
		return nil, err
	}

	return database, nil
}

// Migrate runs GORM's AutoMigrate, which automatically creates or updates
// database tables to match our Go structs.
//
// For example, if model.Note has fields ID and Content, AutoMigrate will:
//   - Create the "notes" table if it doesn't exist
//   - Add any new columns if we add fields to the struct later
//   - It will NOT delete columns or data (it's safe to run on every startup)
//
// WithContext(ctx) ensures the migration can be cancelled if the app is
// shutting down.
func Migrate(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).AutoMigrate(&model.Note{})
}
