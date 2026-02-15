package migrations

import "database/sql"

type MigrationRunner struct {
	db *sql.DB
}

func NewMigrationRunner(db *sql.DB) *MigrationRunner {
	return &MigrationRunner{db: db}
}

