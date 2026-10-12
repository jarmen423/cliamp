package model

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// fakeBrowseProvider adds a Browse page to the fake Spotify library provider.
type fakeBrowseProvider struct {
	*fakeSpotifyLibProvider
	cats    []provider.CategoryInfo
	content map[string]provider.SearchResults
}

func (f *fakeBrowseProvider) BrowseCategories(context.Context) ([]provider.CategoryInfo, error) {
	return f.cats, nil
}

func (f *fakeBrowseProvider) CategoryContent(_ context.Context, cat provider.CategoryInfo) (provider.SearchResults, error) {
	return f.content[cat.ID], nil
}

var _ provider.CategoryBrowser = (*fakeBrowseProvider)(nil)

func newBrowseTestModel() (Model, *fakeBrowseProvider) {
	fake := &fakeBrowseProvider{
		fakeSpotifyLibProvider: &fakeSpotifyLibProvider{name: "Spotify"},
		cats: []provider.CategoryInfo{
			{ID: "mfy", Name: "Made for you", Group: "For you"},
			{ID: "pop", Name: "Pop", Group: "Genres & moods"},
			{ID: "jazz", Name: "Jazz", Group: "Genres & moods"},
		},
		content: map[string]provider.SearchResults{
			"jazz": {
				Tracks:    []playlist.Track{{Path: "spotify:track:1", Title: "So What", Artist: "Miles Davis"}},
				Playlists: []playlist.PlaylistInfo{{ID: "37i9dQZF1DXjazz", Name: "Jazz Classics"}},
			},
			"mfy": {Tracks: []playlist.Track{{Path: "spotify:track:2", Title: "Mine", Artist: "Me"}}},
		},
	}
	m := newSpotifyTestModel(fake)
	m.plVisible = 12
	return m, fake
}

