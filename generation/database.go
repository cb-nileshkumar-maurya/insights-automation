package generation

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type DatabaseDialect string

const (
	SQLite  DatabaseDialect = "sqlite"
	MariaDB DatabaseDialect = "mariadb"
)

type DatabaseConfig struct {
	Dialect DatabaseDialect
	DSN     string
}

func OpenSQLRunStore(ctx context.Context, config DatabaseConfig) (*SQLRunStore, error) {
	if config.DSN == "" {
		return nil, fmt.Errorf("write database DSN is required")
	}
	driver := "mysql"
	if config.Dialect == SQLite {
		driver = "sqlite3"
	}
	if config.Dialect != SQLite && config.Dialect != MariaDB {
		return nil, fmt.Errorf("unsupported write database dialect %q", config.Dialect)
	}
	db, err := sql.Open(driver, config.DSN)
	if err != nil {
		return nil, fmt.Errorf("open write database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping write database: %w", err)
	}
	store, err := NewSQLRunStoreWithDialect(db, config.Dialect)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}
