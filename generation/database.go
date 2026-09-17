package generation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
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

// ReadReplicaConfigFromEnvironment reads the read-only historical source.
// It intentionally does not use the generation write-database configuration.
func ReadReplicaConfigFromEnvironment() (DatabaseConfig, error) {
	username := os.Getenv("SITE_DB_USERNAME_NOMAD")
	password := os.Getenv("SITE_DB_PASSWORD_NOMAD")
	host := os.Getenv("SITE_DB_HOST")
	database := os.Getenv("SITE_DB")
	if username == "" || password == "" || host == "" || database == "" {
		return DatabaseConfig{}, fmt.Errorf("SITE_DB_USERNAME_NOMAD, SITE_DB_PASSWORD_NOMAD, SITE_DB_HOST, and SITE_DB must be set")
	}
	if !strings.Contains(host, ":") {
		host += ":3306"
	}
	config := mysql.NewConfig()
	config.User, config.Passwd, config.Net, config.Addr, config.DBName = username, password, "tcp", host, database
	config.ParseTime, config.Loc, config.TLSConfig = true, time.UTC, "preferred"
	return DatabaseConfig{Dialect: MariaDB, DSN: config.FormatDSN()}, nil
}

// OpenReadReplica opens the historical MariaDB source used to derive context
// and generate cards. Generation runs and results are written separately.
func OpenReadReplica(ctx context.Context) (*sql.DB, error) {
	config, err := ReadReplicaConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("open MariaDB replica: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping MariaDB replica: %w", err)
	}
	return db, nil
}
