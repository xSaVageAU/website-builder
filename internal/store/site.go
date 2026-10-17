package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Theme is a handful of design tokens. Users pick from small option sets so
// every combination still looks good.
type Theme struct {
	Primary string `json:"primary"` // hex colour, e.g. #3b5bdb
	Fonts   string `json:"fonts"`   // modern | classic | friendly | editorial | technical
	Shape   string `json:"shape"`   // rounded | soft | sharp
	Density string `json:"density"` // airy | compact
	Mode    string `json:"mode"`    // light | dark
}

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
}

var (
	FontOptions    = []string{"modern", "classic", "friendly", "editorial", "technical"}
	ShapeOptions   = []string{"rounded", "soft", "sharp"}
	DensityOptions = []string{"airy", "compact"}
	ModeOptions    = []string{"light", "dark"}

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
		},
	}
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
