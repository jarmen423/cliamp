package spotify

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

func TestParseSpotifyLink(t *testing.T) {
	for _, tt := range []struct {
		name       string
		text       string
		wantKind   string
		wantID     string
		wantParsed bool
	}{
		{"shared playlist with tracking params", "https://open.spotify.com/playlist/37i9dQZF1E8HDVFJ9KRwfc?si=N9NZCiB0Tb2K0C7L4_vVeQ&utm_source=copy-link&pi=hAXGsHEESjC7a", "playlist", "37i9dQZF1E8HDVFJ9KRwfc", true},
		{"album in shared text", "listen to this https://open.spotify.com/album/4LH4d3cOWNNsVw41Gqt2kv?si=x please", "album", "4LH4d3cOWNNsVw41Gqt2kv", true},
		{"localized track", "https://open.spotify.com/intl-de/track/0TnOYISbd1XYRBk9myaseg", "track", "0TnOYISbd1XYRBk9myaseg", true},
		{"embed artist", "https://open.spotify.com/embed/artist/06HL4z0CvFAxyc27GXpf02", "artist", "06HL4z0CvFAxyc27GXpf02", true},
		{"episode", "https://open.spotify.com/episode/7makk4oTQel546B0PZlDM5", "episode", "7makk4oTQel546B0PZlDM5", true},
		{"show", "https://open.spotify.com/show/5CfCWKI5pZ28U0uOzXkDHe", "show", "5CfCWKI5pZ28U0uOzXkDHe", true},
		{"no scheme", "open.spotify.com/playlist/1abc", "playlist", "1abc", true},
		{"track uri", "spotify:track:6rqhFgbbKwnb9MLmUQDhG6", "track", "6rqhFgbbKwnb9MLmUQDhG6", true},
		{"legacy user playlist uri", "spotify:user:fred:playlist:2xyz", "playlist", "2xyz", true},
		{"other host", "https://example.com/playlist/37i9dQZF1E8HDVFJ9KRwfc", "", "", false},
		{"no link", "just some words", "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kind, id, ok := parseSpotifyLink(tt.text)
			if ok != tt.wantParsed || kind != tt.wantKind || id != tt.wantID {
				t.Errorf("parseSpotifyLink(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.text, kind, id, ok, tt.wantKind, tt.wantID, tt.wantParsed)
			}
		})
	}
}

func TestResolveLink(t *testing.T) {
	original := itemMetadata
	t.Cleanup(func() { itemMetadata = original })
	itemMetadata = func(_ *Session, _ context.Context, uris []string) (map[string]playlist.Track, error) {
		return map[string]playlist.Track{uris[0]: {Path: uris[0], Title: "Song One", Artist: "The Band", AlbumArtURL: "https://img/t1"}}, nil
	}

	for _, tt := range []struct {
		name     string
		text     string
		wantKind string
		wantID   string
		wantName string
		wantPath string
	}{
		{"playlist", "https://open.spotify.com/playlist/pl1?si=zzz", provider.LinkPlaylist, "pl1", "Mix One", ""},
		{"album", "spotify:album:al1", provider.LinkAlbum, "al1", "Record One", ""},
		{"artist", "https://open.spotify.com/artist/ar1", provider.LinkArtist, "ar1", "The Band", ""},
		{"show opens as its episode list", "https://open.spotify.com/show/sh1", provider.LinkPlaylist, savedShowIDPrefix + "sh1", "Talk Show", ""},
		{"track carries the track", "https://open.spotify.com/track/t1", provider.LinkTrack, "t1", "Song One", "spotify:track:t1"},
		{"episode carries the track", "spotify:episode:e1", provider.LinkEpisode, "e1", "Song One", "spotify:episode:e1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockAPI(t)
			m.handlers["/v1/playlists/pl1"] = func(t *testing.T, _ url.Values) string {
				return `{"name":"Mix One","images":[{"url":"https://img/pl1"}]}`
			}
			m.handlers["/v1/albums/al1"] = func(t *testing.T, _ url.Values) string {
				return `{"name":"Record One","images":[{"url":"https://img/al1"}]}`
			}
			m.handlers["/v1/artists/ar1"] = func(t *testing.T, _ url.Values) string {
				return `{"name":"The Band","images":[{"url":"https://img/ar1"}]}`
			}
			m.handlers["/v1/shows/sh1"] = func(t *testing.T, _ url.Values) string {
				return `{"name":"Talk Show","images":[{"url":"https://img/sh1"}]}`
			}
			got, err := newTestProvider().ResolveLink(t.Context(), tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.wantKind || got.ID != tt.wantID || got.Name != tt.wantName {
				t.Errorf("link = (%q, %q, %q), want (%q, %q, %q)", got.Kind, got.ID, got.Name, tt.wantKind, tt.wantID, tt.wantName)
			}
			if tt.wantPath != "" && (got.Track == nil || got.Track.Path != tt.wantPath) {
				t.Errorf("track = %+v, want path %q", got.Track, tt.wantPath)
			}
		})
	}
}

func TestResolveLinkShortAndUnknown(t *testing.T) {
	original := resolveShortLink
	t.Cleanup(func() { resolveShortLink = original })
	resolveShortLink = func(_ context.Context, raw string) (string, error) {
		return "https://open.spotify.com/playlist/pl9?si=1", nil
	}
	m := newMockAPI(t)
	m.handlers["/v1/playlists/pl9"] = func(t *testing.T, _ url.Values) string {
		return `{"name":"Short Mix"}`
	}
	p := newTestProvider()
	got, err := p.ResolveLink(t.Context(), "https://spotify.link/AbC123")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pl9" || got.Name != "Short Mix" {
		t.Errorf("link = %+v, want pl9 Short Mix", got)
	}
	if _, err := p.ResolveLink(t.Context(), "no link at all"); err == nil || !strings.Contains(err.Error(), "no Spotify link") {
		t.Errorf("err = %v, want no-link error", err)
	}
}
