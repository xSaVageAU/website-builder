package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"sitesmith/internal/build"
	"sitesmith/internal/render"
	"sitesmith/internal/store"
)

// A workspace is a folder of sites, one subfolder each:
//
//	<root>/sites/<id>/site.db, media/, published/
//	<root>/trash/<id>-<time>/   removed sites, kept until the user empties it
//	<root>/state.json           which site was open last
//
// One site is open at a time. Switching closes its database and opens another.
type Workspace struct {
	root string
	r    *render.Renderer

	// Held for reading by every request that touches the open site, and for
	// writing while switching, so no request sees a half-closed store.
	mu  sync.RWMutex
	cur *siteCtx
}

type siteCtx struct {
	id       string
	dir      string
	st       *store.Store
	builder  *build.Builder
	mediaDir string
	distDir  string
}

type SiteInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SetupDone bool      `json:"setupDone"`
	UpdatedAt time.Time `json:"updatedAt"`
	Current   bool      `json:"current"`
}

var siteID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

var errBadID = errors.New("unknown site")

func OpenWorkspace(root string, r *render.Renderer) (*Workspace, error) {
	w := &Workspace{root: root, r: r}
	if err := os.MkdirAll(w.sitesDir(), 0o755); err != nil {
		return nil, err
	}
	if err := w.migrateLegacy(); err != nil {
		return nil, fmt.Errorf("moving your existing site into %s: %w", w.sitesDir(), err)
	}

	id := w.lastOpened()
	if id == "" {
		list, err := w.List()
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			id = list[0].ID // most recently edited
		} else if id, err = w.newSiteDir(); err != nil {
			return nil, err
		}
	}
	if err := w.open(id); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Workspace) sitesDir() string { return filepath.Join(w.root, "sites") }

func (w *Workspace) dir(id string) (string, error) {
	if !siteID.MatchString(id) {
		return "", errBadID
	}
	d := filepath.Join(w.sitesDir(), id)
	if _, err := os.Stat(filepath.Join(d, "site.db")); err != nil {
		return "", errBadID
	}
	return d, nil
}

// Before workspaces, the data folder itself held one site. Move it into
// sites/<name>/ so it shows up in the list like any other.
func (w *Workspace) migrateLegacy() error {
	legacy := filepath.Join(w.root, "site.db")
	if _, err := os.Stat(legacy); err != nil {
		return nil
	}
	// Opening and closing checkpoints the write-ahead log into site.db.
	st, err := store.Open(legacy)
	if err != nil {
		return err
	}
	site, err := st.Site()
	st.Close()
	if err != nil {
		return err
	}
	id := w.freeID(store.Slugify(site.Name))
	dest := filepath.Join(w.sitesDir(), id)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"site.db", "site.db-wal", "site.db-shm", "media", "published"} {
		src := filepath.Join(w.root, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, filepath.Join(dest, name)); err != nil {
			return err
		}
	}
	return w.saveState(id)
}

func (w *Workspace) freeID(base string) string {
	if !siteID.MatchString(base) {
		base = "site"
	}
	id := base
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(w.sitesDir(), id)); os.IsNotExist(err) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

func (w *Workspace) newSiteDir() (string, error) {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	id := w.freeID("site-" + hex.EncodeToString(b))
	d := filepath.Join(w.sitesDir(), id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	st, err := store.Open(filepath.Join(d, "site.db"))
	if err != nil {
		return "", err
	}
	return id, st.Close()
}

type state struct {
	Current string `json:"current"`
}

func (w *Workspace) lastOpened() string {
	data, err := os.ReadFile(filepath.Join(w.root, "state.json"))
	if err != nil {
		return ""
	}
	var s state
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	if _, err := w.dir(s.Current); err != nil {
		return ""
	}
	return s.Current
}

func (w *Workspace) saveState(id string) error {
	data, _ := json.Marshal(state{Current: id})
	return os.WriteFile(filepath.Join(w.root, "state.json"), data, 0o644)
}

// open switches to a site. The caller must hold w.mu for writing, or be the
// constructor.
func (w *Workspace) open(id string) error {
	d, err := w.dir(id)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(d, "site.db"))
	if err != nil {
		return err
	}
	c := &siteCtx{id: id, dir: d, st: st, mediaDir: filepath.Join(d, "media"), distDir: filepath.Join(d, "published")}
	c.builder = &build.Builder{Store: st, Renderer: w.r, MediaDir: c.mediaDir, OutDir: c.distDir}
	if w.cur != nil {
		w.cur.st.Close()
	}
	w.cur = c
	return w.saveState(id)
}

func (w *Workspace) Switch(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.id == id {
		return nil
	}
	return w.open(id)
}

// Create makes an empty site and opens it; the editor then runs the setup wizard.
func (w *Workspace) Create() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id, err := w.newSiteDir()
	if err != nil {
		return "", err
	}
	return id, w.open(id)
}

// Trash moves a site out of the list. Nothing is deleted: the folder can be
// moved back into sites/ to restore it.
func (w *Workspace) Trash(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.id == id {
		return errors.New("Switch to another site before removing this one.")
	}
	d, err := w.dir(id)
	if err != nil {
		return err
	}
	// A site that never got past the setup wizard has nothing worth keeping.
	if site, err := readSite(d); err == nil && !site.SetupDone {
		return os.RemoveAll(d)
	}
	trash := filepath.Join(w.root, "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	return os.Rename(d, filepath.Join(trash, id+"-"+time.Now().Format("20060102-150405")))
}

// List returns every site, most recently edited first.
func (w *Workspace) List() ([]SiteInfo, error) {
	entries, err := os.ReadDir(w.sitesDir())
	if err != nil {
		return nil, err
	}
	out := []SiteInfo{}
	for _, e := range entries {
		d, err := w.dir(e.Name())
		if err != nil {
			continue
		}
		info := SiteInfo{ID: e.Name(), UpdatedAt: lastModified(d)}
		var site store.Site
		if w.cur != nil && w.cur.id == info.ID {
			info.Current = true
			site, err = w.cur.st.Site()
		} else {
			site, err = readSite(d)
		}
		if err != nil {
			continue
		}
		info.Name, info.SetupDone = site.Name, site.SetupDone
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func readSite(dir string) (store.Site, error) {
	st, err := store.Open(filepath.Join(dir, "site.db"))
	if err != nil {
		return store.Site{}, err
	}
	defer st.Close()
	return st.Site()
}

// lastModified looks at the database and its write-ahead log, which is where
// recent edits land first.
func lastModified(dir string) time.Time {
	var t time.Time
	for _, f := range []string{"site.db", "site.db-wal"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t
}

func (w *Workspace) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil {
		w.cur.st.Close()
		w.cur = nil
	}
}
