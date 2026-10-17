package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Media struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"` // file stem, e.g. "sunset-1a2b3c4d"
	Ext          string    `json:"ext"`  // ".jpg", ".png", ".gif"
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Sizes        []int     `json:"sizes"` // available widths, ascending; the last equals Width
	OriginalName string    `json:"originalName"`
	CreatedAt    time.Time `json:"createdAt"`
}

// File is the file name of the rendition with the given width.
func (m Media) File(width int) string {
	if width == m.Width || width == 0 {
		return m.Name + m.Ext
	}
	return fmt.Sprintf("%s-%d%s", m.Name, width, m.Ext)
}

// Files lists every file on disk that belongs to this media item.
func (m Media) Files() []string {
	out := make([]string, 0, len(m.Sizes))
	for _, w := range m.Sizes {
		out = append(out, m.File(w))
	}
	return out
}

func (s *Store) AddMedia(m *Media) error {
	sizes, _ := json.Marshal(m.Sizes)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO media (name, ext, width, height, sizes, original_name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, m.Name, m.Ext, m.Width, m.Height, string(sizes), m.OriginalName, ts)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	m.CreatedAt = parseTime(ts)
	return nil
}

const mediaCols = `id, name, ext, width, height, sizes, original_name, created_at`

func scanMedia(r scanner) (*Media, error) {
	var m Media
	var sizes, created string
	if err := r.Scan(&m.ID, &m.Name, &m.Ext, &m.Width, &m.Height, &sizes, &m.OriginalName, &created); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(sizes), &m.Sizes)
	m.CreatedAt = parseTime(created)
	return &m, nil
}

func (s *Store) ListMedia() ([]*Media, error) {
	rows, err := s.db.Query(`SELECT ` + mediaCols + ` FROM media ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Media(id int64) (*Media, error) {
	m, err := scanMedia(s.db.QueryRow(`SELECT `+mediaCols+` FROM media WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) DeleteMedia(id int64) error {
	_, err := s.db.Exec(`DELETE FROM media WHERE id = ?`, id)
	return err
}
