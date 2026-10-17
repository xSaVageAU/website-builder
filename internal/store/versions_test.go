package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func newSite(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	site := DefaultSite()
	site.Name, site.SetupDone = "Test", true
	if err := st.SaveSite(site); err != nil {
		t.Fatal(err)
	}
	return st
}

func titles(t *testing.T, st *Store) []string {
	t.Helper()
	tree, err := st.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range tree.Pages {
		out = append(out, p.Title)
	}
	return out
}

func TestRestoreBringsBackEverything(t *testing.T) {
	st := newSite(t)
	home, _ := st.CreatePage(0, "Home", nil)
	about, _ := st.CreatePage(0, "About", nil)
	team, _ := st.CreatePage(about.ID, "Team", nil)
	m := &Media{Name: "a", Ext: ".jpg", Width: 10, Height: 10, Sizes: []int{10}}
	st.AddMedia(m)

	v, err := st.SaveVersion("saved", "before experiment")
	if err != nil {
		t.Fatal(err)
	}

	// Experiment: rename, delete a branch, add a page, recolour, delete an image.
	title := "Welcome"
	st.UpdatePage(home.ID, PageUpdate{Title: &title})
	st.DeletePage(about.ID)
	st.CreatePage(0, "Shop", nil)
	site, _ := st.Site()
	site.Theme.Primary = "#ff0000"
	st.SaveSite(site)
	st.DeleteMedia(m.ID)

	if err := st.RestoreVersion(v, true); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, st); len(got) != 3 || got[0] != "Home" || got[1] != "About" || got[2] != "Team" {
		t.Fatalf("pages after restore = %v", got)
	}
	if p, _ := st.Page(team.ID); p == nil || p.ParentID != about.ID {
		t.Error("subpage not restored under its parent")
	}
	if site, _ := st.Site(); site.Theme.Primary == "#ff0000" {
		t.Error("design not restored")
	}
	if list, _ := st.ListMedia(); len(list) != 1 {
		t.Error("deleted image not restored")
	}

	// The restore itself can be undone: the state before it was saved.
	vs, _ := st.Versions()
	if vs[0].Kind != "restore" {
		t.Fatalf("newest version = %+v, want the pre-restore snapshot", vs[0])
	}
	if err := st.RestoreVersion(vs[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, st); len(got) != 2 || got[0] != "Welcome" || got[1] != "Shop" {
		t.Errorf("undoing the restore gave %v", got)
	}
}

func TestChangesAndDiscard(t *testing.T) {
	st := newSite(t)
	home, _ := st.CreatePage(0, "Home", nil)
	about, _ := st.CreatePage(0, "About", nil)

	if c, _ := st.Changes(); c.EverPublished {
		t.Error("never published, but EverPublished is true")
	}
	if err := st.DiscardChanges(true); err != ErrNeverPublished {
		t.Errorf("discard before publishing: %v", err)
	}
	if err := st.MarkPublished(); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Changes(); c.Count != 0 {
		t.Fatalf("changes right after publishing = %+v", c)
	}

	blocks := json.RawMessage(`[{"id":"x","type":"divider","data":{}}]`)
	st.UpdatePage(home.ID, PageUpdate{Blocks: &blocks})
	st.DeletePage(about.ID)
	shop, _ := st.CreatePage(0, "Shop", nil)
	c, _ := st.Changes()
	if c.Pages[home.ID] != "changed" || c.Pages[shop.ID] != "new" || c.Deleted != 1 || c.Count != 3 {
		t.Fatalf("changes = %+v", c)
	}

	// Reverting one page only touches that page.
	if _, err := st.RevertPage(home.ID, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Changes(); c.Pages[home.ID] != "" || c.Count != 2 {
		t.Errorf("after reverting home: %+v", c)
	}

	if err := st.DiscardChanges(true); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Changes(); c.Count != 0 {
		t.Errorf("changes after discard = %+v", c)
	}
	if got := titles(t, st); len(got) != 2 || got[1] != "About" {
		t.Errorf("pages after discard = %v", got)
	}
}

func TestAutoVersion(t *testing.T) {
	st := newSite(t)
	home, _ := st.CreatePage(0, "Home", nil)
	count := func() int { vs, _ := st.Versions(); return len(vs) }

	st.AutoVersion(time.Minute)
	if count() != 1 {
		t.Fatalf("first change should snapshot the starting point, have %d", count())
	}
	title := "A"
	st.UpdatePage(home.ID, PageUpdate{Title: &title})
	st.AutoVersion(time.Minute)
	if count() != 1 {
		t.Error("snapshotted again within the interval")
	}
	st.AutoVersion(0)
	if count() != 2 {
		t.Error("no snapshot after the interval despite changes")
	}
	st.AutoVersion(0)
	if count() != 2 {
		t.Error("snapshotted an unchanged site")
	}
}

func TestPruneVersions(t *testing.T) {
	st := newSite(t)
	now := time.Now()
	add := func(kind string, ago time.Duration) {
		st.db.Exec(`INSERT INTO versions (kind, label, created_at, hash, data) VALUES (?, '', ?, '', x'')`,
			kind, now.Add(-ago).UTC().Format(time.RFC3339Nano))
	}
	for i := 0; i < 5; i++ {
		add("auto", time.Duration(i)*time.Hour) // today: all kept
	}
	for i := 0; i < 4; i++ {
		add("auto", 3*24*time.Hour+time.Duration(i)*time.Minute) // same day, 3 days ago: one kept
	}
	add("saved", 3*24*time.Hour) // never pruned
	if err := st.pruneVersions(now); err != nil {
		t.Fatal(err)
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM versions`).Scan(&n)
	if n != 7 {
		t.Errorf("versions after pruning = %d, want 7", n)
	}
}

func TestRestoreOnlySavesWhatIsUnsaved(t *testing.T) {
	st := newSite(t)
	home, _ := st.CreatePage(0, "Home", nil)
	count := func() int { vs, _ := st.Versions(); return len(vs) }
	a, _ := st.SaveVersion("saved", "A")
	title := "B"
	st.UpdatePage(home.ID, PageUpdate{Title: &title})
	b, _ := st.SaveVersion("saved", "B")

	// Everything is already in version B: going back to A adds nothing.
	if cur, _ := st.CurrentVersion(); cur != b {
		t.Fatalf("current version = %d, want %d", cur, b)
	}
	st.RestoreVersion(a, true)
	if count() != 2 {
		t.Errorf("versions = %d, want 2: nothing was unsaved", count())
	}
	if cur, _ := st.CurrentVersion(); cur != a {
		t.Errorf("after going back, current = %d, want %d", cur, a)
	}

	// Unsaved edit, user chose "go back without saving": nothing kept.
	title = "C"
	st.UpdatePage(home.ID, PageUpdate{Title: &title})
	if cur, _ := st.CurrentVersion(); cur != 0 {
		t.Error("an unsaved edit still matches a version")
	}
	st.RestoreVersion(b, false)
	if count() != 2 {
		t.Errorf("versions = %d, want 2 after going back without saving", count())
	}

	// Unsaved edit, user chose "save and go back": kept once.
	title = "D"
	st.UpdatePage(home.ID, PageUpdate{Title: &title})
	st.RestoreVersion(a, true)
	if count() != 3 {
		t.Errorf("versions = %d, want 3 after saving first", count())
	}
}
