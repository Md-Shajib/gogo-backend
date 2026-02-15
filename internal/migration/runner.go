package migration

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type Runner struct {
	db *sql.DB
}

func NewRunner(db *sql.DB) *Runner {
	return &Runner{db: db}
}

func (r *Runner) Run(direction string) error {
	var fileName string

	switch direction {
	case "up":
		fileName = "000001_user.up.sql"
	case "down":
		fileName = "000001_user.down.sql"
	default:
		return fmt.Errorf("invalid direction: use 'up' or 'down'")
	}

	path := filepath.Join("pkg", "migrations", "sql", fileName)

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(string(sqlBytes))
	return err
}
