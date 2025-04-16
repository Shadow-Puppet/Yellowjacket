package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:generate sqlc vet
//go:generate sqlc generate

type DB struct{
	db *sql.DB
}

func NewDB(sqliteDBFilePath string) (*DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("could not connect to sqlite database: %w", err)
	}
	return &DB{
		db: db,
	}, nil
}
