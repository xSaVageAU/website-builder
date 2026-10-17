package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"sitesmith/internal/blocks"
)

// A reusable section lives once in the site's library; pages place it with a
// "shared" block that points at it, so editing it updates every page.
type Shared struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Block     json.RawMessage `json:"block"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Uses      int             `json:"uses"` // pages that place it
}

func (s *Store) SharedBlocks() ([]Shared, error) {
	rows, err := s.db.Query(`SELECT id, name, block, updated_at FROM shared_blocks ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Shared{}
	for rows.Next() {
		var sh Shared
		var block, updated string
		if err := rows.Scan(&sh.ID, &sh.Name, &block, &updated); err != nil {
			return nil, err
		}
		sh.Block, sh.UpdatedAt = json.RawMessage(block), parseTime(updated)
		out = append(out, sh)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	uses, err := s.sharedUses()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Uses = uses[out[i].ID]
	}
	return out, nil
}

// sharedUses counts, per reusable section, the pages that place it.
func (s *Store) sharedUses() (map[string]int, error) {
	t, err := s.Tree()
	if err != nil {
		return nil, err
	}
	uses := map[string]int{}
	for _, p := range t.Pages {
		seen := map[string]bool{}
		for _, b := range blocks.Parse(p.Blocks) {
			if ref, _ := b.Data["ref"].(string); b.Type == "shared" && !seen[ref] {
				seen[ref] = true
				uses[ref]++
			}
		}
	}
	return uses, nil
}

func cleanName(name string) string {
	name = clip(name, 80)
	if name == "" {
		name = "Reusable section"
	}
	return name
}

// CreateShared adds a section to the library. block must already be normalised.
func (s *Store) CreateShared(name string, block json.RawMessage) (*Shared, error) {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	sh := &Shared{ID: "r" + hex.EncodeToString(b), Name: cleanName(name), Block: block}
	ts := now()
	if _, err := s.db.Exec(`INSERT INTO shared_blocks (id, name, block, updated_at) VALUES (?, ?, ?, ?)`,
		sh.ID, sh.Name, string(block), ts); err != nil {
		return nil, err
	}
	sh.UpdatedAt = parseTime(ts)
	return sh, nil
}

// UpdateShared changes a reusable section's name and/or content (nil = keep).
func (s *Store) UpdateShared(id string, name *string, block *json.RawMessage) error {
	var cur Shared
	var raw string
	err := s.db.QueryRow(`SELECT name, block FROM shared_blocks WHERE id = ?`, id).Scan(&cur.Name, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if name != nil {
		cur.Name = cleanName(*name)
	}
	if block != nil {
		raw = string(*block)
	}
	_, err = s.db.Exec(`UPDATE shared_blocks SET name = ?, block = ?, updated_at = ? WHERE id = ?`, cur.Name, raw, now(), id)
	return err
}

// DeleteShared removes a reusable section from the library. Every page that
// placed it gets its own copy first, so no page loses content.
func (s *Store) DeleteShared(id string) error {
	var raw string
	err := s.db.QueryRow(`SELECT block FROM shared_blocks WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	t, err := s.Tree()
	if err != nil {
		return err
	}
	for _, p := range t.Pages {
		if !strings.Contains(string(p.Blocks), id) {
			continue
		}
		var list []map[string]any
		if err := json.Unmarshal(p.Blocks, &list); err != nil {
			return err
		}
		changed := false
		for i, b := range list {
			data, _ := b["data"].(map[string]any)
			if b["type"] != "shared" || data["ref"] != id {
				continue
			}
			var copy map[string]any
			if err := json.Unmarshal([]byte(raw), &copy); err != nil {
				return err
			}
			copy["id"] = b["id"] // keep the page's own section id
			list[i] = copy
			changed = true
		}
		if !changed {
			continue
		}
		out, _ := json.Marshal(list)
		clean, err := blocks.Normalize(out)
		if err != nil {
			return err
		}
		if _, err := s.UpdatePage(p.ID, PageUpdate{Blocks: &clean}); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`DELETE FROM shared_blocks WHERE id = ?`, id)
	return err
}
