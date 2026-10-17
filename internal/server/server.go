// Package server is the local editor: a JSON API, the admin UI, a live
// preview and the last published build.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"sitesmith/internal/blocks"
	"sitesmith/internal/build"
	"sitesmith/internal/media"
	"sitesmith/internal/render"
	"sitesmith/internal/serverbin"
	"sitesmith/internal/starter"
	"sitesmith/internal/store"
)

type Server struct {
	ws      *Workspace
	r       *render.Renderer
	admin   fs.FS
	buildMu sync.Mutex
	autoMu  sync.Mutex // one auto-version check at a time
}

func New(ws *Workspace, r *render.Renderer, admin fs.FS) *Server {
	return &Server{ws: ws, r: r, admin: admin}
}

// c is the open site. Only valid inside a handler wrapped by onSite.
func (s *Server) c() *siteCtx { return s.ws.cur }

// onSite holds the workspace's read lock for the whole request, so switching
// sites waits for in-flight requests instead of closing their database.
func (s *Server) onSite(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.ws.mu.RLock()
		defer s.ws.mu.RUnlock()
		h(w, r)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	site := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, s.onSite(h)) }
	// edit is for requests that change content: the site is snapshotted first
	// when the last version is getting old, so any change can be undone.
	edit := func(pattern string, h http.HandlerFunc) {
		site(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.autoMu.Lock()
			err := s.c().st.AutoVersion(autoVersionEvery)
			s.autoMu.Unlock()
			if err != nil {
				log.Printf("auto version: %v", err)
			}
			h(w, r)
		})
	}

	mux.Handle("GET /{$}", http.HandlerFunc(s.adminIndex))
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServerFS(s.admin)))
	mux.HandleFunc("GET /api/schema", s.schema)

	mux.HandleFunc("GET /api/sites", s.onSite(s.listSites))
	mux.HandleFunc("POST /api/sites", s.createSite)
	mux.HandleFunc("POST /api/sites/{id}/open", s.openSite)
	mux.HandleFunc("DELETE /api/sites/{id}", s.trashSite)

	site("GET /api/site", s.getSite)
	edit("PUT /api/site", s.putSite)
	site("POST /api/setup", s.setup)

	site("GET /api/pages", s.listPages)
	edit("POST /api/pages", s.createPage)
	site("GET /api/pages/{id}", s.getPage)
	edit("PUT /api/pages/{id}", s.updatePage)
	edit("DELETE /api/pages/{id}", s.deletePage)
	edit("POST /api/pages/{id}/move", s.movePage)
	edit("POST /api/pages/{id}/duplicate", s.duplicatePage)
	site("POST /api/pages/{id}/discard", s.discardPage)

	site("GET /api/versions", s.versions)
	site("POST /api/versions", s.saveVersion)
	site("POST /api/versions/{id}/restore", s.restoreVersion)
	site("POST /api/discard", s.discardChanges)

	site("GET /api/media", s.listMedia)
	site("POST /api/media", s.uploadMedia)
	edit("DELETE /api/media/{id}", s.deleteMedia)

	site("POST /api/publish", s.publish)
	site("GET /api/export", s.export)
	site("POST /api/open-folder", s.openFolder)

	site("GET /preview/", s.preview)
	site("GET /site/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/site", http.FileServer(http.Dir(s.c().distDir))).ServeHTTP(w, r)
	})

	return guard(mux)
}

// ---------- sites ----------

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	list, err := s.ws.List()
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	id, err := s.ws.Create()
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Server) openSite(w http.ResponseWriter, r *http.Request) {
	if err := s.ws.Switch(r.PathValue("id")); err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) trashSite(w http.ResponseWriter, r *http.Request) {
	if err := s.ws.Trash(r.PathValue("id")); err != nil {
		if errors.Is(err, errBadID) {
			fail(w, 404, "Not found.")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// guard keeps the editor private to this machine: it rejects requests for
// other host names (DNS rebinding) and cross-site writes (CSRF), since there
// is no login.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "[::1]" && host != "::1" {
			http.Error(w, "The editor only accepts connections from this computer.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				http.Error(w, "Cross-site request blocked.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) failErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, errBadID) {
		fail(w, http.StatusNotFound, "Not found.")
		return
	}
	log.Printf("error: %v", err)
	fail(w, http.StatusInternalServerError, err.Error())
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// ---------- admin ----------

func (s *Server) adminIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.admin, "index.html")
	if err != nil {
		s.failErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"blocks":      blocks.Types,
		"styleFields": blocks.StyleFields,
		"starters":    starter.Kinds,
		"fonts":       store.FontOptions,
		// Platforms a download can include a website server for.
		"servers":       serverbin.List(),
		"defaultServer": serverbin.Default(),
	})
}

