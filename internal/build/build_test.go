package build

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitesmith/internal/render"
	"sitesmith/internal/starter"
	"sitesmith/internal/store"
)

func setup(t *testing.T, kind string) (*store.Store, *Builder) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	k, _ := starter.Find(kind)
	site := store.DefaultSite()
	site.Name = "Test Co"
	if err := starter.Apply(st, k, site); err != nil {
		t.Fatal(err)
	}
	return st, &Builder{Store: st, Renderer: r, MediaDir: filepath.Join(dir, "media"), OutDir: filepath.Join(dir, "out")}
}

func read(t *testing.T, b *Builder, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.OutDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("missing %s: %v", rel, err)
	}
	return string(data)
}

func TestStarterSitesBuild(t *testing.T) {
	for _, k := range starter.Kinds {
		t.Run(k.ID, func(t *testing.T) {
			_, b := setup(t, k.ID)
			rep, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			if rep.Pages == 0 {
				t.Fatal("no pages built")
			}
			if len(rep.Warnings) > 0 {
				t.Errorf("unexpected warnings: %v", rep.Warnings)
			}
			read(t, b, "index.html")
			read(t, b, "404.html")
			read(t, b, "assets/site.css")
		})
	}
}

func TestRelativeLinksAndNav(t *testing.T) {
	_, b := setup(t, "business")
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	home := read(t, b, "index.html")
	for _, want := range []string{`href="services/"`, `href="contact/"`, `href="assets/site.css?v=`, "<h1>"} {
		if !strings.Contains(home, want) {
			t.Errorf("home page missing %q", want)
		}
	}
	sub := read(t, b, "services/first-service/index.html")
	for _, want := range []string{`href="../../contact/"`, `aria-label="Breadcrumb"`, `href="../../assets/site.css`} {
		if !strings.Contains(sub, want) {
			t.Errorf("subpage missing %q", want)
		}
	}
	// The services page lists its children automatically.
	if svc := read(t, b, "services/index.html"); !strings.Contains(svc, `href="../services/first-service/"`) {
		t.Error("subpage list missing child link")
	}
}

func TestRenameCreatesRedirectOnlyAfterPublish(t *testing.T) {
	st, b := setup(t, "business")
	tree, _ := st.Tree()
	var about *store.Page
	for _, p := range tree.Pages {
		if p.Slug == "about" {
			about = p
		}
	}

	// Renaming before the first publish: no redirect, nobody saw the old URL.
	slug := "about-us"
	if _, err := st.UpdatePage(about.ID, store.PageUpdate{Slug: &slug}); err != nil {
		t.Fatal(err)
	}
	rep, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Redirects != 1 { // only /home/ -> /
		t.Fatalf("redirects = %d, want 1", rep.Redirects)
	}

	// Renaming a published page: the old URL forwards to the new one.
	slug = "our-story"
	if _, err := st.UpdatePage(about.ID, store.PageUpdate{Slug: &slug}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	if r := read(t, b, "about-us/index.html"); !strings.Contains(r, `url=../our-story/`) {
		t.Errorf("redirect page wrong: %s", r)
	}
}

func TestDraftHidesBranch(t *testing.T) {
	st, b := setup(t, "business")
	tree, _ := st.Tree()
	var services *store.Page
	for _, p := range tree.Pages {
		if p.Slug == "services" {
			services = p
		}
	}
	off := false
	if _, err := st.UpdatePage(services.ID, store.PageUpdate{Published: &off}); err != nil {
		t.Fatal(err)
	}
	rep, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.OutDir, "services", "first-service", "index.html")); err == nil {
		t.Error("subpage of a draft was published")
	}
	if strings.Contains(read(t, b, "index.html"), `href="services/"`) {
		t.Error("draft page still in the menu")
	}
	// Home links to the draft page, which should be reported.
	if len(rep.Warnings) == 0 {
		t.Error("expected a warning about links to the draft page")
	}
}

func TestZipWithServer(t *testing.T) {
	_, b := setup(t, "blank")
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Zip(b.OutDir, &buf, "site/",
		ExtraFile{Name: "website-server", Data: []byte("bin"), Mode: 0o755},
		ExtraFile{Name: "README.txt", Data: SelfHostReadme("Test", "linux", "website-server"), Mode: 0o644})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]os.FileMode{}
	for _, f := range zr.File {
		modes[f.Name] = f.Mode()
		if f.Modified.Year() < 2000 {
			t.Errorf("%s has no real date: %v", f.Name, f.Modified)
		}
	}
	if modes["website-server"] != 0o755 {
		t.Errorf("server mode = %v, want runnable", modes["website-server"])
	}
	if _, ok := modes["site/index.html"]; !ok {
		t.Errorf("site not under site/: %v", modes)
	}
	if _, ok := modes["index.html"]; ok {
		t.Error("site files also at the top level")
	}
}

