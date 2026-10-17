package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"sitesmith/internal/blocks"
)

// Theme is a handful of design tokens. Users pick from small option sets so
// every combination still looks good.
type Theme struct {
	Primary string `json:"primary"` // hex colour, e.g. #3b5bdb
	Fonts   string `json:"fonts"`   // modern | classic | friendly | editorial | technical
	Shape   string `json:"shape"`   // rounded | soft | sharp
	Density string `json:"density"` // airy | compact
	Mode    string `json:"mode"`    // light | dark
	Heading string `json:"heading"` // heading size: small | normal | large
}

// Header is the bar at the top of every page.
type Header struct {
	Layout      string `json:"layout"`     // classic (logo left, menu right) | centered
	Background  string `json:"background"` // "" (plain) | soft | brand | contrast
	Sticky      bool   `json:"sticky"`     // stays at the top while scrolling
	ButtonLabel string `json:"buttonLabel"`
	ButtonLink  string `json:"buttonLink"` // "page:<id>" or a URL, like section links
}

type FooterLink struct {
	Label string `json:"label"`
	Link  string `json:"link"`
}

// Footer is the strip at the bottom of every page. Its text lives in
// Site.FooterText, which predates this struct.
type Footer struct {
	Links         []FooterLink      `json:"links"`
	Social        map[string]string `json:"social"`     // network -> profile link, see SocialNetworks
	Background    string            `json:"background"` // "" (subtle) | plain | soft | brand | contrast
	ShowCopyright bool              `json:"showCopyright"`
}

// SocialNetworks are the profiles a footer can link to, in display order.
var SocialNetworks = []string{"instagram", "facebook", "x", "linkedin", "youtube", "tiktok", "github", "email"}

type Site struct {
	Name       string `json:"name"`
	Tagline    string `json:"tagline"`
	Logo       int64  `json:"logo"` // media id, 0 = use the site name as text
	HomePageID int64  `json:"homePageId"`
	FooterText string `json:"footerText"`
	BaseURL    string `json:"baseUrl"` // optional; needed for sitemap.xml and absolute 404 links
	Language   string `json:"language"`
	SetupDone  bool   `json:"setupDone"`
	Theme      Theme  `json:"theme"`
	Header     Header `json:"header"`
	Footer     Footer `json:"footer"`
	// CustomCSS is added after the theme's stylesheet (advanced).
	CustomCSS string `json:"customCss"`
}

var (
	FontOptions    = []string{"modern", "classic", "friendly", "editorial", "technical"}
	ShapeOptions   = []string{"rounded", "soft", "sharp"}
	DensityOptions = []string{"airy", "compact"}
	ModeOptions    = []string{"light", "dark"}
	HeadingOptions = []string{"normal", "small", "large"}
	LayoutOptions  = []string{"classic", "centered"}

	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	langCode = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})?$`)
)

func DefaultSite() Site {
	return Site{
		Name:     "My website",
		Language: "en",
		Theme: Theme{
			Primary: "#3b5bdb",
			Fonts:   "modern",
			Shape:   "rounded",
			Density: "airy",
			Mode:    "light",
			Heading: "normal",
		},
		Header: Header{Layout: "classic", Sticky: true},
		Footer: Footer{ShowCopyright: true},
	}
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func oneOf(v string, opts []string) string {
	for _, o := range opts {
		if v == o {
			return v
		}
	}
	return opts[0]
}

// Normalize clamps every field to something the renderer can use, so a bad
// request can never produce a broken site.
func (s *Site) Normalize() {
	d := DefaultSite()
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = d.Name
	}
	s.Tagline = strings.TrimSpace(s.Tagline)
	s.FooterText = strings.TrimSpace(s.FooterText)
	s.BaseURL = strings.TrimSpace(s.BaseURL)
	if s.BaseURL != "" {
		if !strings.Contains(s.BaseURL, "://") {
			s.BaseURL = "https://" + s.BaseURL
		}
		s.BaseURL = strings.TrimRight(s.BaseURL, "/") + "/"
	}
	if !langCode.MatchString(s.Language) {
		s.Language = d.Language
	}
	if !hexColor.MatchString(s.Theme.Primary) {
		s.Theme.Primary = d.Theme.Primary
	}
	s.Theme.Primary = strings.ToLower(s.Theme.Primary)
	s.Theme.Fonts = oneOf(s.Theme.Fonts, FontOptions)
	s.Theme.Shape = oneOf(s.Theme.Shape, ShapeOptions)
	s.Theme.Density = oneOf(s.Theme.Density, DensityOptions)
	s.Theme.Mode = oneOf(s.Theme.Mode, ModeOptions)
	s.Theme.Heading = oneOf(s.Theme.Heading, HeadingOptions)
	s.CustomCSS = clip(s.CustomCSS, blocks.MaxCSS)

	s.Header.Layout = oneOf(s.Header.Layout, LayoutOptions)
	s.Header.Background = oneOf(s.Header.Background, []string{"", "soft", "brand", "contrast"})
	s.Header.ButtonLabel = clip(s.Header.ButtonLabel, 40)
	s.Header.ButtonLink = blocks.CleanLink(s.Header.ButtonLink)

	s.Footer.Background = oneOf(s.Footer.Background, []string{"", "plain", "soft", "brand", "contrast"})
	links := []FooterLink{}
	for _, l := range s.Footer.Links {
		l.Label, l.Link = clip(l.Label, 60), blocks.CleanLink(l.Link)
		if l.Label != "" || l.Link != "" { // keep half-filled rows while someone is typing
			links = append(links, l)
		}
		if len(links) == 12 {
			break
		}
	}
	s.Footer.Links = links
	social := map[string]string{}
	for _, n := range SocialNetworks {
		if v := blocks.CleanLink(s.Footer.Social[n]); v != "" {
			social[n] = v
		}
	}
	s.Footer.Social = social
}

func (s *Store) Site() (Site, error) {
	site := DefaultSite()
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'site'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return site, nil
	}
	if err != nil {
		return site, err
	}
	if err := json.Unmarshal([]byte(raw), &site); err != nil {
		return site, err
	}
	site.Normalize()
	return site, nil
}

func (s *Store) SaveSite(site Site) error {
	site.Normalize()
	raw, err := json.Marshal(site)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value) VALUES ('site', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(raw))
	return err
}
