package model

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/provider"
)

// immMultiStub adds a MultiSearcher to immProvStub.
type immMultiStub struct {
	immProvStub
	calls int
}

func (s *immMultiStub) SearchAll(context.Context, string, int) (provider.SearchResults, error) {
	s.calls++
	return searchFixture(), nil
}

// suggestModel opens the search input on a multi-search provider and types
// query, then delivers the typing-pause tick and the results.
func suggestModel(t *testing.T, query string) (*Model, *immMultiStub) {
	t.Helper()
	m := immersiveModel(t)
	stub := &immMultiStub{}
	m.immersive.prov = stub
	m.openImmersiveSearch()
	for _, r := range query {
		m.handleImmersiveInputKey(tea.KeyPressMsg{Text: string(r)})
	}
	cmd, ok := m.handleImmersiveSuggestMsg(immSuggestTickMsg{gen: m.requests.immersiveSuggest})
	if !ok || cmd == nil {
		t.Fatal("typing-pause tick did not start a search")
	}
	if _, ok := m.handleImmersiveSuggestMsg(cmd()); !ok {
		t.Fatal("suggestion results not handled")
	}
	return m, stub
}

func TestImmersiveSuggestListsRankedResults(t *testing.T) {
	m, stub := suggestModel(t, "kanye west")
	if stub.calls != 1 {
		t.Fatalf("SearchAll calls = %d, want 1 (one per typing pause, not per key)", stub.calls)
	}
	if len(m.immersive.suggest) == 0 || m.immersive.suggest[0].Title != "Kanye West" {
		t.Fatalf("suggestions = %v, want the Kanye West artist first", resultKinds(m.immersive.suggest))
	}
	if m.immersive.suggestCursor != -1 {
		t.Fatalf("suggestCursor = %d, want no pick until Up/Down", m.immersive.suggestCursor)
	}
	// A tick from before the last edit is stale and searches nothing.
	if cmd, _ := m.handleImmersiveSuggestMsg(immSuggestTickMsg{gen: m.requests.immersiveSuggest - 1}); cmd != nil {
		t.Fatal("stale typing-pause tick started a search")
	}
}

func TestImmersiveSuggestPickOpensArtistAndBackReturns(t *testing.T) {
	m, _ := suggestModel(t, "kanye west")
	m.handleImmersiveInputKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.handleImmersiveInputKey(keyMsg("enter"))
	if m.immersive.searching || m.immersive.view != immViewArtist || m.immersive.ctxID != "ar1" {
		t.Fatalf("searching=%v view=%d ctx=%q, want the artist page ar1", m.immersive.searching, m.immersive.view, m.immersive.ctxID)
	}
	m.immersiveGoBack()
	if m.immersive.view != immViewSearch || len(m.immersive.tracks) == 0 || m.immersive.tracks[0].Title != "Kanye West" {
		t.Fatalf("back: view=%d tracks=%v, want the suggestions as results", m.immersive.view, resultKinds(m.immersive.tracks))
	}
}

// Enter with nothing picked keeps the old behavior: the full search.
func TestImmersiveSuggestEnterWithoutPickRunsSearch(t *testing.T) {
	m, _ := suggestModel(t, "kanye")
	cmd := m.handleImmersiveInputKey(keyMsg("enter"))
	if cmd == nil || m.immersive.view != immViewSearch || !m.immersive.searchLoading {
		t.Fatalf("view=%d loading=%v, want a full search", m.immersive.view, m.immersive.searchLoading)
	}
	if len(m.immersive.suggest) != 0 {
		t.Fatal("dropdown still open after Enter")
	}
}

func TestImmersiveSuggestDropdownRendersInFrame(t *testing.T) {
	m, _ := suggestModel(t, "kanye west")
	g := m.immGeom()
	frame := m.renderImmersive()
	if !strings.Contains(frame, "Suggestions") || !strings.Contains(frame, "Artist") {
		t.Fatalf("dropdown missing from frame:\n%s", ansi.Strip(frame))
	}
	for i, l := range strings.Split(frame, "\n") {
		if w := ansi.StringWidth(l); w != g.w {
			t.Fatalf("line %d width %d, want %d: %q", i, w, g.w, ansi.Strip(l))
		}
	}
	r, _ := m.immSuggestGeom()
	for _, s := range m.immArtSlots() {
		if m.immSuggestCovers(s.rect) {
			t.Fatalf("art slot %v under the dropdown %v would draw over it", s.rect, r)
		}
	}
	m.handleImmersiveInputKey(keyMsg("esc"))
	if strings.Contains(m.renderImmersive(), "Suggestions") {
		t.Fatal("Esc left the dropdown open")
	}
}