// ---------- site ----------

func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	site, err := s.c().st.Site()
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, site)
}

func (s *Server) putSite(w http.ResponseWriter, r *http.Request) {
	var site store.Site
	if err := readJSON(r, &site); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	cur, err := s.c().st.Site()
	if err != nil {
		s.failErr(w, err)
		return
	}
	site.SetupDone = cur.SetupDone || site.SetupDone
	if err := s.c().st.SaveSite(site); err != nil {
		s.failErr(w, err)
		return
	}
	s.getSite(w, r)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Primary string `json:"primary"`
		Fonts   string `json:"fonts"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	cur, err := s.c().st.Site()
	if err != nil {
		s.failErr(w, err)
		return
	}
	if cur.SetupDone {
		fail(w, 409, "This site is already set up.")
		return
	}
	k, ok := starter.Find(req.Kind)
	if !ok {
		k, _ = starter.Find("blank")
	}
	site := store.DefaultSite()
	site.Name = req.Name
	site.Normalize()
	site.Theme.Primary = k.Primary
	site.Theme.Fonts = k.Fonts
	if req.Primary != "" {
		site.Theme.Primary = req.Primary
	}
	if req.Fonts != "" {
		site.Theme.Fonts = req.Fonts
	}
	if err := starter.Apply(s.c().st, k, site); err != nil {
		s.failErr(w, err)
		return
	}
	s.getSite(w, r)
}

// ---------- pages ----------

type pageItem struct {
	*store.Page
	Live   bool   `json:"live"`
	IsHome bool   `json:"isHome"`
	Change string `json:"change,omitempty"` // "changed" | "new" since the last publish
}

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	t, err := s.c().st.Tree()
	if err != nil {
		s.failErr(w, err)
		return
	}
	changes, err := s.c().st.Changes()
	if err != nil {
		s.failErr(w, err)
		return
	}
	out := make([]pageItem, 0, len(t.Pages))
	for _, p := range t.Pages {
		cp := *p
		cp.Blocks = nil
		out = append(out, pageItem{Page: &cp, Live: t.IsLive(p.ID), IsHome: p.ID == t.HomeID, Change: changes.Pages[p.ID]})
	}
	writeJSON(w, 200, map[string]any{"pages": out, "homeId": t.HomeID, "changes": changes})
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.c().st.Page(pathID(r, "id"))
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentID int64  `json:"parentId"`
		Title    string `json:"title"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	p, err := s.c().st.CreatePage(req.ParentID, req.Title, nil)
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) updatePage(w http.ResponseWriter, r *http.Request) {
	var u store.PageUpdate
	if err := readJSON(r, &u); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	if u.Blocks != nil {
		clean, err := blocks.Normalize(*u.Blocks)
		if err != nil {
			fail(w, 400, "Invalid page content.")
			return
		}
		u.Blocks = &clean
	}
	p, err := s.c().st.UpdatePage(pathID(r, "id"), u)
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request) {
	if err := s.c().st.DeletePage(pathID(r, "id")); err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) movePage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentID int64 `json:"parentId"`
		Index    int   `json:"index"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	if err := s.c().st.MovePage(pathID(r, "id"), req.ParentID, req.Index); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.failErr(w, err)
			return
		}
		fail(w, 400, err.Error())
		return
	}
	s.listPages(w, r)
}

func (s *Server) duplicatePage(w http.ResponseWriter, r *http.Request) {
	t, err := s.c().st.Tree()
	if err != nil {
		s.failErr(w, err)
		return
	}
	src := t.ByID[pathID(r, "id")]
	if src == nil {
		fail(w, 404, "Not found.")
		return
	}
	p, err := s.c().st.CreatePage(src.ParentID, src.Title+" (copy)", src.Blocks)
	if err != nil {
		s.failErr(w, err)
		return
	}
	idx := 0
	for i, sib := range t.Children[src.ParentID] {
		if sib.ID == src.ID {
			idx = i + 1
		}
	}
	if err := s.c().st.MovePage(p.ID, src.ParentID, idx); err != nil {
		s.failErr(w, err)
		return
	}
	desc, menu, pub := src.Description, src.ShowInMenu, false
	p, err = s.c().st.UpdatePage(p.ID, store.PageUpdate{Description: &desc, ShowInMenu: &menu, Published: &pub})
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) discardPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.c().st.RevertPage(pathID(r, "id"), saveFirst(r))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.failErr(w, err)
			return
		}
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// ---------- versions ----------

// Snapshot often enough that any moment is within a few minutes of a version.
const autoVersionEvery = 5 * time.Minute

func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	list, err := s.c().st.Versions()
	if err != nil {
		s.failErr(w, err)
		return
	}
	changes, err := s.c().st.Changes()
	if err != nil {
		s.failErr(w, err)
		return
	}
	cur, err := s.c().st.CurrentVersion()
	if err != nil {
		s.failErr(w, err)
		return
	}
	// current is the version identical to the site right now (0 = unsaved
	// changes), so the editor only asks "save first?" when it matters.
	writeJSON(w, 200, map[string]any{"versions": list, "changes": changes, "current": cur})
}

// saveFirst reads {"save": bool} from a go-back request. It defaults to true:
// keeping the current state is the safe choice if the editor didn't say.
func saveFirst(r *http.Request) bool {
	var req struct {
		Save *bool `json:"save"`
	}
	if readJSON(r, &req) != nil || req.Save == nil {
		return true
	}
	return *req.Save
}

func (s *Server) saveVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	label := strings.TrimSpace(req.Label)
	if len(label) > 100 {
		label = label[:100]
	}
	id, err := s.c().st.SaveVersion("saved", label)
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]int64{"id": id})
}

func (s *Server) restoreVersion(w http.ResponseWriter, r *http.Request) {
	if err := s.c().st.RestoreVersion(pathID(r, "id"), saveFirst(r)); err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) discardChanges(w http.ResponseWriter, r *http.Request) {
	if err := s.c().st.DiscardChanges(saveFirst(r)); err != nil {
		if errors.Is(err, store.ErrNeverPublished) {
			fail(w, 400, "There's nothing to go back to yet: the site hasn't been published.")
			return
		}
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- media ----------

func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	list, err := s.c().st.ListMedia()
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10*media.MaxUpload)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail(w, 400, "Upload too large or invalid.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	added := []*store.Media{}
	problems := []string{}
	for _, fh := range r.MultipartForm.File["files"] {
		if fh.Size > media.MaxUpload {
			problems = append(problems, fh.Filename+": larger than 30 MB")
			continue
		}
		f, err := fh.Open()
		if err != nil {
			problems = append(problems, fh.Filename+": "+err.Error())
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			problems = append(problems, fh.Filename+": "+err.Error())
			continue
		}
		m, err := media.Process(s.c().mediaDir, data, fh.Filename)
		if err != nil {
			problems = append(problems, fh.Filename+": "+err.Error())
			continue
		}
		if err := s.c().st.AddMedia(m); err != nil {
			media.Remove(s.c().mediaDir, m)
			problems = append(problems, fh.Filename+": "+err.Error())
			continue
		}
		added = append(added, m)
	}
	writeJSON(w, 200, map[string]any{"added": added, "problems": problems})
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if _, err := s.c().st.Media(id); err != nil {
		s.failErr(w, err)
		return
	}
	if r.URL.Query().Get("force") != "1" {
		used, err := s.mediaUsage(id)
		if err != nil {
			s.failErr(w, err)
			return
		}
		if len(used) > 0 {
			writeJSON(w, 409, map[string]any{"error": "This image is in use.", "usedOn": used})
			return
		}
	}
	// Files stay on disk: an older version may still use this image.
	if err := s.c().st.DeleteMedia(id); err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// mediaUsage lists where an image appears, so people don't delete a photo
// that's on a live page without knowing.
func (s *Server) mediaUsage(id int64) ([]string, error) {
	site, err := s.c().st.Site()
	if err != nil {
		return nil, err
	}
	var used []string
	if site.Logo == id {
		used = append(used, "Site logo")
	}
	t, err := s.c().st.Tree()
	if err != nil {
		return nil, err
	}
	for _, p := range t.Pages {
		for _, b := range blocks.Parse(p.Blocks) {
			bt, _ := blocks.Lookup(b.Type)
			if usesImage(bt.Fields, b.Data, id) {
				used = append(used, p.Title)
				break
			}
		}
	}
	return used, nil
}

func usesImage(fields []blocks.Field, data map[string]any, id int64) bool {
	for _, f := range fields {
		switch f.Kind {
		case "image":
			if n, _ := data[f.Key].(float64); int64(n) == id {
				return true
			}
		case "list":
			items, _ := data[f.Key].([]any)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok && usesImage(f.Fields, m, id) {
					return true
				}
			}
		}
	}
	return false
}

// ---------- publishing ----------

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	rep, err := s.build()
	if err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, rep)
}

// build writes the site out and records that content as the live version,
// which is what "unpublished changes" and "Discard changes" compare against.
func (s *Server) build() (*build.Report, error) {
	rep, err := s.c().builder.Build()
	if err != nil {
		return nil, err
	}
	return rep, s.c().st.MarkPublished()
}

// export downloads the site as a ZIP. With ?server=<os-arch> it also
// includes the website server for that platform, with the site in a "site"
// folder next to it, so the download can host itself.
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	var extras []build.ExtraFile
	prefix, suffix := "", ""
	if id := r.URL.Query().Get("server"); id != "" {
		t, bin, err := serverbin.Binary(id)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		name := serverbin.FileName(t)
		site, _ := s.c().st.Site()
		extras = []build.ExtraFile{
			{Name: name, Data: bin, Mode: 0o755},
			{Name: "README.txt", Data: build.SelfHostReadme(site.Name, t.OS, name), Mode: 0o644},
		}
		prefix, suffix = "site/", "-"+t.ID
	}

	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if _, err := s.build(); err != nil {
		s.failErr(w, err)
		return
	}
	site, _ := s.c().st.Site()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s.zip"`, store.Slugify(site.Name), suffix))
	if err := build.Zip(s.c().distDir, w, prefix, extras...); err != nil {
		log.Printf("zip: %v", err)
	}
}

