package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSharedSections(t *testing.T) {
	st := newSite(t)
	a, _ := st.CreatePage(0, "A", nil)
	b, _ := st.CreatePage(0, "B", nil)
	sh, err := st.CreateShared("Big CTA", json.RawMessage(`{"id":"inner","type":"cta","data":{"heading":"Call us"}}`))
	if err != nil {
		t.Fatal(err)
	}
	place := json.RawMessage(`[{"id":"p1","type":"shared","data":{"ref":"` + sh.ID + `"}}]`)
	st.UpdatePage(a.ID, PageUpdate{Blocks: &place})
	st.UpdatePage(b.ID, PageUpdate{Blocks: &place})

	list, _ := st.SharedBlocks()
	if len(list) != 1 || list[0].Uses != 2 || list[0].Name != "Big CTA" {
		t.Fatalf("library = %+v", list)
	}
	v, _ := st.SaveVersion("saved", "")

	// Editing once changes it for both pages (they only hold a reference).
	edited := json.RawMessage(`{"id":"inner","type":"cta","data":{"heading":"Call us today"}}`)
	if err := st.UpdateShared(sh.ID, nil, &edited); err != nil {
		t.Fatal(err)
	}

	// Deleting it leaves each page with its own copy of the latest content.
	if err := st.DeleteShared(sh.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		p, _ := st.Page(id)
		if !strings.Contains(string(p.Blocks), `"type":"cta"`) || !strings.Contains(string(p.Blocks), "Call us today") || !strings.Contains(string(p.Blocks), `"id":"p1"`) {
			t.Errorf("page %d after delete = %s", id, p.Blocks)
		}
	}
	if list, _ := st.SharedBlocks(); len(list) != 0 {
		t.Error("still in the library")
	}

	// Going back to the version restores the library entry and the references.
	if err := st.RestoreVersion(v, false); err != nil {
		t.Fatal(err)
	}
	list, _ = st.SharedBlocks()
	if len(list) != 1 || list[0].Uses != 2 || !strings.Contains(string(list[0].Block), `"Call us"`) {
		t.Errorf("after restore = %+v", list)
	}
}
