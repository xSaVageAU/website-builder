// Package render turns pages into HTML using the built-in theme. The same code
// serves the live editor preview and the static build; only URL generation
// differs between the two.
package render

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sitesmith/internal/blocks"
	"sitesmith/internal/store"
)

//go:embed theme/*
var themeFS embed.FS

type Renderer struct {
	tmpl    *template.Template
	baseCSS string
}

func New() (*Renderer, error) {
	t, err := template.ParseFS(themeFS, "theme/templates.html")
	if err != nil {
		return nil, err
	}
	css, err := themeFS.ReadFile("theme/site.css")
	if err != nil {
		return nil, err
	}
	return &Renderer{tmpl: t, baseCSS: string(css)}, nil
}

// Mode decides how URLs are written.
type Mode int

const (
	// Preview links point into the editor's /preview/ route and drafts are visible.
	Preview Mode = iota
	// Relative links work wherever the folder is uploaded, including sub-paths
	// like GitHub Pages project sites.
	Relative
	// Absolute links from the site root; used for 404.html, which hosts serve
	// at arbitrary URLs.
	Absolute
)

// Site is everything needed to render any page: loaded once per build.
type Site struct {
	Info  store.Site
	Tree  *store.Tree
	Media map[int64]*store.Media
	CSS   string // generated stylesheet
	// CSSVersion busts caches when the design changes.
	CSSVersion string
}

func (r *Renderer) LoadSite(st *store.Store) (*Site, error) {
	info, err := st.Site()
	if err != nil {
		return nil, err
	}
	tree, err := st.Tree()
	if err != nil {
		return nil, err
	}
	list, err := st.ListMedia()
	if err != nil {
		return nil, err
	}
	media := map[int64]*store.Media{}
	for _, m := range list {
		media[m.ID] = m
	}
	css := r.CSS(info.Theme)
	sum := sha256.Sum256([]byte(css))
	return &Site{Info: info, Tree: tree, Media: media, CSS: css, CSSVersion: hex.EncodeToString(sum[:4])}, nil
}

type NavItem struct {
	Title    string
	Href     string
	Active   bool // the current page or one of its ancestors
	Current  bool
	Children []NavItem
}

type Crumb struct {
	Title string
	Href  string
}

type Image struct {
	Src    string
	Srcset string
	Full   string
	Width  int
	Height int
}

type BlockView struct {
	ID      string
	Type    string
	D       map[string]any
	Preview bool
	First   bool   // first block on a page without an automatic title: its heading becomes the h1
	Class   string // from the section's Style choices, e.g. "pt-l bg-brand"
}

type PageView struct {
	Lang        string
	SiteName    string
	DocTitle    string
	Title       string
	Description string
	Canonical   string
	OGImage     string
	CSSHref     string
	HomeHref    string
	Logo        *Image
	Nav         []NavItem
	Crumbs      []Crumb
	ShowTitle   bool
	TitleHidden bool // page title switched off: only a hidden <h1> for search engines and screen readers
	Blocks      []BlockView
	FooterText  string
	Year        int
	IsHome      bool
	Preview     bool
	NoIndex     bool
}

// pageCtx resolves links and images relative to the page being rendered.
type pageCtx struct {
	site     *Site
	mode     Mode
	from     string // path of the page being rendered
	base     string // URL path prefix for Absolute mode, e.g. "/" or "/my-repo/"
	warnings *[]string
	pageName string
}

func (c *pageCtx) url(target string) string {
	switch c.mode {
	case Preview:
		return "/preview" + target
	case Absolute:
		return c.base + strings.TrimPrefix(target, "/")
	}
	depth := strings.Count(c.from, "/") - 1
	out := strings.Repeat("../", max(depth, 0)) + strings.TrimPrefix(target, "/")
	if out == "" {
		return "./"
	}
	return out
}

func (c *pageCtx) warn(format string, args ...any) {
	if c.warnings != nil {
		*c.warnings = append(*c.warnings, fmt.Sprintf("%s: %s", c.pageName, fmt.Sprintf(format, args...)))
	}
}

