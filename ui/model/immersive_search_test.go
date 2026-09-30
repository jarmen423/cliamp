package model

import (
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

func TestMatchScore(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		want        int
	}{
		{"Kanye West", "kanye west", 3},
		{"Kanye West", "  Kanye ", 2},
		{"The Kanye Tribute", "kanye", 1},
		{"Kanyewest", "west", 0},
		{"Anything", "", 0},
	} {
		if got := matchScore(tt.name, tt.query); got != tt.want {
			t.Errorf("matchScore(%q, %q) = %d, want %d", tt.name, tt.query, got, tt.want)
		}
	}
}

func searchFixture() provider.SearchResults {
	return provider.SearchResults{
		Tracks: []playlist.Track{{Title: "Stronger", Artist: "Kanye West", Path: "spotify:track:1"}},
		Albums: []provider.AlbumInfo{
			{ID: "al1", Name: "Graduation", Artist: "Kanye West"},
			{ID: "al2", Name: "Kanye West", Artist: "Tribute Band"},
		},
		Artists: []provider.ArtistInfo{
			{ID: "ar2", Name: "Kanye West Tribute Band"},
			{ID: "ar1", Name: "Kanye West"},
			{ID: "ar3", Name: "Jay-Z"},
		},
		Playlists: []playlist.PlaylistInfo{{ID: "pl1", Name: "This Is Kanye West", TrackCount: 50}},
	}
}

func resultKinds(ts []playlist.Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = firstNonEmpty(t.ProviderMeta[playlist.MetaKind], "track") + ":" + t.Title
	}
	return out
}

// Typing an artist's name puts that artist first (exact beats prefix), and
// songs come before the remaining albums, artists and playlists.
func TestRankSearchResultsLeadsWithMatchingArtist(t *testing.T) {
	got := resultKinds(rankSearchResults("kanye west", searchFixture()))
	want := []string{
		"artist:Kanye West",
		"artist:Kanye West Tribute Band",
		"track:Stronger",
		"album:Graduation",
		"album:Kanye West",
		"artist:Jay-Z",
		"playlist:This Is Kanye West",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

// With no artist match, an album named exactly like the query leads.
func TestRankSearchResultsLeadsWithExactAlbum(t *testing.T) {
	got := resultKinds(rankSearchResults("graduation", searchFixture()))
	if got[0] != "album:Graduation" || got[1] != "track:Stronger" {
		t.Fatalf("got %v, want the exact album first, then songs", got)
	}
}

// Placeholders open instead of playing: the canvas maps them to artist,
// album and playlist items, and they never enter the play queue.
func TestSearchPlaceholdersMapToItems(t *testing.T) {
	ranked := rankSearchResults("kanye west", searchFixture())
	items := trackItems(ranked)
	if items[0].kind != immKindArtist || items[0].id != "ar1" || items[0].sub != "Artist" {
		t.Fatalf("first item = %+v, want the Kanye West artist page", items[0])
	}
	if last := items[len(items)-1]; last.kind != immKindPlaylist || last.id != "pl1" || last.sub != "Playlist · 50 songs" {
		t.Fatalf("last item = %+v, want the playlist", last)
	}
	kept, at := playableFrom(ranked, 2)
	if len(kept) != 1 || kept[at].Title != "Stronger" {
		t.Fatalf("playableFrom kept %v at %d, want only the song", kept, at)
	}
}