func TestSectionStyleClasses(t *testing.T) {
	st, b := setup(t, "blank")
	tree, _ := st.Tree()
	home := tree.Pages[0]
	blocks := json.RawMessage(`[{"id":"h","type":"hero","data":{"heading":"Hi"},"style":{"height":"short","background":"brand"}},
		{"id":"t","type":"text","data":{"html":"<p>x</p>"},"style":{"spaceTop":"xl","width":"narrow"}}]`)
	if _, err := st.UpdatePage(home.ID, store.PageUpdate{Blocks: &blocks}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	html := read(t, b, "index.html")
	for _, want := range []string{`class="b b-hero h-short bg-brand align-center"`, `class="b b-text pt-xl w-narrow"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	if css := read(t, b, "assets/site.css"); !strings.Contains(css, "--contrast-bg:") || !strings.Contains(css, ".b-hero.h-short") {
		t.Error("style tokens or classes missing from site.css")
	}
}

func TestHideTitle(t *testing.T) {
	st, b := setup(t, "business")
	tree, _ := st.Tree()
	var about *store.Page
	for _, p := range tree.Pages {
		if p.Slug == "about" {
			about = p
		}
	}
	hide := true
	if _, err := st.UpdatePage(about.ID, store.PageUpdate{HideTitle: &hide}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	html := read(t, b, "about/index.html")
	if strings.Contains(html, `class="page-head`) {
		t.Error("visible page title still shown")
	}
	if !strings.Contains(html, `<h1 class="sr-only">About</h1>`) {
		t.Error("hidden h1 missing: search engines and screen readers lose the title")
	}
	if !strings.Contains(read(t, b, "contact/index.html"), `class="page-head`) {
		t.Error("other pages should keep their title")
	}
}

func TestHeaderAndFooter(t *testing.T) {
	st, b := setup(t, "business")
	tree, _ := st.Tree()
	var contact *store.Page
	for _, p := range tree.Pages {
		if p.Slug == "contact" {
			contact = p
		}
	}
	site, _ := st.Site()
	site.Header = store.Header{Layout: "centered", Background: "brand", Sticky: false,
		ButtonLabel: "Book now", ButtonLink: fmt.Sprintf("page:%d", contact.ID)}
	site.FooterText = "123 Example St"
	site.Footer = store.Footer{
		Links:      []store.FooterLink{{Label: "Privacy", Link: "example.com/privacy"}, {Label: "Contact", Link: fmt.Sprintf("page:%d", contact.ID)}},
		Social:     map[string]string{"instagram": "instagram.com/me", "email": "me@example.com", "myspace": "nope"},
		Background: "contrast",
	}
	site.Theme.Heading = "large"
	if err := st.SaveSite(site); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	html := read(t, b, "about/index.html")
	for _, want := range []string{
		`<header class="site-header header-centered not-sticky bg-brand">`,
		`<a class="btn header-btn" href="../contact/">Book now</a>`,
		`<footer class="site-footer bg-contrast">`,
		`<a href="https://example.com/privacy">Privacy</a>`,
		`<a href="../contact/">Contact</a>`,
		`href="https://instagram.com/me" aria-label="Instagram"`,
		`href="mailto:me@example.com" aria-label="Email"`,
		`123 Example St`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(html, "&copy;") {
		t.Error("copyright shown although switched off")
	}
	if strings.Contains(html, "myspace") {
		t.Error("unknown social network rendered")
	}
	if !strings.Contains(read(t, b, "assets/site.css"), "--hs: 1.15;") {
		t.Error("heading size token missing")
	}
}

func TestCustomCSSAndHTML(t *testing.T) {
	st, b := setup(t, "blank")
	tree, _ := st.Tree()
	home := tree.Pages[0]
	site, _ := st.Site()
	site.CustomCSS = ".brand { letter-spacing: 0.1em; }"
	st.SaveSite(site)
	blocks := json.RawMessage(`[
		{"id":"sec1","type":"text","data":{"html":"<p>x</p>"},"css":"padding: 4rem;\nh2 { color: red; }\n</style><script>alert(1)</script>"},
		{"id":"emb","type":"html","data":{"code":"<div id=\"widget\"><script>window.w=1</script></div>"}}]`)
	if _, err := st.UpdatePage(home.ID, store.PageUpdate{Blocks: &blocks}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatal(err)
	}
	html := read(t, b, "index.html")
	if !strings.Contains(html, `class="b b-text s-sec1"`) || !strings.Contains(html, ".s-sec1 {") || !strings.Contains(html, "h2 { color: red; }") {
		t.Errorf("section CSS not scoped to its section:\n%s", html)
	}
	if strings.Contains(html, "</style><script>alert(1)") {
		t.Error("section CSS could close the <style> element")
	}
	// Published: the embed code goes out exactly as written.
	if !strings.Contains(html, `<div id="widget"><script>window.w=1</script></div>`) {
		t.Error("custom HTML not published as written")
	}
	if !strings.Contains(read(t, b, "assets/site.css"), ".brand { letter-spacing: 0.1em; }") {
		t.Error("site-wide custom CSS missing from site.css")
	}

	// Preview: the same code runs in a sandboxed frame instead.
	r, _ := render.New()
	s, _ := r.LoadSite(st)
	var buf bytes.Buffer
	p, _ := st.Page(home.ID)
	if err := r.RenderPage(&buf, s, p, render.Preview, nil); err != nil {
		t.Fatal(err)
	}
	prev := buf.String()
	if !strings.Contains(prev, `sandbox="allow-scripts allow-popups allow-forms"`) || strings.Contains(prev, `<div id="widget"><script>`) {
		t.Error("preview runs custom HTML outside the sandbox")
	}
}
