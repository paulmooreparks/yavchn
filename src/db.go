package main

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS articles (
  url_hash   TEXT PRIMARY KEY,
  url        TEXT NOT NULL,
  fetched_at INTEGER NOT NULL,
  title      TEXT NOT NULL,
  byline     TEXT NOT NULL,
  content    TEXT NOT NULL
);
`

func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
