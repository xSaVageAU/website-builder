package blocks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanLink(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"page:12":               "page:12",
		"example.com":           "https://example.com",
		"https://example.com/a": "https://example.com/a",
		"hello@example.com":     "mailto:hello@example.com",
		"tel:+15550100":         "tel:+15550100",
		"#contact":              "#contact",
		"/about/":               "/about/",
		"javascript:alert(1)":   "",
		"//evil.example":        "",
		"not a link":            "",
		`x" onclick="alert(1)`:  "",
	}
	for in, want := range cases {
		if got := CleanLink(in); got != want {
			t.Errorf("CleanLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	in := `[
		{"type":"text","data":{"html":"<p onclick=x>Hi <script>bad()</script><a href=\"page:3\">us</a></p>"}},
		{"type":"nope","data":{}},
		{"id":"keep","type":"heading","data":{"level":"9","extra":"dropped"}}
	]`
	out, err := Normalize(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	var got []Block
	_ = json.Unmarshal(out, &got)
	if len(got) != 2 {
		t.Fatalf("got %d blocks, want 2", len(got))
	}
	html := got[0].Data["html"].(string)
	if strings.Contains(html, "script") || strings.Contains(html, "onclick") || !strings.Contains(html, `href="page:3"`) {
		t.Errorf("sanitised html = %q", html)
	}
	h := got[1]
	if h.ID != "keep" || h.Data["level"] != "2" || h.Data["text"] != "Section title" || h.Data["extra"] != nil {
		t.Errorf("heading = %+v", h)
	}
}

func TestNormalizeStyle(t *testing.T) {
	in := `[
		{"id":"h","type":"hero","data":{},"style":{"height":"tall","background":"brand","spaceTop":"xl","width":"bogus"}},
		{"id":"t","type":"text","data":{},"style":{"spaceTop":"","spaceBottom":"l"}},
		{"id":"d","type":"divider","data":{}}
	]`
	out, err := Normalize(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	var got []Block
	_ = json.Unmarshal(out, &got)
	hero := got[0].Style
	// spaceTop isn't offered on a hero, and "bogus" isn't a width.
	if len(hero) != 2 || hero["height"] != "tall" || hero["background"] != "brand" {
		t.Errorf("hero style = %v", hero)
	}
	if text := got[1].Style; len(text) != 1 || text["spaceBottom"] != "l" {
		t.Errorf("text style = %v (auto values should be dropped)", text)
	}
	if got[2].Style != nil || strings.Contains(string(out), `"style":{}`) {
		t.Errorf("unstyled block should have no style: %s", out)
	}
}
