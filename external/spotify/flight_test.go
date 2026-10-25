package spotify

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlightSharesOneCall(t *testing.T) {
	t.Parallel()
	var f flight[int]
	var calls atomic.Int32
	release := make(chan struct{})

	const callers = 5
	var started, done sync.WaitGroup
	got := make([]int, callers)
	started.Add(callers)
	done.Add(callers)
	for i := range callers {
		go func() {
			defer done.Done()
			started.Done()
			got[i], _ = f.do(func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
		}()
	}
	started.Wait()
	time.Sleep(50 * time.Millisecond) // let every caller join the call
	close(release)
	done.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("fn ran %d times, want 1", n)
	}
	for i, v := range got {
		if v != 42 {
			t.Fatalf("caller %d got %d, want 42", i, v)
		}
	}
	// A call after the first finished runs again.
	if v, _ := f.do(func() (int, error) { calls.Add(1); return 7, nil }); v != 7 || calls.Load() != 2 {
		t.Fatalf("later call = %d after %d runs, want 7 after 2", v, calls.Load())
	}
}

// countingRoutes wraps playlistRoutes so each path counts its requests, and
// holds /v1/me/playlists until release closes.
func countingRoutes(release <-chan struct{}, counts *sync.Map) map[string]func(*http.Request) *http.Response {
	routes := playlistRoutes(apiResponse(http.StatusOK, `{"total":2,"items":[
		{"album":{"id":"a2","name":"Zed","total_tracks":3,"artists":[{"name":"Beta"}]}},
		{"album":{"id":"a1","name":"Alpha","total_tracks":5,"artists":[{"name":"Alpha"}]}}
	]}`))
	for path, route := range routes {
		route := route
		routes[path] = func(req *http.Request) *http.Response {
			n, _ := counts.LoadOrStore(req.URL.Path, new(atomic.Int32))
			n.(*atomic.Int32).Add(1)
			if req.URL.Path == "/v1/me/playlists" {
				<-release
			}
			return route(req)
		}
	}
	return routes
}

func requestCount(counts *sync.Map, path string) int32 {
	n, ok := counts.Load(path)
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

// A provider switch fetches the full list, its fast first rows and the album
// list at once. They must share one walk of each endpoint.
func TestConcurrentListFetchesShareRequests(t *testing.T) {
	t.Parallel()
	var counts sync.Map
	release := make(chan struct{})
	p := stubSpotifyAPI("own", countingRoutes(release, &counts))

	var wg sync.WaitGroup
	var full, partial int
	var albums int
	wg.Add(3)
	go func() {
		defer wg.Done()
		lists, err := p.Playlists()
		if err != nil {
			t.Errorf("Playlists() error = %v", err)
		}
		full = len(lists)
	}()
	go func() {
		defer wg.Done()
		lists, err := p.PlaylistsPartial()
		if err != nil {
			t.Errorf("PlaylistsPartial() error = %v", err)
		}
		partial = len(lists)
	}()
	go func() {
		defer wg.Done()
		list, err := p.AlbumList(SortRecent, 0, 50)
		if err != nil {
			t.Errorf("AlbumList() error = %v", err)
		}
		albums = len(list)
	}()
	time.Sleep(100 * time.Millisecond) // let all three join the in-flight calls
	close(release)
	wg.Wait()

	for _, path := range []string{"/v1/me", "/v1/me/tracks", "/v1/me/playlists", "/v1/me/albums"} {
		if n := requestCount(&counts, path); n != 1 {
			t.Errorf("%s requested %d times, want 1", path, n)
		}
	}
	if full != 6 || partial != 4 || albums != 2 {
		t.Fatalf("full = %d, partial = %d, albums = %d; want 6, 4 and 2", full, partial, albums)
	}
}

func TestPlaylistsOrdersRowsThenSortedAlbums(t *testing.T) {
	t.Parallel()
	var counts sync.Map
	release := make(chan struct{})
	close(release)
	p := stubSpotifyAPI("own", countingRoutes(release, &counts))

	lists, err := p.Playlists()
	if err != nil {
		t.Fatalf("Playlists() error = %v", err)
	}
	var got []string
	for _, l := range lists {
		got = append(got, l.ID)
	}
	want := []string{savedTracksPlaylistID, "mine", "shared", "followed", savedAlbumIDPrefix + "a1", savedAlbumIDPrefix + "a2"}
	if len(got) != len(want) {
		t.Fatalf("Playlists() IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Playlists() IDs = %v, want %v", got, want)
		}
	}
	if lists[4].Name != "Alpha - Alpha" || lists[1].Section != "Your playlists" || lists[3].Section != "Followed playlists" {
		t.Fatalf("unexpected rows: %+v", lists)
	}
}

// Refresh must drop the saved albums too, so the next list refetches them
// instead of serving an empty cache.
func TestRefreshRefetchesSavedAlbums(t *testing.T) {
	t.Parallel()
	var counts sync.Map
	release := make(chan struct{})
	close(release)
	p := stubSpotifyAPI("own", countingRoutes(release, &counts))

	if _, err := p.Playlists(); err != nil {
		t.Fatalf("Playlists() error = %v", err)
	}
	p.Refresh()
	lists, err := p.Playlists()
	if err != nil {
		t.Fatalf("second Playlists() error = %v", err)
	}
	if n := requestCount(&counts, "/v1/me/albums"); n != 2 {
		t.Fatalf("/v1/me/albums requested %d times, want 2", n)
	}
	if len(lists) != 6 {
		t.Fatalf("second Playlists() returned %d rows, want 6 with the saved albums", len(lists))
	}
}
