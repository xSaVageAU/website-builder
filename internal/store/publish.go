package store

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
