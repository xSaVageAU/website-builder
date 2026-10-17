package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Page struct {
	ID          int64           `json:"id"`
	ParentID    int64           `json:"parentId"`
	Position    int             `json:"position"`
	Title       string          `json:"title"`
	Slug        string          `json:"slug"`
	ShowInMenu  bool            `json:"showInMenu"`
	Published   bool            `json:"published"`
	Description string          `json:"description"`
	Blocks      json.RawMessage `json:"blocks,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt"`

	// Filled in from the tree, not stored.
	Path string `json:"path"`
}

// Top-level slugs that would collide with folders the build writes.
var reservedSlugs = map[string]bool{"assets": true, "media": true}

// Slugify turns a title into a URL segment: lowercase letters/digits joined by dashes.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	if out == "" {
		out = "page"
	}
	return out
}

// Tree is the whole page hierarchy. Sites are small, so we always load all of it.
type Tree struct {
	Pages    []*Page // depth-first order, matches the sidebar and the menu
	ByID     map[int64]*Page
	Children map[int64][]*Page // key 0 = top level
	HomeID   int64
}

func (t *Tree) Ancestors(id int64) []*Page {
	var out []*Page
	p := t.ByID[id]
	for p != nil && p.ParentID != 0 {
		p = t.ByID[p.ParentID]
		if p != nil {
			out = append([]*Page{p}, out...)
		}
	}
	return out
}

// IsLive reports whether a page ends up on the published site: a draft hides
// itself and everything beneath it.
func (t *Tree) IsLive(id int64) bool {
	for p := t.ByID[id]; p != nil; p = t.ByID[p.ParentID] {
		if !p.Published {
			return false
		}
		if p.ParentID == 0 {
			break
		}
	}
	return t.ByID[id] != nil
}

func (t *Tree) computePaths() {
	var walk func(parent int64, prefix string)
	walk = func(parent int64, prefix string) {
		for _, p := range t.Children[parent] {
			own := prefix + p.Slug + "/"
			p.Path = own
			if p.ID == t.HomeID {
				p.Path = "/"
			}
			// Children of the home page keep the home slug in their URL so
			// they can never collide with top-level pages.
			walk(p.ID, own)
		}
	}
	walk(0, "/")
}

func (s *Store) Tree() (*Tree, error) {
	site, err := s.Site()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, COALESCE(parent_id, 0), position, title, slug, show_in_menu,
		published, description, blocks, updated_at FROM pages ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	t := &Tree{ByID: map[int64]*Page{}, Children: map[int64][]*Page{}, HomeID: site.HomePageID}
	var all []*Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, p)
		t.ByID[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range all {
		parent := p.ParentID
		if _, ok := t.ByID[parent]; !ok {
			parent = 0
		}
		t.Children[parent] = append(t.Children[parent], p)
	}
	for _, kids := range t.Children {
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Position < kids[j].Position })
	}
	if _, ok := t.ByID[t.HomeID]; !ok {
		t.HomeID = 0
		if top := t.Children[0]; len(top) > 0 {
			t.HomeID = top[0].ID
		}
	}
	var order func(parent int64)
	order = func(parent int64) {
		for _, p := range t.Children[parent] {
			t.Pages = append(t.Pages, p)
			order(p.ID)
		}
	}
	order(0)
	t.computePaths()
	return t, nil
}

type scanner interface{ Scan(...any) error }

func scanPage(r scanner) (*Page, error) {
	var p Page
	var menu, pub int
	var blocks, updated string
	if err := r.Scan(&p.ID, &p.ParentID, &p.Position, &p.Title, &p.Slug, &menu, &pub,
		&p.Description, &blocks, &updated); err != nil {
		return nil, err
	}
	p.ShowInMenu = menu == 1
	p.Published = pub == 1
	p.Blocks = json.RawMessage(blocks)
	p.UpdatedAt = parseTime(updated)
	return &p, nil
}

