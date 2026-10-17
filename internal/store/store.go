// Package store keeps all site content in a single SQLite file.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB

	mu    sync.Mutex
	pub   *snapshot // decoded copy of the live version, see publishedSnapshot
	pubID int64
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

-- Per-page history, replaced by whole-site versions.
DROP TABLE IF EXISTS revisions;

-- Whole-site snapshots: settings, every page and which images existed.
CREATE TABLE IF NOT EXISTS versions (
	id         INTEGER PRIMARY KEY,
	kind       TEXT    NOT NULL, -- auto | saved | published | restore
	label      TEXT    NOT NULL DEFAULT '',
	created_at TEXT    NOT NULL,
	hash       TEXT    NOT NULL, -- of the content, to skip identical snapshots
	data       BLOB    NOT NULL  -- gzipped JSON
);

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
	s := &Store{db: db}
	// Deleted images are only hidden, so restoring an older version can bring them back.
	if err := s.addColumn("media", "deleted", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) addColumn(table, col, def string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col, def))
	return err
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