// visible reports whether a page can be linked to in this mode.
func (c *pageCtx) visible(id int64) bool {
	if c.mode == Preview {
		return c.site.Tree.ByID[id] != nil
	}
	return c.site.Tree.IsLive(id)
}

func (c *pageCtx) pageHref(id int64) (string, bool) {
	p := c.site.Tree.ByID[id]
	if p == nil || !c.visible(id) {
		return "", false
	}
	return c.url(p.Path), true
}

func (c *pageCtx) link(v string) template.URL {
	if v == "" {
		return ""
	}
	if id, ok := strings.CutPrefix(v, "page:"); ok {
		n, _ := strconv.ParseInt(id, 10, 64)
		href, ok := c.pageHref(n)
		if !ok {
			c.warn("a link points to a page that was deleted or is a draft")
			return ""
		}
		return template.URL(href)
	}
	// Already validated by blocks.CleanLink.
	return template.URL(v)
}

var richPageLink = regexp.MustCompile(`href="page:(\d+)"`)

func (c *pageCtx) rich(v string) template.HTML {
	v = blocks.SanitizeRich(v)
	v = richPageLink.ReplaceAllStringFunc(v, func(m string) string {
		n, _ := strconv.ParseInt(richPageLink.FindStringSubmatch(m)[1], 10, 64)
		href, ok := c.pageHref(n)
		if !ok {
			c.warn("a text link points to a page that was deleted or is a draft")
			href = "#"
		}
		return `href="` + template.HTMLEscapeString(href) + `"`
	})
	return template.HTML(v)
}

func (c *pageCtx) image(id int64) *Image {
	m := c.site.Media[id]
	if m == nil {
		if id != 0 {
			c.warn("an image is missing from the media library")
		}
		return nil
	}
	img := &Image{Width: m.Width, Height: m.Height, Full: c.url("/media/" + m.File(m.Width))}
	var set []string
	src := m.Width
	for _, w := range m.Sizes {
		set = append(set, fmt.Sprintf("%s %dw", c.url("/media/"+m.File(w)), w))
		if w <= 1600 {
			src = w
		}
	}
	img.Src = c.url("/media/" + m.File(src))
	img.Srcset = strings.Join(set, ", ")
	return img
}

// resolve converts stored field values into what templates need: hrefs,
// image renditions and trusted HTML.
func (c *pageCtx) resolve(fields []blocks.Field, data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for _, f := range fields {
		v := data[f.Key]
		switch f.Kind {
		case "image":
			id, _ := v.(float64)
			if img := c.image(int64(id)); img != nil {
				out[f.Key] = img
			} else {
				out[f.Key] = nil
			}
		case "link":
			s, _ := v.(string)
			out[f.Key] = c.link(s)
		case "rich":
			s, _ := v.(string)
			out[f.Key] = c.rich(s)
		case "list":
			items, _ := v.([]any)
			list := make([]map[string]any, 0, len(items))
			for _, it := range items {
				m, _ := it.(map[string]any)
				list = append(list, c.resolve(f.Fields, m))
			}
			out[f.Key] = list
		default:
			out[f.Key] = v
		}
	}
	return out
}

type subpage struct {
	Title       string
	Href        string
	Description string
}

func (c *pageCtx) blockView(b blocks.Block, page *store.Page) BlockView {
	t, _ := blocks.Lookup(b.Type)
	d := c.resolve(t.Fields, b.Data)
	switch b.Type {
	case "subpages":
		var list []subpage
		for _, child := range c.site.Tree.Children[page.ID] {
			if href, ok := c.pageHref(child.ID); ok {
				list = append(list, subpage{child.Title, href, child.Description})
			}
		}
		d["pages"] = list
	case "video":
		s, _ := d["url"].(string)
		embed := videoEmbed(s)
		if embed == "" && s != "" {
			c.warn("couldn't recognise the video address %q", s)
		}
		d["embed"] = template.URL(embed)
	case "columns":
		if items, _ := d["items"].([]map[string]any); len(items) == 0 {
			d["items"] = []map[string]any{}
		}
	}
	return BlockView{ID: b.ID, Type: b.Type, D: d, Preview: c.mode == Preview, Class: styleClass(b.Style)}
}

