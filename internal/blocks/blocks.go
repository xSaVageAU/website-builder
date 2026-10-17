// Package blocks defines the section types a page is built from. The schema
// here is the single source of truth: the admin UI renders its forms from it,
// the API validates against it and the renderer resolves values with it.
package blocks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field kinds:
//
//	text      single line of plain text
//	textarea  multiple lines of plain text (line breaks kept)
//	rich      formatted text (sanitised HTML: bold, italic, links, lists)
//	image     a media id
//	link      "page:<id>", a URL, mailto: or tel:
//	choice    one of Options
//	toggle    true/false
//	code      raw HTML for the Custom HTML section, published unchanged
//	list      repeated group of Fields
type Field struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	Label       string   `json:"label"`
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
	Big         bool     `json:"big,omitempty"`
	Fields      []Field  `json:"fields,omitempty"`
	Min         int      `json:"min,omitempty"`
	Max         int      `json:"max,omitempty"`
	ItemLabel   string   `json:"itemLabel,omitempty"`
}

type Type struct {
	Type        string  `json:"type"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Fields      []Field `json:"fields"`
	// Style lists the keys of StyleFields this section type offers.
	Style []string `json:"style"`
}

type Block struct {
	ID    string         `json:"id"`
	Type  string         `json:"type"`
	Data  map[string]any `json:"data"`
	Style map[string]any `json:"style,omitempty"` // only the choices that differ from "auto"
	// CSS is the section's own custom CSS (advanced). Plain declarations apply
	// to the section, nested rules (h2 { … }) to things inside it.
	CSS string `json:"css,omitempty"`
}

// MaxCSS caps custom CSS, per section and site-wide.
const MaxCSS = 50_000

// StyleFields are the fine-tuning controls shared by every section type.
// Each is a few steps rather than a free number, so any combination still
// looks right. The empty value means "auto": whatever the theme does.
var StyleFields = []Field{
	{Key: "spaceTop", Kind: "choice", Label: "Space above", Options: steps},
	{Key: "spaceBottom", Kind: "choice", Label: "Space below", Options: steps},
	{Key: "height", Kind: "choice", Label: "Height", Options: []Option{
		{"", "Auto"}, {"short", "Short"}, {"medium", "Medium"}, {"tall", "Tall"}, {"full", "Full screen"}}},
	{Key: "background", Kind: "choice", Label: "Background", Options: []Option{
		{"", "Auto"}, {"plain", "Plain"}, {"soft", "Soft"}, {"brand", "Brand"}, {"contrast", "Contrast"}}},
	{Key: "width", Kind: "choice", Label: "Content width", Options: []Option{
		{"", "Auto"}, {"narrow", "Narrow"}, {"medium", "Medium"}, {"wide", "Wide"}, {"full", "Full"}}},
}

var steps = []Option{{"", "Auto"}, {"none", "None"}, {"s", "S"}, {"m", "M"}, {"l", "L"}, {"xl", "XL"}}

// Which style controls each section type gets, if not the default set.
var (
	defaultStyle = []string{"spaceTop", "spaceBottom", "background", "width"}
	styleFor     = map[string][]string{
		"hero":    {"height", "background", "width"},
		"cta":     {"spaceTop", "spaceBottom", "height", "background", "width"},
		"image":   {"spaceTop", "spaceBottom", "background"}, // has its own width choice
		"divider": {"spaceTop", "spaceBottom", "width"},
	}
)

func init() {
	for i := range Types {
		if s, ok := styleFor[Types[i].Type]; ok {
			Types[i].Style = s
		} else {
			Types[i].Style = defaultStyle
		}
	}
}

func normalizeStyle(t *Type, in map[string]any) map[string]any {
	var out map[string]any
	for _, key := range t.Style {
		for _, f := range StyleFields {
			if f.Key != key {
				continue
			}
			if v, _ := normalizeValue(f, in[key]).(string); v != "" {
				if out == nil {
					out = map[string]any{}
				}
				out[key] = v
			}
		}
	}
	return out
}

var (
	alignOpts = []Option{{"left", "Left"}, {"center", "Centered"}}
	linkHelp  = "Pick one of your pages, or paste a web address."
)

var Types = []Type{
	{
		Type: "hero", Label: "Hero", Icon: "hero",
		Description: "A big headline to open a page, with an optional button and background photo.",
		Fields: []Field{
			{Key: "heading", Kind: "text", Label: "Headline", Big: true, Default: "A short, bold headline"},
			{Key: "text", Kind: "textarea", Label: "Intro", Placeholder: "One or two sentences that say what this is about."},
			{Key: "buttonLabel", Kind: "text", Label: "Button text", Placeholder: "e.g. Get in touch"},
			{Key: "buttonLink", Kind: "link", Label: "Button goes to", Help: linkHelp},
			{Key: "image", Kind: "image", Label: "Background photo"},
			{Key: "align", Kind: "choice", Label: "Alignment", Options: alignOpts, Default: "center"},
		},
	},
	{
		Type: "heading", Label: "Heading", Icon: "heading",
		Description: "A title that starts a new part of the page.",
		Fields: []Field{
			{Key: "text", Kind: "text", Label: "Heading", Big: true, Default: "Section title"},
			{Key: "level", Kind: "choice", Label: "Size", Options: []Option{{"2", "Large"}, {"3", "Small"}}, Default: "2"},
			{Key: "align", Kind: "choice", Label: "Alignment", Options: alignOpts, Default: "left"},
		},
	},
	{
		Type: "text", Label: "Text", Icon: "text",
		Description: "Paragraphs of writing, with bold, italics, links and lists.",
		Fields: []Field{
			{Key: "html", Kind: "rich", Label: "Text", Placeholder: "Start writing…"},
		},
	},
	{
		Type: "image", Label: "Image", Icon: "image",
		Description: "A single photo or picture, with an optional caption.",
		Fields: []Field{
			{Key: "image", Kind: "image", Label: "Image"},
			{Key: "alt", Kind: "text", Label: "Description", Help: "Describes the picture for people who can't see it, and for search engines."},
			{Key: "caption", Kind: "text", Label: "Caption", Placeholder: "Optional"},
			{Key: "width", Kind: "choice", Label: "Width", Default: "normal",
				Options: []Option{{"normal", "Text width"}, {"wide", "Wide"}, {"full", "Edge to edge"}}},
		},
	},
	{
		Type: "columns", Label: "Columns", Icon: "columns",
		Description: "Two to four side-by-side boxes. Great for services, features or team members.",
		Fields: []Field{
			{Key: "items", Kind: "list", Label: "Columns", ItemLabel: "Column", Min: 1, Max: 4,
				Default: []any{
					map[string]any{"heading": "First thing"},
					map[string]any{"heading": "Second thing"},
					map[string]any{"heading": "Third thing"},
				},
				Fields: []Field{
					{Key: "image", Kind: "image", Label: "Picture"},
					{Key: "heading", Kind: "text", Label: "Title"},
					{Key: "html", Kind: "rich", Label: "Text", Placeholder: "A sentence or two."},
					{Key: "linkLabel", Kind: "text", Label: "Link text", Placeholder: "e.g. Learn more"},
					{Key: "link", Kind: "link", Label: "Link goes to", Help: linkHelp},
				}},
		},
	},
	{
		Type: "button", Label: "Button", Icon: "button",
		Description: "A button that takes visitors to another page or website.",
		Fields: []Field{
			{Key: "label", Kind: "text", Label: "Button text", Default: "Find out more"},
			{Key: "link", Kind: "link", Label: "Goes to", Help: linkHelp},
			{Key: "style", Kind: "choice", Label: "Style", Default: "primary",
				Options: []Option{{"primary", "Solid"}, {"secondary", "Outline"}}},
			{Key: "align", Kind: "choice", Label: "Alignment", Options: alignOpts, Default: "left"},
		},
	},
	{
		Type: "cta", Label: "Call to action", Icon: "cta",
		Description: "A coloured band asking visitors to do one thing. Good at the end of a page.",
		Fields: []Field{
			{Key: "heading", Kind: "text", Label: "Heading", Big: true, Default: "Ready to get started?"},
			{Key: "text", Kind: "textarea", Label: "Text"},
			{Key: "buttonLabel", Kind: "text", Label: "Button text", Default: "Contact us"},
			{Key: "buttonLink", Kind: "link", Label: "Button goes to", Help: linkHelp},
		},
	},
	{
		Type: "gallery", Label: "Gallery", Icon: "gallery",
		Description: "A grid of photos.",
		Fields: []Field{
			{Key: "items", Kind: "list", Label: "Photos", ItemLabel: "Photo", Min: 0, Max: 48,
				Fields: []Field{
					{Key: "image", Kind: "image", Label: "Photo"},
					{Key: "alt", Kind: "text", Label: "Description"},
				}},
			{Key: "columns", Kind: "choice", Label: "Photos per row", Default: "3",
				Options: []Option{{"2", "2"}, {"3", "3"}, {"4", "4"}}},
		},
	},
	{
		Type: "quote", Label: "Quote", Icon: "quote",
		Description: "A testimonial or a memorable quote.",
		Fields: []Field{
			{Key: "text", Kind: "textarea", Label: "Quote", Big: true, Default: "Something nice someone said."},
			{Key: "cite", Kind: "text", Label: "Who said it", Placeholder: "e.g. Sam, happy customer"},
		},
	},
	{
		Type: "video", Label: "Video", Icon: "video",
		Description: "A YouTube or Vimeo video.",
		Fields: []Field{
			{Key: "url", Kind: "text", Label: "Video address", Placeholder: "https://www.youtube.com/watch?v=…"},
			{Key: "caption", Kind: "text", Label: "Caption", Placeholder: "Optional"},
		},
	},
	{
		Type: "subpages", Label: "Subpage list", Icon: "subpages",
		Description: "Automatically lists the pages nested under this one. Updates itself.",
		Fields: []Field{
			{Key: "style", Kind: "choice", Label: "Show as", Default: "cards",
				Options: []Option{{"cards", "Cards"}, {"list", "Simple list"}}},
		},
	},
	{
		Type: "divider", Label: "Divider", Icon: "divider",
		Description: "A thin line to separate parts of a page.",
	},
	{
		Type: "html", Label: "Custom HTML", Icon: "code",
		Description: "Paste embed code from another service (booking widget, newsletter form, map…) or write your own HTML.",
		Fields: []Field{
			{Key: "code", Kind: "code", Label: "HTML",
				Placeholder: "<iframe src=\"https://…\"></iframe>",
				Help:        "Published exactly as written, scripts included, so only paste code you trust. In the editor's preview it runs in an isolated frame."},
		},
	},
}

var byType = func() map[string]*Type {
	m := map[string]*Type{}
	for i := range Types {
		m[Types[i].Type] = &Types[i]
	}
	return m
}()

func Lookup(t string) (*Type, bool) {
	bt, ok := byType[t]
	return bt, ok
}

var richPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "div", "br", "strong", "b", "em", "i", "u", "s", "ul", "ol", "li", "code")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto", "tel", "page")
	p.AllowRelativeURLs(true)
	p.RequireParseableURLs(true)
	return p
}()

// SanitizeRich keeps only the formatting the editor can produce.
func SanitizeRich(s string) string { return richPolicy.Sanitize(s) }

var (
	pageLink = regexp.MustCompile(`^page:\d+$`)
	blockID  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)
)

// CleanLink accepts what a non-technical user is likely to type ("example.com",
// "hello@example.com") and turns it into a safe link, or "" if it can't.
func CleanLink(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "", strings.HasPrefix(s, "//"):
		return ""
	case pageLink.MatchString(s):
		return s
	case strings.HasPrefix(s, "#"), strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//"):
		return s
	}
	lower := strings.ToLower(s)
	for _, prefix := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, prefix) {
			if _, err := url.Parse(s); err != nil {
				return ""
			}
			return s
		}
	}
	if strings.ContainsAny(s, " \t\n<>\"'") {
		return ""
	}
	if strings.Contains(s, "@") && !strings.Contains(s, "/") {
		return "mailto:" + s
	}
	if strings.Contains(s, ".") {
		return "https://" + s
	}
	return ""
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Normalize validates a block list from the editor. Unknown block types and
// fields are dropped, every value is coerced to its field's kind and rich
// text is sanitised.
func Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var in []struct {
		ID    string         `json:"id"`
		Type  string         `json:"type"`
		Data  map[string]any `json:"data"`
		Style map[string]any `json:"style"`
		CSS   string         `json:"css"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("[]")
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("blocks: %w", err)
	}
	out := make([]Block, 0, len(in))
	seen := map[string]bool{}
	for _, b := range in {
		t, ok := byType[b.Type]
		if !ok {
			continue
		}
		id := b.ID
		if !blockID.MatchString(id) || seen[id] {
			id = newID()
		}
		seen[id] = true
		out = append(out, Block{ID: id, Type: b.Type, Data: normalizeFields(t.Fields, b.Data), Style: normalizeStyle(t, b.Style), CSS: clip(strings.TrimSpace(b.CSS), MaxCSS)})
	}
	return json.Marshal(out)
}

