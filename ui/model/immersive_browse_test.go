package model

import (
	"context"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

var (
	immBrowseCats = []provider.CategoryInfo{
		{ID: "mfy", Name: "Made for you", Group: "For you"},
		{ID: "new", Name: "New releases", Group: "For you"},
		{ID: "jazz", Name: "Jazz", Group: "Genres & moods"},
	}
	immBrowseContent = map[string]provider.SearchResults{
		"jazz": {
			Tracks:    []playlist.Track{{Title: "So What", Artist: "Miles Davis", Path: "spotify:track:1"}},
			Playlists: []playlist.PlaylistInfo{{ID: "pl-jazz", Name: "Jazz Classics", TrackCount: 80}},
		},
	}
)

// immBrowseStub adds a Browse page (and its provider-pane entry) to
// immProvStub.
type immBrowseStub struct{ immProvStub }

func (immBrowseStub) BrowseCategories(context.Context) ([]provider.CategoryInfo, error) {
	return immBrowseCats, nil
}

func (immBrowseStub) CategoryContent(_ context.Context, cat provider.CategoryInfo) (provider.SearchResults, error) {
	return immBrowseContent[cat.ID], nil
}

func (immBrowseStub) BrowseEntries() []provider.BrowseEntry {
	return []provider.BrowseEntry{{ID: "stub:browse", Name: "Browse", Section: "Discover", Mode: provider.BrowseCategories}}
}

func immBrowseModel(t *testing.T) *Model {
	t.Helper()
	m := immersiveModel(t)
	m.immersive.prov = immBrowseStub{}
	return m
}

// The Browse row leads the Playlists section whatever the sort, and the
// filter can hide it; providers without a Browse page get no row.
func TestImmersiveBrowseRowLeadsPlaylists(t *testing.T) {
	for _, tt := range []struct {
		name   string
		prov   playlist.Provider
		filter string
		sort   immBrowseSort
		want   []string
	}{
		{"recents", immBrowseStub{}, "", immBrowseSortRecents, []string{"Browse", "Stuff to listen 2", "kanyesTimelessSound", "Liked Songs"}},
		{"a-z keeps it first", immBrowseStub{}, "", immBrowseSortAlpha, []string{"Browse", "kanyesTimelessSound", "Liked Songs", "Stuff to listen 2"}},
		{"filter matches it", immBrowseStub{}, "brow", immBrowseSortRecents, []string{"Browse"}},
		{"filter hides it", immBrowseStub{}, "liked", immBrowseSortRecents, []string{"Liked Songs"}},
		{"no Browse page", immProvStub{}, "", immBrowseSortRecents, []string{"Stuff to listen 2", "kanyesTimelessSound", "Liked Songs"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := immersiveModel(t)
			m.immersive.prov = tt.prov
			m.immersive.filter, m.immersive.sort = tt.filter, tt.sort
			items := m.browseItems()
			var got []string
			for _, it := range items {
				got = append(got, it.title)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("items = %v, want %v", got, tt.want)
			}
			if got[0] == "Browse" && (items[0].kind != immKindBrowse || items[0].id != "stub:browse" || items[0].sub != "Discover") {
				t.Fatalf("Browse row = %+v, want the provider's entry", items[0])
			}
		})
	}
}

// Browse opens the category list, a category opens its page with playlists
// first, and Back walks out the same way.
func TestImmersiveBrowseFlow(t *testing.T) {
	m := immBrowseModel(t)
	cmd := m.activateItem(m.browseItems()[0])
	if m.immersive.view != immViewCategories || !m.immersive.loadingCategories || m.immersive.tracksLoading {
		t.Fatalf("view=%d loadingCategories=%v tracksLoading=%v; want the category list loading",
			m.immersive.view, m.immersive.loadingCategories, m.immersive.tracksLoading)
	}
	if got := m.renderImmCanvasInner(60, 4)[0]; !strings.Contains(got, "loading browse") {
		t.Fatalf("canvas = %q, want the Browse loading line", got)
	}
	deliver(m, runCmdUntil[immersiveCategoriesMsg](t, cmd))
	items := m.canvasItems()
	if len(items) != 3 || items[0].kind != immKindCategory || items[0].sub != "For you" || items[2].sub != "Genres & moods" {
		t.Fatalf("category items = %+v, want the three categories with their groups", items)
	}
	if title := m.immCanvasTitle(); !strings.HasPrefix(title, "Browse ·") {
		t.Fatalf("title = %q, want Browse", title)
	}

	m.immersive.cursor = 2
	cmd = m.immersiveActivate()
	if m.immersive.view != immViewCategory || !m.immersive.tracksLoading || m.immersive.ctxID != "jazz" {
		t.Fatalf("view=%d loading=%v ctx=%q; want the Jazz page loading", m.immersive.view, m.immersive.tracksLoading, m.immersive.ctxID)
	}
	deliver(m, runCmdUntil[immersiveContentMsg](t, cmd))
	items = m.canvasItems()
	if len(items) != 2 || items[0].kind != immKindPlaylist || items[0].id != "pl-jazz" || items[1].kind != immKindTrack {
		t.Fatalf("page items = %+v, want the playlist then the song", items)
	}
	if title := m.immCanvasTitle(); !strings.HasPrefix(title, "Browse: Jazz") {
		t.Fatalf("title = %q, want Browse: Jazz", title)
	}

	m.immersiveGoBack()
	if m.immersive.view != immViewCategories || m.immersive.cursor != 2 || len(m.immersive.categories) != 3 {
		t.Fatalf("back: view=%d cursor=%d categories=%d; want the list as left", m.immersive.view, m.immersive.cursor, len(m.immersive.categories))
	}
	if cmd := m.immersiveEscape(); cmd != nil || m.immersive.view != immViewBrowse {
		t.Fatalf("esc: view=%d, want the Playlists section", m.immersive.view)
	}
	if cmd := m.activateItem(m.browseItems()[0]); cmd != nil || m.immersive.loadingCategories {
		t.Fatal("reopening Browse refetched the category list")
	}
}

// A failed category list is fetched again the next time Browse opens.
func TestImmersiveBrowseRetriesAfterError(t *testing.T) {
	m := immBrowseModel(t)
	m.activateItem(m.browseItems()[0])
	deliver(m, immersiveCategoriesMsg{providerName: "stub", gen: m.requests.immersiveBrowse, err: context.DeadlineExceeded})
	if m.immersive.loadingCategories || m.immersive.categories != nil {
		t.Fatal("a failed fetch left the list loading or filled")
	}
	m.immersiveGoBack()
	if cmd := m.activateItem(m.browseItems()[0]); cmd == nil || !m.immersive.loadingCategories {
		t.Fatal("reopening Browse after a failure did not refetch")
	}
}

func TestCategoryTracksOrder(t *testing.T) {
	got := resultKinds(categoryTracks(provider.SearchResults{
		Tracks: []playlist.Track{
			{Title: "Song", Path: "spotify:track:1"},
			albumPlaceholder(provider.AlbumInfo{ID: "dup", Name: "Stray placeholder"}),
		},
		Albums:    []provider.AlbumInfo{{ID: "al1", Name: "Album"}},
		Artists:   []provider.ArtistInfo{{ID: "ar1", Name: "Artist"}},
		Playlists: []playlist.PlaylistInfo{{ID: "pl1", Name: "List"}},
	}))
	want := []string{"playlist:List", "album:Album", "track:Song", "artist:Artist"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Both Browse pages are saved and reopen on the next launch.
func TestImmersiveBrowseResume(t *testing.T) {
	for _, tt := range []struct {
		name     string
		view     immersiveView
		kind     immItemKind
		id, ctx  string
		wantSave resume.View
	}{
		{"category list", immViewCategories, immKindBrowse, "stub:browse", "Browse",
			resume.View{Provider: "stub", Section: "playlists", View: "categories", Kind: "browse", ID: "stub:browse", Name: "Browse", Sub: "Discover", Cursor: 1}},
		{"category page", immViewCategory, immKindCategory, "jazz", "Jazz",
			resume.View{Provider: "stub", Section: "playlists", View: "category", Kind: "category", ID: "jazz", Name: "Jazz", Sub: "Genres & moods", Cursor: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prev := immBrowseModel(t)
			prev.immersive.view, prev.immersive.ctxKind = tt.view, tt.kind
			prev.immersive.ctxID, prev.immersive.ctxName, prev.immersive.ctxSub = tt.id, tt.ctx, tt.wantSave.Sub
			prev.immersive.cursor = 1
			saved := prev.immersiveResumeView()
			if saved == nil || *saved != tt.wantSave {
				t.Fatalf("saved %+v, want %+v", saved, tt.wantSave)
			}

			m := immersiveModel(t)
			m.immersive = immersiveState{}
			m.provider = immBrowseStub{}
			m.SetImmersiveRestore(saved)
			m.startImmersive()
			cmd := listsAnswer(m, nil)
			if tt.view == immViewCategories {
				deliver(m, runCmdUntil[immersiveCategoriesMsg](t, cmd))
			} else {
				deliver(m, runCmdUntil[immersiveContentMsg](t, cmd))
			}
			if m.immersive.view != tt.view || m.immersive.ctxID != tt.id || m.immersive.cursor != 1 {
				t.Fatalf("restored view=%d ctx=%q cursor=%d, want view=%d ctx=%q cursor=1", m.immersive.view, m.immersive.ctxID, m.immersive.cursor, tt.view, tt.id)
			}
		})
	}
}
