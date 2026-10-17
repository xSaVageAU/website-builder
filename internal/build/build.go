// Package build writes the whole site out as plain static files.
package build

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sitesmith/internal/render"
	"sitesmith/internal/store"
)

type Report struct {
	Pages     int      `json:"pages"`
	Redirects int      `json:"redirects"`
	Media     int      `json:"media"`
	Warnings  []string `json:"warnings"`
	Duration  string   `json:"duration"`
	OutputDir string   `json:"outputDir"`
	BuiltAt   string   `json:"builtAt"`
}

type Builder struct {
	Store    *store.Store
	Renderer *render.Renderer
	MediaDir string
	OutDir   string
}

// Build renders into a fresh temporary folder and swaps it in at the end, so
// a failed build never leaves a half-written site behind.
func (b *Builder) Build() (*Report, error) {
	start := time.Now()
	site, err := b.Renderer.LoadSite(b.Store)
	if err != nil {
		return nil, err
	}
	tmp := b.OutDir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	rep := &Report{Warnings: []string{}}

	write := func(rel string, data []byte) error {
		p := filepath.Join(tmp, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, data, 0o644)
	}

	// Pages.
	live := map[string]int64{}
	for _, p := range site.Tree.Pages {
		if !site.Tree.IsLive(p.ID) {
			continue
		}
		var buf bytes.Buffer
		if err := b.Renderer.RenderPage(&buf, site, p, render.Relative, &rep.Warnings); err != nil {
			return nil, fmt.Errorf("render %q: %w", p.Title, err)
		}
		if err := write(p.Path+"index.html", buf.Bytes()); err != nil {
			return nil, err
		}
		live[p.Path] = p.ID
		rep.Pages++
	}
	if rep.Pages == 0 {
		rep.Warnings = append(rep.Warnings, "There are no published pages yet, so the site is empty.")
	}

	// Redirects for URLs that changed since the last publish, plus the home
	// page's own slug (its subpages live under it).
	if err := b.Store.RecordPublish(live); err != nil {
		return nil, err
	}
	redirects, err := b.Store.Redirects()
	if err != nil {
		return nil, err
	}
	if home := site.Tree.ByID[site.Tree.HomeID]; home != nil {
		redirects["/"+home.Slug+"/"] = home.ID
	}
	for from, id := range redirects {
		target := site.Tree.ByID[id]
		if _, taken := live[from]; taken || target == nil || !site.Tree.IsLive(id) || from == target.Path {
			continue
		}
		if err := write(from+"index.html", redirectPage(from, target.Path)); err != nil {
			return nil, err
		}
		rep.Redirects++
	}

	// 404, stylesheet, media.
	var nf bytes.Buffer
	if err := b.Renderer.Render404(&nf, site); err != nil {
		return nil, err
	}
	if err := write("404.html", nf.Bytes()); err != nil {
		return nil, err
	}
	if err := write("assets/site.css", []byte(site.CSS)); err != nil {
		return nil, err
	}
	for _, m := range site.Media {
		for _, f := range m.Files() {
			if err := copyFile(filepath.Join(b.MediaDir, f), filepath.Join(tmp, "media", f)); err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("Couldn't copy image %s: %v", m.OriginalName, err))
			}
		}
		rep.Media++
	}

	// Search engines: a sitemap needs absolute URLs, so only when the address is known.
	robots := "User-agent: *\nAllow: /\n"
	if site.Info.BaseURL != "" {
		paths := make([]string, 0, len(live))
		for p := range live {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		var sm bytes.Buffer
		sm.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
		for _, p := range paths {
			fmt.Fprintf(&sm, "  <url><loc>%s</loc><lastmod>%s</lastmod></url>\n",
				template.HTMLEscapeString(site.Info.BaseURL+strings.TrimPrefix(p, "/")),
				site.Tree.ByID[live[p]].UpdatedAt.Format("2006-01-02"))
		}
		sm.WriteString("</urlset>\n")
		if err := write("sitemap.xml", sm.Bytes()); err != nil {
			return nil, err
		}
		robots += "Sitemap: " + site.Info.BaseURL + "sitemap.xml\n"
	}
	if err := write("robots.txt", []byte(robots)); err != nil {
		return nil, err
	}

	if err := os.RemoveAll(b.OutDir); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, b.OutDir); err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(b.OutDir)
	rep.OutputDir = abs
	rep.Duration = time.Since(start).Round(time.Millisecond).String()
	rep.BuiltAt = time.Now().Format(time.RFC3339)
	rep.Warnings = dedupe(rep.Warnings)
	return rep, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// redirectPage is a static stand-in for a 301, which plain file hosting can't send.
func redirectPage(from, to string) []byte {
	depth := strings.Count(from, "/") - 1
	rel := strings.Repeat("../", max(depth, 0)) + strings.TrimPrefix(to, "/")
	if rel == "" {
		rel = "./"
	}
	h := template.HTMLEscapeString(rel)
	return []byte(`<!doctype html><html><head><meta charset="utf-8"><title>Moved</title>` +
		`<meta http-equiv="refresh" content="0; url=` + h + `"><link rel="canonical" href="` + h + `">` +
		`<meta name="robots" content="noindex"></head><body><p>This page has moved to <a href="` + h + `">a new address</a>.</p></body></html>`)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Zip streams the built folder as a zip archive.
// ExtraFile is something added to an export next to the site, such as the
// self-hosting server and its instructions.
type ExtraFile struct {
	Name string
	Data []byte
	Mode fs.FileMode // 0755 for programs, so they stay runnable on Mac and Linux
}

// Zip streams the built folder as a zip archive. Site files go under prefix
// (e.g. "site/"), extras at the top level.
func Zip(dir string, w io.Writer, prefix string, extras ...ExtraFile) error {
	zw := zip.NewWriter(w)
	for _, e := range extras {
		hdr := &zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: time.Now()}
		hdr.SetMode(e.Mode)
		f, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := f.Write(e.Data); err != nil {
			return err
		}
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info) // keeps the real modified date
		if err != nil {
			return err
		}
		hdr.Name, hdr.Method = prefix+filepath.ToSlash(rel), zip.Deflate
		hdr.SetMode(0o644)
		f, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(f, in)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}