// Parse reads stored blocks for rendering, re-normalising so templates can
// rely on every field being present with the right type.
func Parse(raw json.RawMessage) []Block {
	clean, err := Normalize(raw)
	if err != nil {
		return nil
	}
	var out []Block
	_ = json.Unmarshal(clean, &out)
	return out
}

func normalizeFields(fields []Field, data map[string]any) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		v, present := data[f.Key]
		if !present && f.Default != nil {
			v = f.Default
		}
		out[f.Key] = normalizeValue(f, v)
	}
	return out
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func normalizeValue(f Field, v any) any {
	switch f.Kind {
	case "text":
		s, _ := v.(string)
		return clip(strings.ReplaceAll(s, "\n", " "), 500)
	case "textarea":
		s, _ := v.(string)
		return clip(s, 5000)
	case "code":
		// Deliberately not sanitised: it's the site owner's own code.
		s, _ := v.(string)
		return clip(s, 200_000)
	case "rich":
		s, _ := v.(string)
		return SanitizeRich(clip(s, 100_000))
	case "image":
		switch n := v.(type) {
		case float64:
			if n > 0 {
				return int64(n)
			}
		case int64:
			return n
		}
		return int64(0)
	case "link":
		s, _ := v.(string)
		return CleanLink(s)
	case "toggle":
		b, _ := v.(bool)
		return b
	case "choice":
		s, _ := v.(string)
		for _, o := range f.Options {
			if o.Value == s {
				return s
			}
		}
		if d, ok := f.Default.(string); ok {
			return d
		}
		return f.Options[0].Value
	case "list":
		items, _ := v.([]any)
		if f.Max > 0 && len(items) > f.Max {
			items = items[:f.Max]
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			m, _ := it.(map[string]any)
			out = append(out, normalizeFields(f.Fields, m))
		}
		return out
	}
	return nil
}