// styleClass turns style choices into the classes site.css understands.
func styleClass(style map[string]any) string {
	prefix := map[string]string{"spaceTop": "pt-", "spaceBottom": "pb-", "height": "h-", "background": "bg-", "width": "w-"}
	var out []string
	for _, f := range blocks.StyleFields { // fixed order keeps the HTML stable
		if v, _ := style[f.Key].(string); v != "" {
			out = append(out, prefix[f.Key]+v)
		}
	}
	return strings.Join(out, " ")
}

var (
	ytID    = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	vimeoID = regexp.MustCompile(`^\d{4,12}$`)
)

// videoEmbed turns a YouTube/Vimeo page address into a privacy-friendly embed URL.
func videoEmbed(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "m.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := ""
	switch host {
	case "youtube.com", "youtube-nocookie.com":
		if v := u.Query().Get("v"); v != "" {
			id = v
		} else if len(parts) == 2 && (parts[0] == "embed" || parts[0] == "shorts" || parts[0] == "live") {
			id = parts[1]
		}
	case "youtu.be":
		id = parts[0]
	case "vimeo.com", "player.vimeo.com":
		last := parts[len(parts)-1]
		if vimeoID.MatchString(last) {
			return "https://player.vimeo.com/video/" + last + "?dnt=1"
		}
	}
	if ytID.MatchString(id) {
		return "https://www.youtube-nocookie.com/embed/" + id
	}
	return ""
}

func (c *pageCtx) nav(current *store.Page) []NavItem {
	tree := c.site.Tree
	active := map[int64]bool{current.ID: true}
	for _, a := range tree.Ancestors(current.ID) {
		active[a.ID] = true
	}
	var build func(parent int64, depth int) []NavItem
	build = func(parent int64, depth int) []NavItem {
		var items []NavItem
		for _, p := range tree.Children[parent] {
			// Drafts stay out of the menu even in preview, so the preview
			// matches what will be published.
			if !p.ShowInMenu || !tree.IsLive(p.ID) {
				continue
			}
			it := NavItem{Title: p.Title, Href: c.url(p.Path), Active: active[p.ID], Current: p.ID == current.ID}
			if depth == 0 {
				it.Children = build(p.ID, 1)
			}
			items = append(items, it)
		}
		return items
	}
	return build(0, 0)
}

// RenderPage writes a full HTML page. warnings collects problems worth telling
// the user about (broken links, missing images); it may be nil.
func (r *Renderer) RenderPage(w io.Writer, s *Site, page *store.Page, mode Mode, warnings *[]string) error {
	c := &pageCtx{site: s, mode: mode, from: page.Path, base: s.basePath(), warnings: warnings, pageName: page.Title}
	v := r.pageView(c, page.Title)
	v.Description = page.Description
	v.IsHome = page.ID == s.Tree.HomeID
	if v.IsHome {
		v.DocTitle = s.Info.Name
		if s.Info.Tagline != "" {
			v.DocTitle += " – " + s.Info.Tagline
		}
	}
	if s.Info.BaseURL != "" {
		v.Canonical = s.Info.BaseURL + strings.TrimPrefix(page.Path, "/")
	}
	v.Nav = c.nav(page)

	ancestors := s.Tree.Ancestors(page.ID)
	if len(ancestors) > 0 {
		if home := s.Tree.ByID[s.Tree.HomeID]; home != nil && s.Tree.IsLive(home.ID) {
			v.Crumbs = append(v.Crumbs, Crumb{Title: home.Title, Href: c.url("/")})
		}
		for _, a := range ancestors {
			if a.ID == s.Tree.HomeID {
				continue
			}
			href, _ := c.pageHref(a.ID)
			v.Crumbs = append(v.Crumbs, Crumb{Title: a.Title, Href: href})
		}
		v.Crumbs = append(v.Crumbs, Crumb{Title: page.Title})
	}

	list := blocks.Parse(page.Blocks)
	// A hero at the top supplies the h1 itself; otherwise the page title does,
	// visibly unless the page has its title switched off.
	heroFirst := len(list) > 0 && list[0].Type == "hero"
	v.ShowTitle = !heroFirst && !page.HideTitle
	v.TitleHidden = !heroFirst && page.HideTitle
	for i, b := range list {
		bv := c.blockView(b, page)
		bv.First = i == 0 && heroFirst
		v.Blocks = append(v.Blocks, bv)
		if v.OGImage == "" && s.Info.BaseURL != "" {
			if img, ok := bv.D["image"].(*Image); ok && img != nil {
				v.OGImage = absolutize(s.Info.BaseURL, page.Path, img.Src)
			}
		}
	}
	return r.tmpl.ExecuteTemplate(w, "page", v)
}

