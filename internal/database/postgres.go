package database

import (
	"database/sql"
	"log"
	"os"
)

func NewPostgresDb() *sql.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Failed to open database connection:", err)
	}

	return db
}