// browseUpdate applies one message and returns the model and its command.
func browseUpdate(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func openBrowseLoaded(t *testing.T) Model {
	t.Helper()
	m, fake := newBrowseTestModel()
	cmd := m.openNavBrowserAt(fake, provider.BrowseCategories)
	if !m.spotSearch.visible || m.spotSearch.screen != spotSearchBrowse || !m.spotSearch.loading {
		t.Fatalf("overlay visible=%v screen=%d loading=%v; want loading Browse", m.spotSearch.visible, m.spotSearch.screen, m.spotSearch.loading)
	}
	if m.navBrowser.visible {
		t.Fatal("the provider-pane route must not open the hierarchical browser")
	}
	m, _ = browseUpdate(t, m, runCmdUntil[spotCategoriesMsg](t, cmd))
	if len(m.spotSearch.categories) != 3 || m.spotSearch.loading {
		t.Fatalf("categories = %d loading = %v", len(m.spotSearch.categories), m.spotSearch.loading)
	}
	return m
}

func TestSpotBrowseRendersGroupedCategories(t *testing.T) {
	m := openBrowseLoaded(t)
	body := m.renderSpotBrowseBody(12)
	for _, want := range []string{"For you", "Made for you", "Genres & moods", "Pop", "Jazz"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "For you") > strings.Index(body, "Made for you") {
		t.Error("group separator must precede its categories")
	}
}

func TestSpotBrowseOpensCategoryAndReturns(t *testing.T) {
	m := openBrowseLoaded(t)
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.spotSearch.category.ID != "jazz" || !m.spotSearch.loading {
		t.Fatalf("category = %q loading = %v; want jazz loading", m.spotSearch.category.ID, m.spotSearch.loading)
	}
	m, _ = browseUpdate(t, m, runCmdUntil[spotCategoryMsg](t, cmd))

	if m.spotSearch.screen != spotSearchResults || !m.spotSearch.multi {
		t.Fatalf("screen = %d multi = %v; want tabbed results", m.spotSearch.screen, m.spotSearch.multi)
	}
	if m.spotSearch.tab != spotTabPlaylists {
		t.Fatalf("tab = %d, want playlists first for a genre", m.spotSearch.tab)
	}
	if h := m.spotSearchHeaderLine(); !strings.Contains(h, "Browse: Spotify / Jazz") {
		t.Fatalf("header = %q, want the Browse crumb", h)
	}

	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.spotSearch.screen != spotSearchBrowse || m.spotSearch.category.ID != "" {
		t.Fatalf("Esc: screen = %d category = %q; want back on the list", m.spotSearch.screen, m.spotSearch.category.ID)
	}
	if m.spotSearch.catCursor != 2 {
		t.Fatalf("catCursor = %d, want 2 kept", m.spotSearch.catCursor)
	}

	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.spotSearch.visible {
		t.Fatal("Esc on the list must close the overlay")
	}
}

func TestSpotBrowsePrefersTracksWithoutPlaylists(t *testing.T) {
	m := openBrowseLoaded(t)
	m, cmd := browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = browseUpdate(t, m, runCmdUntil[spotCategoryMsg](t, cmd))
	if m.spotSearch.tab != spotTabTracks {
		t.Fatalf("tab = %d, want tracks", m.spotSearch.tab)
	}
}

func TestSpotBrowseDropsCategoryLoadAfterLeaving(t *testing.T) {
	m := openBrowseLoaded(t)
	m, cmd := browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := runCmdUntil[spotCategoryMsg](t, cmd)
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.spotSearch.loading {
		t.Fatal("/ must drop the category load, not leave the search input loading")
	}
	m, _ = browseUpdate(t, m, msg)
	if m.spotSearch.screen != spotSearchInput {
		t.Fatalf("screen = %d; a category load from before / must not leave the search input", m.spotSearch.screen)
	}
}

func TestSpotBrowseSearchRoundTrip(t *testing.T) {
	m := openBrowseLoaded(t)
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.spotSearch.screen != spotSearchInput {
		t.Fatalf("screen = %d, want search input after /", m.spotSearch.screen)
	}
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.spotSearch.visible || m.spotSearch.screen != spotSearchBrowse {
		t.Fatalf("Esc from search: visible = %v screen = %d; want back on Browse", m.spotSearch.visible, m.spotSearch.screen)
	}
}

func TestSpotSearchEmptyEnterOpensBrowse(t *testing.T) {
	m, fake := newBrowseTestModel()
	m.openProviderSearchWith(fake)
	m, cmd := browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.spotSearch.screen != spotSearchBrowse || cmd == nil {
		t.Fatalf("screen = %d cmd nil = %v; want Browse loading", m.spotSearch.screen, cmd == nil)
	}
	if m.spotSearch.err != "" {
		t.Fatalf("err = %q, want none", m.spotSearch.err)
	}
}

func TestNavMenuOffersBrowseAndKeepsBrowserUnderneath(t *testing.T) {
	m, fake := newBrowseTestModel()
	m.openNavBrowserWith(fake)
	items := m.navMenuItems()
	idx := -1
	for i, it := range items {
		if it.mode == provider.BrowseCategories {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("menu = %+v, want a Browse item", items)
	}
	m.navBrowser.cursor = idx
	m, cmd := browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.spotSearch.visible || !m.navBrowser.visible {
		t.Fatalf("overlay = %v nav = %v; want Browse over the open browser", m.spotSearch.visible, m.navBrowser.visible)
	}
	m, _ = browseUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.spotSearch.visible || !m.navBrowser.visible {
		t.Fatalf("after Esc overlay = %v nav = %v; want the browser menu back", m.spotSearch.visible, m.navBrowser.visible)
	}
}

func TestSpotBrowseScrollCountsSeparators(t *testing.T) {
	m, _ := newBrowseTestModel()
	for i := range 20 {
		group := "A"
		if i >= 10 {
			group = "B"
		}
		m.spotSearch.categories = append(m.spotSearch.categories, provider.CategoryInfo{ID: string(rune('a' + i)), Name: "C", Group: group})
	}
	m.spotSearch.catCursor = 19
	m.spotBrowseMaybeAdjustScroll(5)
	if rows := sectionRowsToCursor(m.spotSearch.categories, m.spotSearch.catScroll, 19, categoryGroup); rows > 5 {
		t.Fatalf("cursor sits %d rows down a 5-row window", rows)
	}
}
