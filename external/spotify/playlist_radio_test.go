package spotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestPlaylistTracksRequestRadioIDs(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		fields := req.URL.Query().Get("fields")
		containsID := func(group string) bool {
			match := regexp.MustCompile(group + `\(([^)]*)\)`).FindStringSubmatch(fields)
			return len(match) == 2 && slices.Contains(strings.Split(match[1], ","), "id")
		}
		album := map[string]any{"name": "Album"}
		artist := map[string]any{"name": "Artist"}
		if containsID("album") {
			album["id"] = "album-id"
		}
		if containsID("artists") {
			artist["id"] = "artist-id"
		}
		payload := map[string]any{"total": 1, "items": []map[string]any{{"item": map[string]any{
			"id": "track-id", "type": "track", "name": "Song", "uri": "spotify:track:track-id",
			"album": album, "artists": []map[string]any{artist},
		}}}}
		body, _ := json.Marshal(payload)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})
	session := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})}
	p := New(session, "client", 320)
	tracks, _, err := p.fetchTracksPage(context.Background(), "playlist-id", 0)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks=%v, err=%v", tracks, err)
	}
	if tracks[0].AlbumID() != "album-id" {
		t.Errorf("playlist track album ID=%q", tracks[0].AlbumID())
	}
	if artist, ok := p.ArtistForTrack(tracks[0]); !ok || artist.ID != "artist-id" {
		t.Errorf("playlist track artist=%+v, resolved=%v", artist, ok)
	}
}
