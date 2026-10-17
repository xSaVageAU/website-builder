package store

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// A version is a snapshot of the whole site: settings, every page, and which
// images existed. Restoring one puts all of that back.
type Version struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // auto | saved | published | restore
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
	Live      bool      `json:"live"` // the version currently published
}

type snapPage struct {
	ID          int64           `json:"id"`
	ParentID    int64           `json:"parentId"`
	Position    int             `json:"position"`
	Title       string          `json:"title"`
	Slug        string          `json:"slug"`
	ShowInMenu  bool            `json:"showInMenu"`
	Published   bool            `json:"published"`
	Description string          `json:"description"`
	Blocks      json.RawMessage `json:"blocks"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
}

type snapshot struct {
	Site  json.RawMessage `json:"site"`
	Pages []snapPage      `json:"pages"`
	Media []int64         `json:"media"`
}

// contentHash ignores timestamps, so identical content always hashes the same.
func (p snapPage) contentHash() string {
	b, _ := json.Marshal([]any{p.ParentID, p.Position, p.Title, p.Slug, p.ShowInMenu, p.Published, p.Description, p.Blocks})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (d *snapshot) hash() string {
	h := sha256.New()
	h.Write(d.Site)
	for _, p := range d.Pages {
		fmt.Fprintf(h, "|%d:%s", p.ID, p.contentHash())
	}
	return hex.EncodeToString(h.Sum(nil))
}

type querier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func capture(q querier) (*snapshot, error) {
	d := &snapshot{Pages: []snapPage{}, Media: []int64{}}
	var site string
	err := q.QueryRow(`SELECT value FROM settings WHERE key = 'site'`).Scan(&site)
	if errors.Is(err, sql.ErrNoRows) {
		b, _ := json.Marshal(DefaultSite())
		site, err = string(b), nil
	}
	if err != nil {
		return nil, err
	}
	d.Site = json.RawMessage(site)

	rows, err := q.Query(`SELECT id, COALESCE(parent_id, 0), position, title, slug, show_in_menu, published,
		description, blocks, created_at, updated_at FROM pages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p snapPage
		var menu, pub int
		var blocks string
		if err := rows.Scan(&p.ID, &p.ParentID, &p.Position, &p.Title, &p.Slug, &menu, &pub,
			&p.Description, &blocks, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.ShowInMenu, p.Published, p.Blocks = menu == 1, pub == 1, json.RawMessage(blocks)
		d.Pages = append(d.Pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mrows, err := q.Query(`SELECT id FROM media WHERE deleted = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var id int64
		if err := mrows.Scan(&id); err != nil {
			return nil, err
		}
		d.Media = append(d.Media, id)
	}
	return d, mrows.Err()
}

func encode(d *snapshot) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(d); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decode(b []byte) (*snapshot, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var d snapshot
	return &d, json.Unmarshal(raw, &d)
}

func (s *Store) insertVersion(d *snapshot, kind, label string) (int64, error) {
	data, err := encode(d)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO versions (kind, label, created_at, hash, data) VALUES (?, ?, ?, ?, ?)`,
		kind, label, now(), d.hash(), data)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveVersion snapshots the site as it is right now.
func (s *Store) SaveVersion(kind, label string) (int64, error) {
	d, err := capture(s.db)
	if err != nil {
		return 0, err
	}
	return s.insertVersion(d, kind, label)
}

// AutoVersion is called before every change. If the newest version is older
// than interval and the site has changed since, it snapshots the site as it
// is *before* the change. That way any moment of editing is at most one
// interval away from a version, including the end of the previous session.
func (s *Store) AutoVersion(interval time.Duration) error {
	site, err := s.Site()
	if err != nil || !site.SetupDone {
		return err
	}
	var created, hash string
	err = s.db.QueryRow(`SELECT created_at, hash FROM versions ORDER BY id DESC LIMIT 1`).Scan(&created, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && time.Since(parseTime(created)) < interval {
		return nil
	}
	d, err := capture(s.db)
	if err != nil {
		return err
	}
	if d.hash() == hash {
		return nil
	}
	if _, err := s.insertVersion(d, "auto", ""); err != nil {
		return err
	}
	return s.pruneVersions(time.Now())
}

// pruneVersions thins out automatic versions: all from the last day, then
// one per day for a month, then one per week. Saved and published versions
// are always kept.
func (s *Store) pruneVersions(now time.Time) error {
	rows, err := s.db.Query(`SELECT id, created_at FROM versions WHERE kind IN ('auto', 'restore') ORDER BY id DESC`)
	if err != nil {
		return err
	}
	var drop []int64
	seen := map[string]bool{}
	for rows.Next() {
		var id int64
		var created string
		if err := rows.Scan(&id, &created); err != nil {
			rows.Close()
			return err
		}
		t := parseTime(created).Local()
		age := now.Sub(t)
		var bucket string
		switch {
		case age < 24*time.Hour:
			continue
		case age < 30*24*time.Hour:
			bucket = t.Format("2006-01-02")
		default:
			y, w := t.ISOWeek()
			bucket = fmt.Sprintf("%d-w%d", y, w)
		}
		if seen[bucket] {
			drop = append(drop, id)
		}
		seen[bucket] = true
	}
	rows.Close()
	for _, id := range drop {
		if _, err := s.db.Exec(`DELETE FROM versions WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) liveVersionID() int64 {
	var v string
	if s.db.QueryRow(`SELECT value FROM settings WHERE key = 'live_version'`).Scan(&v) != nil {
		return 0
	}
	var id int64
	fmt.Sscan(v, &id)
	return id
}

// MarkPublished snapshots the site and records it as what's live.
func (s *Store) MarkPublished() error {
	id, err := s.SaveVersion("published", "")
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value) VALUES ('live_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprint(id))
	return err
}

func (s *Store) Versions() ([]Version, error) {
	live := s.liveVersionID()
	rows, err := s.db.Query(`SELECT id, kind, label, created_at FROM versions ORDER BY id DESC LIMIT 300`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		var created string
		if err := rows.Scan(&v.ID, &v.Kind, &v.Label, &created); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(created)
		v.Live = v.ID == live
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) loadVersion(id int64) (*snapshot, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM versions WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decode(data)
}

// CurrentVersion returns the newest version identical to the site as it is
// now, or 0 if what's on screen isn't in any version yet ("unsaved").
func (s *Store) CurrentVersion() (int64, error) {
	d, err := capture(s.db)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow(`SELECT id FROM versions WHERE hash = ? ORDER BY id DESC LIMIT 1`, d.hash()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// keepCurrent snapshots the site before it's replaced, if the user asked to
// and no version already holds this exact state.
func (s *Store) keepCurrent(save bool) error {
	if !save {
		return nil
	}
	cur, err := s.CurrentVersion()
	if err != nil || cur != 0 {
		return err
	}
	_, err = s.SaveVersion("restore", "")
	return err
}

// RestoreVersion puts the whole site back the way it was. With save, the
// current state is kept as a version first (unless one already has it).
func (s *Store) RestoreVersion(id int64, save bool) error {
	d, err := s.loadVersion(id)
	if err != nil {
		return err
	}
	if err := s.keepCurrent(save); err != nil {
		return err
	}
	return s.apply(d)
}

var ErrNeverPublished = errors.New("the site hasn't been published yet")

// DiscardChanges goes back to the version that was last published.
func (s *Store) DiscardChanges(save bool) error {
	id := s.liveVersionID()
	if id == 0 {
		return ErrNeverPublished
	}
	return s.RestoreVersion(id, save)
}

func (s *Store) apply(d *snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Pages reference their parents; check those at commit, once every page is back.
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	ids := make([]int64, 0, len(d.Pages))
	for _, p := range d.Pages {
		ids = append(ids, p.ID)
		// Upsert rather than delete-and-reinsert: deleting would cascade away
		// the publish history (old URLs) that redirects are built from.
		if _, err := tx.Exec(`INSERT INTO pages (id, parent_id, position, title, slug, show_in_menu, published,
			description, blocks, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET parent_id = excluded.parent_id, position = excluded.position,
			title = excluded.title, slug = excluded.slug, show_in_menu = excluded.show_in_menu,
			published = excluded.published, description = excluded.description, blocks = excluded.blocks,
			updated_at = excluded.updated_at`,
			p.ID, nullID(p.ParentID), p.Position, p.Title, p.Slug, boolInt(p.ShowInMenu), boolInt(p.Published),
			p.Description, string(p.Blocks), p.CreatedAt, now()); err != nil {
			return err
		}
	}
	idList, _ := json.Marshal(ids)
	// Pages created since that version.
	if _, err := tx.Exec(`DELETE FROM pages WHERE id NOT IN (SELECT value FROM json_each(?))`, string(idList)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('site', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(d.Site)); err != nil {
		return err
	}
	// Images deleted since come back. Images uploaded since stay: the media
	// library isn't part of an experiment, and losing uploads would surprise.
	mediaList, _ := json.Marshal(d.Media)
	if _, err := tx.Exec(`UPDATE media SET deleted = 0 WHERE id IN (SELECT value FROM json_each(?))`, string(mediaList)); err != nil {
		return err
	}
	return tx.Commit()
}

// RevertPage puts one page's content back to how it was last published.
func (s *Store) RevertPage(pageID int64, save bool) (*Page, error) {
	id := s.liveVersionID()
	if id == 0 {
		return nil, ErrNeverPublished
	}
	d, err := s.loadVersion(id)
	if err != nil {
		return nil, err
	}
	for _, p := range d.Pages {
		if p.ID != pageID {
			continue
		}
		if err := s.keepCurrent(save); err != nil {
			return nil, err
		}
		blocks := p.Blocks
		return s.UpdatePage(pageID, PageUpdate{Title: &p.Title, Slug: &p.Slug, ShowInMenu: &p.ShowInMenu,
			Published: &p.Published, Description: &p.Description, Blocks: &blocks})
	}
	return nil, errors.New("this page is new since the last publish, so there's nothing to go back to")
}

// Changes compares the site with what was last published.
type Changes struct {
	EverPublished bool             `json:"everPublished"`
	Pages         map[int64]string `json:"pages"` // page id -> "changed" | "new"
	Deleted       int              `json:"deleted"`
	Site          bool             `json:"site"` // design or settings
	Count         int              `json:"count"`
}

func (s *Store) Changes() (*Changes, error) {
	c := &Changes{Pages: map[int64]string{}}
	id := s.liveVersionID()
	if id == 0 {
		return c, nil
	}
	pub, err := s.publishedSnapshot(id)
	if err != nil {
		return nil, err
	}
	cur, err := capture(s.db)
	if err != nil {
		return nil, err
	}
	c.EverPublished = true
	old := map[int64]string{}
	for _, p := range pub.Pages {
		old[p.ID] = p.contentHash()
	}
	for _, p := range cur.Pages {
		h, ok := old[p.ID]
		switch {
		case !ok:
			c.Pages[p.ID] = "new"
		case h != p.contentHash():
			c.Pages[p.ID] = "changed"
		}
		delete(old, p.ID)
	}
	c.Deleted = len(old)
	c.Site = !bytes.Equal(pub.Site, cur.Site)
	c.Count = len(c.Pages) + c.Deleted
	if c.Site {
		c.Count++
	}
	return c, nil
}

// The published snapshot is read on every page-list request, so keep the
// decoded copy around until something else gets published.
func (s *Store) publishedSnapshot(id int64) (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pubID == id && s.pub != nil {
		return s.pub, nil
	}
	d, err := s.loadVersion(id)
	if err != nil {
		return nil, err
	}
	s.pub, s.pubID = d, id
	return d, nil
}