// Render404 writes the "page not found" page. It uses absolute links because
// hosts serve it at whatever URL was missing.
func (r *Renderer) Render404(w io.Writer, s *Site) error {
	c := &pageCtx{site: s, mode: Absolute, from: "/", base: s.basePath()}
	v := r.pageView(c, "Page not found")
	v.NoIndex = true
	v.Nav = c.nav(&store.Page{})
	v.ShowTitle = true
	v.Blocks = []BlockView{{Type: "text", D: map[string]any{
		"html": template.HTML(`<p>Sorry, we couldn't find the page you were looking for. It may have moved or no longer exists.</p>`),
	}}, {Type: "button", D: map[string]any{
		"label": "Go to the home page", "link": template.URL(c.url("/")), "style": "primary", "align": "left",
	}}}
	return r.tmpl.ExecuteTemplate(w, "page", v)
}

func (r *Renderer) pageView(c *pageCtx, title string) PageView {
	s := c.site
	v := PageView{
		Lang:       s.Info.Language,
		SiteName:   s.Info.Name,
		Title:      title,
		DocTitle:   title + " · " + s.Info.Name,
		CSSHref:    c.url("/assets/site.css") + "?v=" + s.CSSVersion,
		HomeHref:   c.url("/"),
		Logo:       c.image(s.Info.Logo),
		FooterText: s.Info.FooterText,
		Year:       time.Now().Year(),
		Preview:    c.mode == Preview,
	}
	return v
}

func (s *Site) basePath() string {
	if s.Info.BaseURL == "" {
		return "/"
	}
	u, err := url.Parse(s.Info.BaseURL)
	if err != nil || u.Path == "" {
		return "/"
	}
	return strings.TrimRight(u.Path, "/") + "/"
}