// Page returns one page with its path filled in.
func (s *Store) Page(id int64) (*Page, error) {
	t, err := s.Tree()
	if err != nil {
		return nil, err
	}
	p, ok := t.ByID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func uniqueSlug(tx *sql.Tx, parentID int64, slug string, excludeID int64) (string, error) {
	slug = Slugify(slug)
	if parentID == 0 && reservedSlugs[slug] {
		slug += "-page"
	}
	base := slug
	for n := 2; ; n++ {
		var exists int
		err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE COALESCE(parent_id, 0) = ? AND slug = ? AND id != ?`,
			parentID, slug, excludeID).Scan(&exists)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}

// CreatePage adds a page at the end of its parent's children.
func (s *Store) CreatePage(parentID int64, title string, blocks json.RawMessage) (*Page, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New page"
	}
	if len(blocks) == 0 {
		blocks = json.RawMessage("[]")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if parentID != 0 {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ?`, parentID).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ErrNotFound
		}
	}
	slug, err := uniqueSlug(tx, parentID, title, 0)
	if err != nil {
		return nil, err
	}
	var pos int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM pages WHERE COALESCE(parent_id, 0) = ?`,
		parentID).Scan(&pos); err != nil {
		return nil, err
	}
	ts := now()
	res, err := tx.Exec(`INSERT INTO pages (parent_id, position, title, slug, blocks, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, nullID(parentID), pos, title, slug, string(blocks), ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// The first page of a site becomes its home page.
	site, err := s.Site()
	if err != nil {
		return nil, err
	}
	if site.HomePageID == 0 {
		site.HomePageID = id
		if err := s.SaveSite(site); err != nil {
			return nil, err
		}
	}
	return s.Page(id)
}

// PageUpdate holds the editable fields; nil means "leave unchanged".
type PageUpdate struct {
	Title       *string          `json:"title"`
	Slug        *string          `json:"slug"`
	ShowInMenu  *bool            `json:"showInMenu"`
	Published   *bool            `json:"published"`
	Description *string          `json:"description"`
	Blocks      *json.RawMessage `json:"blocks"`
}

func (s *Store) UpdatePage(id int64, u PageUpdate) (*Page, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	p, err := scanPage(tx.QueryRow(`SELECT id, COALESCE(parent_id, 0), position, title, slug, show_in_menu,
		published, description, blocks, updated_at FROM pages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.Title != nil {
		p.Title = strings.TrimSpace(*u.Title)
		if p.Title == "" {
			p.Title = "Untitled"
		}
	}
	if u.Slug != nil && Slugify(*u.Slug) != p.Slug {
		if p.Slug, err = uniqueSlug(tx, p.ParentID, *u.Slug, p.ID); err != nil {
			return nil, err
		}
	}
	if u.ShowInMenu != nil {
		p.ShowInMenu = *u.ShowInMenu
	}
	if u.Published != nil {
		p.Published = *u.Published
	}
	if u.Description != nil {
		p.Description = strings.TrimSpace(*u.Description)
	}
	if u.Blocks != nil {
		p.Blocks = *u.Blocks
	}
	_, err = tx.Exec(`UPDATE pages SET title = ?, slug = ?, show_in_menu = ?, published = ?, description = ?,
		blocks = ?, updated_at = ? WHERE id = ?`,
		p.Title, p.Slug, boolInt(p.ShowInMenu), boolInt(p.Published), p.Description, string(p.Blocks), now(), p.ID)
	if err != nil {
		return nil, err
	}
	if u.Title != nil || u.Blocks != nil {
		if err := snapshot(tx, p, false); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Page(id)
}

// MovePage puts a page under newParent at the given index among its siblings.
func (s *Store) MovePage(id, newParent int64, index int) error {
	t, err := s.Tree()
	if err != nil {
		return err
	}
	if t.ByID[id] == nil || (newParent != 0 && t.ByID[newParent] == nil) {
		return ErrNotFound
	}
	for p := t.ByID[newParent]; p != nil; p = t.ByID[p.ParentID] {
		if p.ID == id {
			return errors.New("a page can't be moved inside itself")
		}
		if p.ParentID == 0 {
			break
		}
	}

	var siblings []int64
	for _, p := range t.Children[newParent] {
		if p.ID != id {
			siblings = append(siblings, p.ID)
		}
	}
	index = max(0, min(index, len(siblings)))
	siblings = append(siblings[:index], append([]int64{id}, siblings[index:]...)...)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if t.ByID[id].ParentID != newParent {
		slug, err := uniqueSlug(tx, newParent, t.ByID[id].Slug, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE pages SET parent_id = ?, slug = ? WHERE id = ?`, nullID(newParent), slug, id); err != nil {
			return err
		}
	}
	for i, sid := range siblings {
		if _, err := tx.Exec(`UPDATE pages SET position = ? WHERE id = ?`, i, sid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeletePage removes a page and all of its subpages.
func (s *Store) DeletePage(id int64) error {
	res, err := s.db.Exec(`DELETE FROM pages WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	site, err := s.Site()
	if err != nil {
		return err
	}
	if site.HomePageID == id {
		site.HomePageID = 0
		t, err := s.Tree()
		if err != nil {
			return err
		}
		site.HomePageID = t.HomeID
		return s.SaveSite(site)
	}
	return nil
}

// CountDescendants is used to warn before a delete takes subpages with it.
func (t *Tree) CountDescendants(id int64) int {
	n := 0
	for _, c := range t.Children[id] {
		n += 1 + t.CountDescendants(c.ID)
	}
	return n
}
