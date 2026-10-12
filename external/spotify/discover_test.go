package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

type route = func(*http.Request) *http.Response

// queryRoute answers each request from its query string.
func queryRoute(reply func(q url.Values) (int, string)) route {
	return func(req *http.Request) *http.Response {
		status, body := reply(req.URL.Query())
		return apiResponse(status, body)(req)
	}
}

var notFoundBody = `{"error":{"status":404,"message":"Not Found"}}`

// playlistItems renders a search playlist page; an "ed" prefix makes the
// playlist Spotify-owned and "" renders a null row.
func playlistItems(ids ...string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		owner := "someone"
		if strings.HasPrefix(id, "ed") {
			owner = spotifyOwnerID
		}
		parts[i] = fmt.Sprintf(`{"id":%q,"name":"List %s","owner":{"id":%q}}`, id, id, owner)
		if id == "" {
			parts[i] = "null"
		}
	}
	return `{"playlists":{"items":[` + strings.Join(parts, ",") + `]}}`
}

func genre(name string) provider.CategoryInfo {
	return provider.CategoryInfo{ID: builtinCategoryPrefix + name, Name: name}
}

func TestBrowseCategoriesListsForYouThenAPICategories(t *testing.T) {
	calls := 0
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/browse/categories": queryRoute(func(q url.Values) (int, string) {
			calls++
			if q.Get("offset") == "0" {
				return http.StatusOK, `{"categories":{"items":[` +
					`{"id":"c1","name":"Pop","icons":[{"url":"https://img/pop","width":274}]},` +
					`{"id":"c2","name":"Made For You"},` +
					`{"id":"c3","name":"Podcast Charts"}` +
					`],"next":"https://api.spotify.com/v1/browse/categories?offset=50"}}`
			}
			return http.StatusOK, `{"categories":{"items":[{"id":"c4","name":"Jazz"}],"next":null}}`
		}),
	})

	got, err := p.BrowseCategories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.CategoryInfo{
		{ID: madeForYouCategoryID, Name: "Made for you", Group: categoryGroupForYou},
		{ID: newReleasesID, Name: "New releases", Group: categoryGroupForYou},
		{ID: "c1", Name: "Pop", Group: categoryGroupGenres, ImageURL: "https://img/pop"},
		{ID: "c4", Name: "Jazz", Group: categoryGroupGenres},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("categories =\n%v\nwant\n%v", got, want)
	}

	if _, err := p.BrowseCategories(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("category requests = %d, want 2 (second call served from cache)", calls)
	}
}

func TestBrowseCategoriesFallsBackToBuiltinGenres(t *testing.T) {
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/browse/categories": apiResponse(http.StatusNotFound, notFoundBody),
	})

	got, err := p.BrowseCategories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2+len(builtinCategories) {
		t.Fatalf("categories = %d, want %d", len(got), 2+len(builtinCategories))
	}
	if got[2] != (provider.CategoryInfo{ID: builtinCategoryPrefix + "Pop", Name: "Pop", Group: categoryGroupGenres}) {
		t.Fatalf("first genre = %+v, want built-in Pop", got[2])
	}
}

func TestCategoryContentSearchesPlaylistsAndGenre(t *testing.T) {
	var queries []string
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/me": apiResponse(http.StatusOK, `{"id":"me"}`),
		"/v1/search": queryRoute(func(q url.Values) (int, string) {
			queries = append(queries, q.Get("q")+"|"+q.Get("type")+"|"+q.Get("offset"))
			switch {
			case q.Get("type") == "playlist" && q.Get("offset") == "":
				return http.StatusOK, playlistItems("a1", "", "ed1", "a2", "a3", "a4", "a5", "a6", "a7", "a8")
			case q.Get("type") == "playlist":
				return http.StatusOK, playlistItems("a1", "ed2")
			}
			return http.StatusOK, `{"artists":{"items":[{"id":"ar1","name":"Miles"}]},"tracks":{"items":[` + trackJSON("t1") + `]}}`
		}),
	})

	got, err := p.CategoryContent(t.Context(), genre("Jazz"))
	if err != nil {
		t.Fatal(err)
	}
	wantQueries := []string{"Jazz|playlist|", "Jazz|playlist|10", `genre:"jazz"|artist,track|`}
	if fmt.Sprint(queries) != fmt.Sprint(wantQueries) {
		t.Fatalf("queries = %q, want %q", queries, wantQueries)
	}
	var ids []string
	for _, pl := range got.Playlists {
		ids = append(ids, pl.ID)
	}
	// Spotify-owned first, null dropped, the repeat of a1 deduplicated.
	if want := "[ed1 ed2 a1 a2 a3 a4 a5 a6 a7 a8]"; fmt.Sprint(ids) != want {
		t.Fatalf("playlists = %v, want %s", ids, want)
	}
	if len(got.Artists) != 1 || len(got.Tracks) != 1 {
		t.Fatalf("artists = %d tracks = %d, want 1 and 1", len(got.Artists), len(got.Tracks))
	}

	if _, err := p.CategoryContent(t.Context(), genre("Jazz")); err != nil {
		t.Fatal(err)
	}
	if len(queries) != len(wantQueries) {
		t.Fatalf("searches = %d after reopening, want the cached page", len(queries))
	}
}

