package server

import (
	"os"
	"path/filepath"
	"testing"

	"sitesmith/internal/render"
	"sitesmith/internal/store"
)

func TestWorkspace(t *testing.T) {
	root := t.TempDir()
	r, err := render.New()
	if err != nil {
		t.Fatal(err)
	}

	// An old single-site data folder: site.db plus media/ at the top level.
	st, err := store.Open(filepath.Join(root, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	site := store.DefaultSite()
	site.Name, site.SetupDone = "Savs Workshop", true
	if err := st.SaveSite(site); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePage(0, "Home", nil); err != nil {
		t.Fatal(err)
	}
	st.Close()
	os.MkdirAll(filepath.Join(root, "media"), 0o755)
	os.WriteFile(filepath.Join(root, "media", "photo.jpg"), []byte("x"), 0o644)

	ws, err := OpenWorkspace(root, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "site.db")); !os.IsNotExist(err) {
		t.Error("legacy site.db still at the top level")
	}
	if _, err := os.Stat(filepath.Join(root, "sites", "savs-workshop", "media", "photo.jpg")); err != nil {
		t.Error("media not moved with the site")
	}
	if ws.cur.id != "savs-workshop" {
		t.Fatalf("open site = %q", ws.cur.id)
	}
	if tree, _ := ws.cur.st.Tree(); len(tree.Pages) != 1 {
		t.Error("pages lost in migration")
	}

	// New site becomes the open one; the old one is still listed.
	newID, err := ws.Create()
	if err != nil {
		t.Fatal(err)
	}
	list, _ := ws.List()
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	for _, s := range list {
		if s.Current != (s.ID == newID) {
			t.Errorf("current flag wrong: %+v", s)
		}
	}

	if err := ws.Trash(newID); err == nil {
		t.Error("trashing the open site should fail")
	}
	if err := ws.Switch("savs-workshop"); err != nil {
		t.Fatal(err)
	}
	if err := ws.Trash(newID); err != nil {
		t.Fatal(err)
	}
	if list, _ := ws.List(); len(list) != 1 {
		t.Errorf("trashed site still listed: %+v", list)
	}
	if err := ws.Switch("../../etc"); err == nil {
		t.Error("path traversal accepted")
	}
	ws.Close()

	// Reopening remembers the last site.
	ws, err = OpenWorkspace(root, r)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if ws.cur.id != "savs-workshop" {
		t.Errorf("reopened site = %q", ws.cur.id)
	}
}
