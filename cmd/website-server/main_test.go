package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("index.html", "<h1>home</h1>")
	write("about/index.html", "<h1>about</h1>")
	write("404.html", "<h1>lost</h1>")
	write("media/cat.jpg", "jpeg")
	write("assets/site.css", "body{}")
	write(".secret", "nope")
	write("empty/readme.txt", "no index here")
	os.WriteFile(filepath.Join(filepath.Dir(root), "outside.txt"), []byte("outside"), 0o644)

	h := Handler(root)
	get := func(method, url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
		return rec
	}

	cases := []struct {
		method, url string
		status      int
		body        string
	}{
		{"GET", "/", 200, "home"},
		{"GET", "/about/", 200, "about"},
		{"GET", "/about", 301, ""},
		{"GET", "/missing/", 404, "lost"},
		{"GET", "/.secret", 404, "lost"},
		{"GET", "/empty/", 404, "lost"}, // folder without index.html: no listing
		{"GET", "/../outside.txt", 404, "lost"},
		{"GET", "/%2e%2e/outside.txt", 404, "lost"},
		{"POST", "/", 405, ""},
		{"GET", "/media/cat.jpg", 200, "jpeg"},
	}
	for _, c := range cases {
		rec := get(c.method, c.url)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s %s = %d %q, want %d containing %q", c.method, c.url, rec.Code, rec.Body.String(), c.status, c.body)
		}
	}

	if loc := get("GET", "/about?x=1").Header().Get("Location"); loc != "/about/?x=1" {
		t.Errorf("redirect Location = %q", loc)
	}
	if cc := get("GET", "/media/cat.jpg").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("media Cache-Control = %q", cc)
	}
	if cc := get("GET", "/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("page Cache-Control = %q", cc)
	}
	if ct := get("GET", "/assets/site.css").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("css Content-Type = %q", ct)
	}
}

func TestSiteDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := siteDir(dir); err == nil {
		t.Error("accepted a folder with no index.html")
	}
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	if got, err := siteDir(dir); err != nil || got == "" {
		t.Errorf("siteDir = %q, %v", got, err)
	}
}
