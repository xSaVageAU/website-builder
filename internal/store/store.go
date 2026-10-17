// Package store keeps all site content in a single SQLite file.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pages (
	id           INTEGER PRIMARY KEY,
	parent_id    INTEGER REFERENCES pages(id) ON DELETE CASCADE,
	position     INTEGER NOT NULL DEFAULT 0,
	title        TEXT    NOT NULL,
	slug         TEXT    NOT NULL,
	show_in_menu INTEGER NOT NULL DEFAULT 1,
	published    INTEGER NOT NULL DEFAULT 1,
	description  TEXT    NOT NULL DEFAULT '',
	blocks       TEXT    NOT NULL DEFAULT '[]',
	created_at   TEXT    NOT NULL,
	updated_at   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS pages_parent ON pages(parent_id, position);

CREATE TABLE IF NOT EXISTS revisions (
	id         INTEGER PRIMARY KEY,
	page_id    INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	title      TEXT    NOT NULL,
	blocks     TEXT    NOT NULL,
	created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS revisions_page ON revisions(page_id, id);

-- Paths that were live in the last published build. Used to work out which
-- URLs moved, so only URLs the public actually saw get redirects.
CREATE TABLE IF NOT EXISTS published_paths (
	path    TEXT PRIMARY KEY,
	page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE
);

-- Old URL -> page. Resolved to the page's current path at build time, so a
-- page that moves twice never produces a redirect chain.
CREATE TABLE IF NOT EXISTS redirects (
	from_path TEXT PRIMARY KEY,
	page_id   INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS media (
	id            INTEGER PRIMARY KEY,
	name          TEXT    NOT NULL UNIQUE,
	ext           TEXT    NOT NULL,
	width         INTEGER NOT NULL,
	height        INTEGER NOT NULL,
	sizes         TEXT    NOT NULL DEFAULT '[]',
	original_name TEXT    NOT NULL DEFAULT '',
	created_at    TEXT    NOT NULL
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// One user, one process: a single connection sidesteps SQLITE_BUSY entirely.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
