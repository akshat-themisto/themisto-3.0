package store

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type Store struct {
	DB         *sql.DB
	bodyCipher *bodyCipher
}

func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	bodyCipher, err := newBodyCipherFromEnv()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init dlp body cipher: %w", err)
	}

	return &Store{DB: db, bodyCipher: bodyCipher}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) Healthy() bool {
	return s.DB.Ping() == nil
}
