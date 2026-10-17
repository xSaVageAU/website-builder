package render

import (
	"html/template"

	"sitesmith/internal/store"
)

// The header and footer ("chrome") shared by every page.

type LinkView struct {
	Label string
	Href  template.URL
}

type HeaderView struct {
	Class  string
	Button *LinkView
}

type SocialView struct {
	Label string
	Href  template.URL
	Icon  template.HTML
}

type FooterView struct {
	Class     string
	Links     []LinkView
	Social    []SocialView
	Copyright bool
}

func (c *pageCtx) header() HeaderView {
	h := c.site.Info.Header
	v := HeaderView{Class: "header-" + h.Layout}
	if !h.Sticky {
		v.Class += " not-sticky"
	}
	if h.Background != "" {
		v.Class += " bg-" + h.Background
	}
	if h.ButtonLabel != "" {
		c.inside("Header", func() {
			v.Button = &LinkView{Label: h.ButtonLabel, Href: c.link(h.ButtonLink)}
		})
	}
	return v
}

func (c *pageCtx) footer() FooterView {
	f := c.site.Info.Footer
	v := FooterView{Copyright: f.ShowCopyright}
	if f.Background != "" {
		v.Class = "bg-" + f.Background
	}
	c.inside("Footer", func() {
		for _, l := range f.Links {
			if l.Label == "" {
				continue
			}
			v.Links = append(v.Links, LinkView{Label: l.Label, Href: c.link(l.Link)})
		}
	})
	for _, n := range store.SocialNetworks {
		if href := f.Social[n]; href != "" {
			v.Social = append(v.Social, SocialView{Label: socialLabels[n], Href: template.URL(href), Icon: socialIcon(n)})
		}
	}
	return v
}

// inside reports problems (like a link to a deleted page) as coming from the
// header or footer rather than from whichever page happened to be rendering.
func (c *pageCtx) inside(name string, fn func()) {
	prev := c.pageName
	c.pageName = name
	fn()
	c.pageName = prev
}

var socialLabels = map[string]string{
	"instagram": "Instagram", "facebook": "Facebook", "x": "X", "linkedin": "LinkedIn",
	"youtube": "YouTube", "tiktok": "TikTok", "github": "GitHub", "email": "Email",
}

// Simple line icons in the same style as the rest of the theme.
var socialPaths = map[string]string{
	"instagram": `<rect x="3" y="3" width="18" height="18" rx="5"/><circle cx="12" cy="12" r="4"/><circle cx="17.5" cy="6.5" r="0.6" fill="currentColor"/>`,
	"facebook":  `<path d="M15 3h-2.5A3.5 3.5 0 0 0 9 6.5V10H6.5v3.5H9V21h3.5v-7.5H15l.5-3.5h-3V7a1 1 0 0 1 1-1H15z"/>`,
	"x":         `<path d="M4 4l16 16M20 4L4 20"/>`,
	"linkedin":  `<rect x="3" y="3" width="18" height="18" rx="3"/><path d="M8 10v7M8 7v.01M12 17v-4a2 2 0 0 1 4 0v4M12 10v7"/>`,
	"youtube":   `<rect x="2.5" y="5.5" width="19" height="13" rx="4"/><path d="M10 9.5l5 2.5-5 2.5z" fill="currentColor"/>`,
	"tiktok":    `<path d="M14 3v11.5a3.5 3.5 0 1 1-3.5-3.5"/><path d="M14 3c.5 2.5 2.5 4.5 5 4.5"/>`,
	"github":    `<path d="M9 19c-4 1.5-4-2-6-2.5M15 21v-3.5a3 3 0 0 0-.8-2.3c2.7-.3 5.6-1.3 5.6-6a4.7 4.7 0 0 0-1.3-3.2 4.3 4.3 0 0 0-.1-3.2s-1-.3-3.4 1.3a11.6 11.6 0 0 0-6 0C6.6 2.5 5.6 2.8 5.6 2.8a4.3 4.3 0 0 0-.1 3.2A4.7 4.7 0 0 0 4.2 9.2c0 4.6 2.8 5.6 5.5 6a3 3 0 0 0-.8 2.3V21"/>`,
	"email":     `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7l9 6 9-6"/>`,
}

func socialIcon(n string) template.HTML {
	return template.HTML(`<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
		socialPaths[n] + `</svg>`)
}
