package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Saves that land within this window are folded into the same revision, so
// autosave doesn't produce hundreds of near-identical entries.
const (
	revisionWindow = 10 * time.Minute
	revisionsKept  = 50
)

type Revision struct {
	ID        int64           `json:"id"`
	PageID    int64           `json:"pageId"`
	Title     string          `json:"title"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func snapshot(tx *sql.Tx, p *Page, force bool) error {
	if !force {
		var id int64
		var created string
		err := tx.QueryRow(`SELECT id, created_at FROM revisions WHERE page_id = ? ORDER BY id DESC LIMIT 1`, p.ID).
			Scan(&id, &created)
		if err == nil && time.Since(parseTime(created)) < revisionWindow {
			_, err = tx.Exec(`UPDATE revisions SET title = ?, blocks = ? WHERE id = ?`, p.Title, string(p.Blocks), id)
			return err
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO revisions (page_id, title, blocks, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Title, string(p.Blocks), now()); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM revisions WHERE page_id = ? AND id NOT IN
		(SELECT id FROM revisions WHERE page_id = ? ORDER BY id DESC LIMIT ?)`, p.ID, p.ID, revisionsKept)
	return err
}

func (s *Store) Revisions(pageID int64) ([]Revision, error) {
	rows, err := s.db.Query(`SELECT id, page_id, title, created_at FROM revisions WHERE page_id = ? ORDER BY id DESC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		var created string
		if err := rows.Scan(&r.ID, &r.PageID, &r.Title, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RestoreRevision(pageID, revID int64) (*Page, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var title, blocks string
	err = tx.QueryRow(`SELECT title, blocks FROM revisions WHERE id = ? AND page_id = ?`, revID, pageID).Scan(&title, &blocks)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE pages SET title = ?, blocks = ?, updated_at = ? WHERE id = ?`, title, blocks, now(), pageID); err != nil {
		return nil, err
	}
	// Always a fresh entry: folding a restore into the latest revision would
	// overwrite the state the user is restoring away from.
	if err := snapshot(tx, &Page{ID: pageID, Title: title, Blocks: json.RawMessage(blocks)}, true); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Page(pageID)
}

// PublishedPaths returns path -> page id for the last build.
func (s *Store) PublishedPaths() (map[string]int64, error) {
	return s.pathMap(`SELECT path, page_id FROM published_paths`)
}

func (s *Store) Redirects() (map[string]int64, error) {
	return s.pathMap(`SELECT from_path, page_id FROM redirects`)
}

func (s *Store) pathMap(q string) (map[string]int64, error) {
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var path string
		var id int64
		if err := rows.Scan(&path, &id); err != nil {
			return nil, err
		}
		out[path] = id
	}
	return out, rows.Err()
}

// RecordPublish stores the paths of a finished build and adds redirects for
// any previously published URL whose page now lives somewhere else.
func (s *Store) RecordPublish(current map[string]int64) error {
	prev, err := s.PublishedPaths()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for path, id := range prev {
		if current[path] == id {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO redirects (from_path, page_id) VALUES (?, ?)
			ON CONFLICT(from_path) DO UPDATE SET page_id = excluded.page_id`, path, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM published_paths`); err != nil {
		return err
	}
	for path, id := range current {
		if _, err := tx.Exec(`INSERT INTO published_paths (path, page_id) VALUES (?, ?)`, path, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
