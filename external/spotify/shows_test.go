package spotify

import (
	"net/url"
	"testing"
)

func showsMock(t *testing.T) *mockAPI {
	m := newMockAPI(t)
	m.handlers["/v1/me/shows"] = func(t *testing.T, _ url.Values) string {
		return `{"items":[{"show":{"id":"sh1","name":"Talk Show","total_episodes":2,"images":[{"url":"https://img/sh1"}]}},{"show":null}],"total":2}`
	}
	m.handlers["/v1/shows/sh1"] = func(t *testing.T, _ url.Values) string {
		return `{"id":"sh1","name":"Talk Show","images":[{"url":"https://img/sh1"}]}`
	}
	m.handlers["/v1/shows/sh1/episodes"] = func(t *testing.T, _ url.Values) string {
		return `{"items":[` +
			`{"id":"e2","name":"Second","duration_ms":120000,"release_date":"2026-01-02","images":[{"url":"https://img/e2"}]},` +
			`{"id":"e1","name":"First","duration_ms":60000,"release_date":"2026-01-01"}
],"total":2}`
	}
	return m
}

func TestSavedShowsRows(t *testing.T) {
	showsMock(t)
	rows := newTestProvider().savedShows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one show", rows)
	}
	row := rows[0]
	if row.ID != savedShowIDPrefix+"sh1" || row.Name != "Talk Show" || row.Section != savedShowSection || row.TrackCount != 2 || row.ImageURL != "https://img/sh1" {
		t.Errorf("row = %+v, want Talk Show in Podcasts", row)
	}
}

func TestShowEpisodes(t *testing.T) {
	showsMock(t)
	got, err := newTestProvider().Tracks(savedShowIDPrefix + "sh1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("episodes = %+v, want 2", got)
	}
	first := got[0]
	if first.Path != "spotify:episode:e2" || first.Title != "Second" || first.Artist != "Talk Show" || first.Album != "Talk Show" || first.DurationSecs != 120 || first.Year != 2026 {
		t.Errorf("episode = %+v, want Second by Talk Show", first)
	}
	if got[1].AlbumArtURL != "https://img/sh1" {
		t.Errorf("episode without art = %q, want the show art", got[1].AlbumArtURL)
	}
}

func TestSavedShowsBestEffort(t *testing.T) {
	newMockAPI(t) // no /v1/me/shows handler: the fetch fails
	if rows := newTestProvider().savedShows(); rows != nil {
		t.Errorf("rows = %+v, want nil on failure", rows)
	}
}
