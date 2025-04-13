package database

//go:generate sqlc generate

type DB struct{}

func NewDB() (*DB, error) {
	return &DB{}, nil
}
