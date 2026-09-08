package generation

import (
	"database/sql"
	"fmt"
)

// SQLRunStore owns the project's write database. The application supplies a
// MariaDB-compatible sql.DB configured with write credentials.
type SQLRunStore struct {
	db *sql.DB
}

func NewSQLRunStore(db *sql.DB) (*SQLRunStore, error) {
	if db == nil {
		return nil, fmt.Errorf("write database is required")
	}
	return &SQLRunStore{db: db}, nil
}
