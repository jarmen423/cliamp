package spotify

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// stubSession returns a signed-in Session that sends its Web API and lyrics
// requests to rt. The test changes no global client, so it can run in
// parallel.
func stubSession(rt roundTripFunc) *Session {
	return &Session{
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}),
		web:         &http.Client{Transport: rt},
	}
}

func TestPlaylistsIncludesFollowedPlaylists(t *testing.T) {
	t.Parallel()
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v1/me":
			body = `{"id":"me"}`
		case "/v1/me/tracks":
			body = `{"total":1}`
		case "/v1/me/playlists":
			body = `{"items":[` +
				`{"id":"owned","name":"Owned","snapshot_id":"one","owner":{"id":"me"},"items":{"total":2}},` +
				`{"id":"followed","name":"Followed","snapshot_id":"two","owner":{"id":"other"},"items":{"total":3}}` +
				`],"total":2}`
		case "/v1/me/albums":
			// Returned most-recently-added first; the provider re-sorts by artist.
			body = `{"items":[` +
				`{"album":{"id":"al0","name":"Punk In Drublic","total_tracks":17,"artists":[{"name":"NOFX"}]}},` +
				`{"album":{"id":"al1","name":"Suffer","total_tracks":15,"artists":[{"name":"Bad Religion"}]}}` +
				`],"total":2}`
		default:
			return nil, fmt.Errorf("unexpected Spotify API path %q", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	sess := stubSession(rt)
	got, err := New(sess, "client", 320).Playlists()
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		id      string
		section string
		owned   bool
	}{
		{id: "YOUR MUSIC", section: "Library"},
		{id: "owned", section: "Your playlists", owned: true},
		{id: "followed", section: "Followed playlists"},
		// Saved albums sort alphabetically by artist: Bad Religion before NOFX.
		{id: "spotify:album:al1", section: "Saved albums"},
		{id: "spotify:album:al0", section: "Saved albums"},
	}
	if len(got) != len(want) {
		t.Fatalf("Playlists() returned %d playlists, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].ID != want[i].id || got[i].Section != want[i].section || got[i].Owned != want[i].owned {
			t.Errorf("playlist %d = (%q, %q, owned=%v), want (%q, %q, owned=%v)",
				i, got[i].ID, got[i].Section, got[i].Owned, want[i].id, want[i].section, want[i].owned)
		}
	}

	// The saved album row carries its artist and track count for display.
	album := got[len(got)-1]
	if album.Name != "NOFX - Punk In Drublic" {
		t.Errorf("saved album name = %q, want %q", album.Name, "NOFX - Punk In Drublic")
	}
	if album.TrackCount != 17 {
		t.Errorf("saved album track count = %d, want 17", album.TrackCount)
	}
}

func TestParseSpotifyAddedAt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"rfc3339 utc", "2026-04-01T12:34:56Z", "2026-04-01T12:34:56Z"},
		{"rfc3339 offset", "2026-04-01T14:34:56+02:00", "2026-04-01T12:34:56Z"},
		{"unix seconds", "1700000000", "2023-11-14T22:13:20Z"},
		{"unix milliseconds", "1700000000500", "2023-11-14T22:13:20.5Z"},
		{"zero", "0", ""},
		{"negative", "-10", ""},
		{"unparseable", "not-a-time", "not-a-time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSpotifyAddedAt(tc.raw); got != tc.want {
				t.Errorf("parseSpotifyAddedAt(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestFetchTracksPagePopulatesAddedAt(t *testing.T) {
	t.Parallel()
	var gotFields string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/playlists/pl1/items" {
			return nil, fmt.Errorf("unexpected Spotify API path %q", req.URL.Path)
		}
		gotFields = req.URL.Query().Get("fields")
		body := `{"items":[` +
			`{"added_at":"2026-01-02T03:04:05Z","item":{"id":"t1","name":"First","duration_ms":120000,"artists":[{"id":"a1","name":"Artist A"}],"album":{"id":"al1","name":"Album A"}}},` +
			`{"added_at":"2026-03-04T05:06:07+01:00","item":{"id":"t2","name":"Second","duration_ms":180000,"artists":[{"id":"a2","name":"Artist B"}],"album":{"id":"al2","name":"Album B"}}}` +
			`],"total":2}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	p := New(stubSession(rt), "client", 320)
	tracks, total, err := p.fetchTracksPage(t.Context(), "pl1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotFields, "added_at") {
		t.Errorf("fields = %q, want added_at included", gotFields)
	}
	if total != 2 || len(tracks) != 2 {
		t.Fatalf("got %d tracks (total=%d), want 2", len(tracks), total)
	}
	if tracks[0].AddedAt != "2026-01-02T03:04:05Z" {
		t.Errorf("tracks[0].AddedAt = %q, want 2026-01-02T03:04:05Z", tracks[0].AddedAt)
	}
	if tracks[1].AddedAt != "2026-03-04T04:06:07Z" {
		t.Errorf("tracks[1].AddedAt = %q, want 2026-03-04T04:06:07Z", tracks[1].AddedAt)
	}
}