func absolutize(baseURL, pagePath, rel string) string {
	b, err := url.Parse(baseURL + strings.TrimPrefix(pagePath, "/"))
	if err != nil {
		return ""
	}
	r, err := url.Parse(rel)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

// ---------- Stylesheet ----------

var fontStacks = map[string][2]string{ // heading, body
	"modern": {
		`system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`,
		`system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`,
	},
	"classic": {
		`Charter, "Bitstream Charter", "Sitka Text", Cambria, Georgia, serif`,
		`Charter, "Bitstream Charter", "Sitka Text", Cambria, Georgia, serif`,
	},
	"friendly": {
		`ui-rounded, "Hiragino Maru Gothic ProN", Quicksand, Comfortaa, Manjari, "Arial Rounded MT", "Arial Rounded MT Bold", Calibri, sans-serif`,
		`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
	},
	"editorial": {
		`Didot, "Bodoni MT", "Noto Serif Display", "URW Palladio L", P052, Sylfaen, Georgia, serif`,
		`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
	},
	"technical": {
		`ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, Consolas, "DejaVu Sans Mono", monospace`,
		`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
	},
}

// CSS builds the stylesheet for a theme: tokens first, then the base styles.
func (r *Renderer) CSS(t store.Theme) string {
	fonts := fontStacks[t.Fonts]
	radius := map[string][3]string{ // card, small, button
		"rounded": {"16px", "10px", "999px"},
		"soft":    {"8px", "6px", "8px"},
		"sharp":   {"0", "0", "0"},
	}[t.Shape]
	space := map[string]string{"airy": "5.5rem", "compact": "3rem"}[t.Density]

	palette := map[string]string{
		"--bg": "#ffffff", "--surface": "#f4f5f7", "--text": "#1a1d23", "--muted": "#5a6170", "--border": "#e2e5ea",
	}
	link := darkenTo(t.Primary, 4.5, "#ffffff")
	soft := "color-mix(in srgb, var(--primary) 9%, var(--bg))"
	if t.Mode == "dark" {
		palette = map[string]string{
			"--bg": "#111318", "--surface": "#1a1d24", "--text": "#e9ebf0", "--muted": "#a3a9b6", "--border": "#2b303b",
		}
		link = lightenTo(t.Primary, 4.5, "#111318")
		soft = "color-mix(in srgb, var(--primary) 18%, var(--bg))"
	}

	var b bytes.Buffer
	b.WriteString(":root {\n")
	for _, k := range []string{"--bg", "--surface", "--text", "--muted", "--border"} {
		fmt.Fprintf(&b, "  %s: %s;\n", k, palette[k])
	}
	fmt.Fprintf(&b, "  --primary: %s;\n  --on-primary: %s;\n  --primary-soft: %s;\n  --link: %s;\n",
		t.Primary, onColor(t.Primary), soft, link)

	// "Contrast" section background: a dark band on a light site, a light
	// band on a dark one. Sections swap the palette variables for these.
	cbg, ctext, cmuted, csurface, cborder := "#16181d", "#f1f3f5", "#a9afba", "#22262e", "#343a45"
	clink := lightenTo(t.Primary, 4.5, cbg)
	if t.Mode == "dark" {
		cbg, ctext, cmuted, csurface, cborder = "#f4f5f7", "#16181d", "#5a6170", "#ffffff", "#dfe3e8"
		clink = darkenTo(t.Primary, 4.5, cbg)
	}
	fmt.Fprintf(&b, "  --page-bg: %s;\n  --contrast-bg: %s;\n  --contrast-text: %s;\n  --contrast-muted: %s;\n  --contrast-surface: %s;\n  --contrast-border: %s;\n  --contrast-link: %s;\n",
		palette["--bg"], cbg, ctext, cmuted, csurface, cborder, clink)
	fmt.Fprintf(&b, "  --font-head: %s;\n  --font-body: %s;\n", fonts[0], fonts[1])
	fmt.Fprintf(&b, "  --radius: %s;\n  --radius-sm: %s;\n  --radius-btn: %s;\n  --space: %s;\n", radius[0], radius[1], radius[2], space)
	if t.Mode == "dark" {
		b.WriteString("  color-scheme: dark;\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(r.baseCSS)
	return b.String()
}

func parseHex(h string) (r, g, b float64) {
	n, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	return float64(n >> 16 & 0xff), float64(n >> 8 & 0xff), float64(n & 0xff)
}

func toHex(r, g, b float64) string {
	c := func(v float64) int { return int(math.Round(math.Max(0, math.Min(255, v)))) }
	return fmt.Sprintf("#%02x%02x%02x", c(r), c(g), c(b))
}

func luminance(h string) float64 {
	r, g, b := parseHex(h)
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// onColor picks white or near-black text for a coloured background.
func onColor(bg string) string {
	if contrast(bg, "#ffffff") >= contrast(bg, "#111111") {
		return "#ffffff"
	}
	return "#111111"
}

// darkenTo / lightenTo mix the colour towards black/white until text in it
// is readable on the given background, so any brand colour works for links.
func darkenTo(c string, ratio float64, bg string) string  { return mixUntil(c, 0, ratio, bg) }
func lightenTo(c string, ratio float64, bg string) string { return mixUntil(c, 255, ratio, bg) }

func mixUntil(c string, target, ratio float64, bg string) string {
	r, g, b := parseHex(c)
	for i := 0; i <= 20; i++ {
		f := float64(i) / 20
		out := toHex(r+(target-r)*f, g+(target-g)*f, b+(target-b)*f)
		if contrast(out, bg) >= ratio {
			return out
		}
	}
	return c
}