func (s *Server) openFolder(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(s.c().distDir); err != nil {
		fail(w, 400, "Publish the site first.")
		return
	}
	abs, _ := filepath.Abs(s.c().distDir)
	if err := OpenPath(abs); err != nil {
		s.failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// OpenPath opens a folder or URL with the operating system's default app.
func OpenPath(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if strings.HasPrefix(target, "http") {
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
		} else {
			cmd = exec.Command("explorer", target)
		}
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// ---------- preview ----------

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/preview")
	switch {
	case path == "/assets/site.css":
		site, err := s.c().st.Site()
		if err != nil {
			s.failErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, s.r.CSS(site.Theme))
		return
	case strings.HasPrefix(path, "/media/"):
		http.StripPrefix("/preview/media", http.FileServer(http.Dir(s.c().mediaDir))).ServeHTTP(w, r)
		return
	}
	if !strings.HasSuffix(path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
		return
	}
	site, err := s.r.LoadSite(s.c().st)
	if err != nil {
		s.failErr(w, err)
		return
	}
	for _, p := range site.Tree.Pages {
		if p.Path == path {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if err := s.r.RenderPage(w, site, p, render.Preview, nil); err != nil {
				log.Printf("preview: %v", err)
			}
			return
		}
	}
	http.Error(w, "No page lives at this address.", http.StatusNotFound)
}
