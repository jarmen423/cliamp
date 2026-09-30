package model

// immersive_search.go ranks multi-type search results for the immersive
// Search view. Providers with a MultiSearcher (Spotify) return tracks,
// albums, artists and playlists in one call; the view lists them as one
// track list, with albums, artists and playlists carried as placeholder
// tracks (ProviderMeta[playlist.MetaKind] set) so every track-view path
// (play, queue, menus) keeps indexing the same list.

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// Placeholder kinds beside playlist.MetaKindAlbum, and the meta keys they
// carry.
const (
	immMetaKindArtist   = "artist"
	immMetaKindPlaylist = "playlist"
	immMetaItemID       = "itemID" // provider id of an artist or playlist placeholder
	immMetaSub          = "sub"    // subtitle of a playlist placeholder
)

// immSearchLimit is the per-type result count asked of a MultiSearcher
// (Spotify's Development Mode caps a search page at 10).
const immSearchLimit = 10

// isSearchPlaceholder reports whether t stands for an album, artist or
// playlist in search results rather than a playable track.
func isSearchPlaceholder(t playlist.Track) bool {
	return t.ProviderMeta[playlist.MetaKind] != ""
}

// matchScore rates how well name matches query, ignoring case and outer
// spaces: 3 exact, 2 prefix, 1 when the query starts a later word, else 0.
func matchScore(name, query string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	q := strings.ToLower(strings.TrimSpace(query))
	switch {
	case q == "":
		return 0
	case n == q:
		return 3
	case strings.HasPrefix(n, q):
		return 2
	case strings.Contains(n, " "+q):
		return 1
	}
	return 0
}

// rankSearchResults orders multi-type results the way people scan them:
// artists whose name matches the query lead (so typing an artist's name
// puts their page first), then an album named exactly like the query when
// no artist leads, then songs, albums, the other artists, and playlists.
func rankSearchResults(query string, res provider.SearchResults) []playlist.Track {
	artists := append([]provider.ArtistInfo(nil), res.Artists...)
	sort.SliceStable(artists, func(i, j int) bool {
		return matchScore(artists[i].Name, query) > matchScore(artists[j].Name, query)
	})
	lead := 0
	for lead < len(artists) && lead < 2 && matchScore(artists[lead].Name, query) >= 2 {
		lead++
	}

	out := make([]playlist.Track, 0, len(res.Tracks)+len(res.Albums)+len(res.Artists)+len(res.Playlists))
	for _, a := range artists[:lead] {
		out = append(out, artistPlaceholder(a))
	}
	albums := res.Albums
	if lead == 0 {
		for i, a := range albums {
			if matchScore(a.Name, query) == 3 {
				out = append(out, albumPlaceholder(a))
				albums = append(append([]provider.AlbumInfo(nil), albums[:i]...), albums[i+1:]...)
				break
			}
		}
	}
	for _, t := range res.Tracks {
		if !isSearchPlaceholder(t) {
			out = append(out, t)
		}
	}
	for _, a := range albums {
		out = append(out, albumPlaceholder(a))
	}
	for _, a := range artists[lead:] {
		out = append(out, artistPlaceholder(a))
	}
	for _, p := range res.Playlists {
		out = append(out, playlistPlaceholder(p))
	}
	return out
}

func albumPlaceholder(a provider.AlbumInfo) playlist.Track {
	return playlist.Track{
		Title: a.Name, Artist: a.Artist, Album: a.Name, AlbumArtURL: a.ImageURL,
		ProviderMeta: map[string]string{playlist.MetaKind: playlist.MetaKindAlbum, playlist.MetaAlbumID: a.ID},
	}
}

func artistPlaceholder(a provider.ArtistInfo) playlist.Track {
	return playlist.Track{
		Title: a.Name, AlbumArtURL: a.ImageURL,
		ProviderMeta: map[string]string{playlist.MetaKind: immMetaKindArtist, immMetaItemID: a.ID},
	}
}

func playlistPlaceholder(p playlist.PlaylistInfo) playlist.Track {
	return playlist.Track{
		Title: p.Name, AlbumArtURL: p.ImageURL,
		ProviderMeta: map[string]string{playlist.MetaKind: immMetaKindPlaylist, immMetaItemID: p.ID, immMetaSub: playlistRowSub(p)},
	}
}

// placeholderItem is the canvas item for a search placeholder track.
func placeholderItem(t playlist.Track) immItem {
	switch t.ProviderMeta[playlist.MetaKind] {
	case immMetaKindArtist:
		return immItem{kind: immKindArtist, id: t.ProviderMeta[immMetaItemID], title: t.Title, sub: "Artist", art: t.AlbumArtURL}
	case immMetaKindPlaylist:
		return immItem{kind: immKindPlaylist, id: t.ProviderMeta[immMetaItemID], title: t.Title, sub: t.ProviderMeta[immMetaSub], art: t.AlbumArtURL}
	}
	// Search results carry album hits; they open the album rather than play.
	return immItem{
		kind: immKindAlbum, id: t.AlbumID(), title: t.Title,
		sub: "Album · " + firstNonEmpty(t.Artist, "Various"), art: t.AlbumArtURL,
	}
}

func fetchImmersiveMultiSearchCmd(ctx context.Context, s provider.MultiSearcher, providerName, query string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		res, err := s.SearchAll(ctx, query, immSearchLimit)
		return immersiveSearchMsg{tracks: rankSearchResults(query, res), providerName: providerName, gen: gen, err: err}
	}
}