func TestCategoryContentFallsBackToPlainSearchForMoods(t *testing.T) {
	var artistQueries []string
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/me": apiResponse(http.StatusOK, `{"id":"me"}`),
		"/v1/search": queryRoute(func(q url.Values) (int, string) {
			if q.Get("type") == "playlist" {
				return http.StatusOK, playlistItems("p1")
			}
			artistQueries = append(artistQueries, q.Get("q"))
			if strings.HasPrefix(q.Get("q"), "genre:") {
				return http.StatusOK, `{"artists":{"items":[]},"tracks":{"items":[]}}`
			}
			return http.StatusOK, `{"artists":{"items":[{"id":"ar1","name":"Calm"}]},"tracks":{"items":[]}}`
		}),
	})

	got, err := p.CategoryContent(t.Context(), genre("Sleep"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `[genre:"sleep" Sleep]`; fmt.Sprint(artistQueries) != want {
		t.Fatalf("artist queries = %v, want %s", artistQueries, want)
	}
	if len(got.Artists) != 1 || got.Artists[0].Name != "Calm" {
		t.Fatalf("artists = %+v, want the plain-search hit", got.Artists)
	}
}

func TestNewReleasesPagesWhenFullPageRejected(t *testing.T) {
	var limits []string
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/browse/new-releases": queryRoute(func(q url.Values) (int, string) {
			limits = append(limits, q.Get("limit")+"@"+q.Get("offset"))
			switch {
			case q.Get("limit") != "10":
				return http.StatusBadRequest, `{"error":{"status":400,"message":"Invalid limit"}}`
			case q.Get("offset") == "10":
				return http.StatusOK, `{"albums":{"items":[]}}`
			}
			return http.StatusOK, `{"albums":{"items":[{"id":"al1","name":"Fresh","release_date":"2026-09-30","artists":[{"id":"ar1","name":"New"}]},null]}}`
		}),
	})

	got, err := p.CategoryContent(t.Context(), provider.CategoryInfo{ID: newReleasesID})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[50@ 10@ 10@10]"; fmt.Sprint(limits) != want {
		t.Fatalf("requests = %v, want %s", limits, want)
	}
	if len(got.Albums) != 1 || got.Albums[0].Name != "Fresh" || got.Albums[0].Year != 2026 {
		t.Fatalf("albums = %+v, want the one non-null release", got.Albums)
	}
}

func TestForeignPlaylistFallsBackToSpclient(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			apiCalls := 0
			p := stubSpotifyAPI("own", map[string]route{
				"/v1/playlists/ed1/items": func(req *http.Request) *http.Response {
					apiCalls++
					return apiResponse(status, `{"error":{"status":0,"message":"no"}}`)(req)
				},
			})
			resolved := 0
			p.resolvePlaylist = func(_ context.Context, id string) ([]playlist.Track, error) {
				resolved++
				if id != "ed1" {
					t.Errorf("resolved %q, want ed1", id)
				}
				return []playlist.Track{{Path: "spotify:track:a", Title: "A"}, {Path: "spotify:track:b", Title: "B"}}, nil
			}

			tracks, next, err := p.TracksPage("ed1", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(tracks) != 2 || next != 0 {
				t.Fatalf("TracksPage = %d tracks next %d, want 2 tracks as one complete page", len(tracks), next)
			}
			again, err := p.Tracks("ed1")
			if err != nil || len(again) != 2 {
				t.Fatalf("Tracks = %d, %v; want the cached 2 tracks", len(again), err)
			}
			if resolved != 1 || apiCalls != 1 {
				t.Fatalf("resolver calls = %d, API calls = %d; want 1 and 1 (second read cached)", resolved, apiCalls)
			}
		})
	}
}

func TestForeignPlaylistFallbackFailureKeepsAPIError(t *testing.T) {
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/playlists/x/items": apiResponse(http.StatusForbidden, `{"error":{"status":403,"message":"Forbidden"}}`),
	})
	p.resolvePlaylist = func(context.Context, string) ([]playlist.Track, error) {
		return nil, errors.New("spclient down")
	}
	_, err := p.Tracks("x")
	if err == nil || !strings.Contains(err.Error(), "playlists you own or collaborate on") {
		t.Fatalf("err = %v, want the Web API hint", err)
	}
}

func TestSavedTracksErrorsDoNotFallBack(t *testing.T) {
	p := stubSpotifyAPI("own", map[string]route{
		"/v1/me/tracks": apiResponse(http.StatusNotFound, notFoundBody),
	})
	p.resolvePlaylist = func(context.Context, string) ([]playlist.Track, error) {
		t.Fatal("saved tracks must not use the playlist fallback")
		return nil, nil
	}
	if _, err := p.Tracks(savedTracksPlaylistID); err == nil {
		t.Fatal("want the 404 error")
	}
}
