package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/playlist"
)

const playlistListCacheTTL = 5 * time.Minute

// currentUserID returns the authenticated user's Spotify ID, fetched from
// /v1/me at most once per session. Failures are remembered so a network blip
// during the first call doesn't trigger a request on every later use.
func (p *SpotifyProvider) currentUserID(ctx context.Context) string {
	p.mu.Lock()
	if p.meFetched {
		id := p.userID
		p.mu.Unlock()
		return id
	}
	p.mu.Unlock()

	var me struct {
		ID string `json:"id"`
	}
	if resp, err := p.webAPI(ctx, "GET", "/v1/me", nil); err == nil {
		_ = decodeBody(resp, &me)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.userID = me.ID
	p.meFetched = true
	return p.userID
}

// Playlists returns all playlists in the authenticated user's Spotify library:
// the Library rows, owned and followed playlists, then saved albums.
func (p *SpotifyProvider) Playlists() ([]playlist.PlaylistInfo, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}
	if cached, ok := p.cachedPlaylists(); ok {
		return cached, nil
	}
	all, err := p.listFlight.do(p.fetchPlaylists)
	return slices.Clone(all), err
}

// PlaylistsPartial returns the rows Playlists lists before the saved albums.
// They need fewer requests, so the UI can show them while the albums load.
// Implements provider.PartialPlaylister.
func (p *SpotifyProvider) PlaylistsPartial() ([]playlist.PlaylistInfo, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}
	if cached, ok := p.cachedPlaylists(); ok {
		return cached, nil
	}
	rows, err := p.rowsFlight.do(p.fetchPlaylistRows)
	return slices.Clone(rows), err
}

func (p *SpotifyProvider) cachedPlaylists() ([]playlist.PlaylistInfo, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listCache != nil && time.Since(p.listCacheAt) < playlistListCacheTTL {
		return slices.Clone(p.listCache), true
	}
	return nil, false
}

// fetchPlaylists builds the full list. The saved albums load alongside the
// playlist rows and come from the same walk AlbumList uses.
func (p *SpotifyProvider) fetchPlaylists() ([]playlist.PlaylistInfo, error) {
	var albums savedAlbumSet
	var albumsErr error
	albumsDone := make(chan struct{})
	go func() {
		defer close(albumsDone)
		albums, albumsErr = p.savedAlbumSet()
	}()

	rows, err := p.rowsFlight.do(p.fetchPlaylistRows)
	<-albumsDone
	if err != nil {
		return nil, err
	}
	all := slices.Clone(rows)
	all = append(all, albums.rows...)

	// Saved albums are extra. When they fail, show the playlists and report
	// the error, and do not cache the partial list.
	if albumsErr != nil {
		return all, albumsErr
	}

	p.mu.Lock()
	p.listCache = all
	p.listCacheAt = time.Now()
	p.mu.Unlock()
	return all, nil
}

// fetchPlaylistRows returns the Library rows and the user's playlists,
// grouped Library, then owned, then followed, in API order within each.
func (p *SpotifyProvider) fetchPlaylistRows() ([]playlist.PlaylistInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// /v1/me, the Library probes and the playlist pages do not depend on
	// each other, so they run at once. Ownership needs the user ID, so the
	// playlists are sorted into sections after everything returns.
	var (
		wg          sync.WaitGroup
		userID      string
		likedTotal  int
		likedErr    error
		topCount    int
		topOK       bool
		recentCount int
		recentOK    bool
		items       []spotifyPlaylistItem
		itemsErr    error
	)
	wg.Add(5)
	go func() { defer wg.Done(); userID = p.currentUserID(ctx) }()
	go func() { defer wg.Done(); likedTotal, likedErr = p.savedTracksTotal(ctx) }()
	go func() { defer wg.Done(); topCount, topOK = p.probeTopTracksCount(ctx) }()
	go func() { defer wg.Done(); recentCount, recentOK = p.probeRecentlyPlayedCount(ctx) }()
	go func() { defer wg.Done(); items, itemsErr = p.playlistItems(ctx) }()
	wg.Wait()
	if likedErr != nil {
		return nil, likedErr
	}
	if itemsErr != nil {
		return nil, itemsErr
	}

	// Unfortunately, the Spotify API doesn't expose the localized display name.
	// i.e. 'Liked Songs' or 'Lieblingssongs' etc.
	// For the moment, "Your Music" must sufficice without adding a localization
	// map.
	all := []playlist.PlaylistInfo{{
		ID:         savedTracksPlaylistID,
		Name:       "Your Music",
		TrackCount: likedTotal,
		Section:    "Library",
	}}

	// Synthetic Library rows backed by playback-history endpoints. Best-effort:
	// when a probe fails (scope missing, network error) the row is silently
	// omitted rather than failing the whole listing.
	if topOK {
		all = append(all, playlist.PlaylistInfo{
			ID:         topTracksID,
			Name:       "Top Tracks",
			TrackCount: topCount,
			Section:    "Library",
		})
	}
	if recentOK {
		all = append(all, playlist.PlaylistInfo{
			ID:         recentlyPlayedID,
			Name:       "Recently Played",
			TrackCount: recentCount,
			Section:    "Library",
		})
	}

	p.mu.Lock()
	for _, item := range items {
		count := 0
		if item.Items != nil {
			count = item.Items.Total
		}
		section := "Followed playlists"
		owned := userID != "" && item.Owner.ID == userID
		if owned {
			section = "Your playlists"
		}
		// Without a user ID, ownership is unknown, so offer every playlist.
		p.writable[item.ID] = owned || item.Collaborative || userID == ""
		all = append(all, playlist.PlaylistInfo{
			ID:         item.ID,
			Name:       item.Name,
			TrackCount: count,
			Section:    section,
			Owned:      owned,
			ImageURL:   pickCoverImage(item.Images),
		})
		// Update snapshot_id in cache; if it changed, invalidate cached tracks.
		if cached, ok := p.trackCache[item.ID]; ok {
			if cached.snapshotID != item.SnapshotID {
				delete(p.trackCache, item.ID)
			}
		}
		// Store snapshot_id for later cache checks in Tracks().
		if _, ok := p.trackCache[item.ID]; !ok && item.SnapshotID != "" {
			p.trackCache[item.ID] = &playlistCache{snapshotID: item.SnapshotID}
		}
	}
	p.mu.Unlock()

	// Group playlists by section so the UI can emit one header per group.
	// Library first, then owned, then followed; preserve API order within
	// each section. Saved albums follow in Playlists.
	sectionOrder := map[string]int{
		"Library":            0,
		"Your playlists":     1,
		"Followed playlists": 2,
	}
	sort.SliceStable(all, func(i, j int) bool {
		return sectionOrder[all[i].Section] < sectionOrder[all[j].Section]
	})
	return all, nil
}

// savedTracksTotal returns how many songs Liked Songs holds. The playlist
// list does not include Liked Songs, so it is read from /v1/me/tracks.
func (p *SpotifyProvider) savedTracksTotal(ctx context.Context) (int, error) {
	resp, err := p.webAPI(ctx, "GET", "/v1/me/tracks", url.Values{"limit": {"1"}})
	if err != nil {
		return 0, fmt.Errorf("spotify: your music: %w", err)
	}
	var result struct {
		Total int `json:"total"`
	}
	if err := decodeBody(resp, &result); err != nil {
		return 0, fmt.Errorf("spotify: parse playlists: %w", err)
	}
	return result.Total, nil
}

// playlistItems pages through /v1/me/playlists. The list includes only the
// playlists the user created or follows, not Liked Songs.
func (p *SpotifyProvider) playlistItems(ctx context.Context) ([]spotifyPlaylistItem, error) {
	var all []spotifyPlaylistItem
	offset := 0
	limit := spotifyPlaylistPageSize
	for {
		query := url.Values{
			"limit":  {strconv.Itoa(limit)},
			"offset": {strconv.Itoa(offset)},
			"fields": {"items(id,name,snapshot_id,collaborative,owner(id),items.total,images),total"},
		}
		resp, err := p.webAPI(ctx, "GET", "/v1/me/playlists", query)
		if err != nil {
			return nil, fmt.Errorf("spotify: list playlists: %w", err)
		}
		var result struct {
			Items []spotifyPlaylistItem `json:"items"`
			Total int                   `json:"total"`
		}
		if err := decodeBody(resp, &result); err != nil {
			return nil, fmt.Errorf("spotify: parse playlists: %w", err)
		}
		all = append(all, result.Items...)
		if offset+limit >= result.Total {
			return all, nil
		}
		offset += limit
	}
}

// CanAddToPlaylist reports whether tracks can be added to pl. Liked Songs
// and saved albums use other endpoints, and Spotify rejects adds to a
// playlist that the user neither owns nor collaborates on.
// Implements provider.PlaylistTargetFilter.
func (p *SpotifyProvider) CanAddToPlaylist(pl playlist.PlaylistInfo) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writable[pl.ID]
}

// AddTrackToPlaylist adds a track to an existing Spotify playlist.
// The track's Path is used as the Spotify URI (e.g. "spotify:track:..." or
// "spotify:episode:..."); the Spotify API accepts either.
// Implements provider.PlaylistWriter.
func (p *SpotifyProvider) AddTrackToPlaylist(ctx context.Context, playlistID string, track playlist.Track) error {
	trackURI := track.Path
	if err := p.ensureSession(); err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]any{"uris": []string{trackURI}})
	path := fmt.Sprintf("/v1/playlists/%s/items", playlistID)

	resp, err := p.webAPIWithRetry(ctx, "POST", path, nil, bytes.NewReader(body), "application/json", http.StatusOK, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("spotify: add track: %w", err)
	}
	resp.Body.Close()

	// Invalidate caches for this playlist.
	p.mu.Lock()
	delete(p.trackCache, playlistID)
	p.listCache = nil
	p.mu.Unlock()

	return nil
}

// CreatePlaylist creates a new private Spotify playlist and returns its ID.
func (p *SpotifyProvider) CreatePlaylist(ctx context.Context, name string) (string, error) {
	if err := p.ensureSession(); err != nil {
		return "", err
	}

	body, _ := json.Marshal(map[string]any{"name": name, "public": false})

	resp, err := p.webAPIWithRetry(ctx, "POST", "/v1/me/playlists", nil, bytes.NewReader(body), "application/json", http.StatusOK, http.StatusCreated)
	if err != nil {
		return "", fmt.Errorf("spotify: create playlist: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := decodeBody(resp, &result); err != nil {
		return "", fmt.Errorf("spotify: parse created playlist: %w", err)
	}

	// Invalidate playlist list cache.
	p.mu.Lock()
	p.listCache = nil
	p.mu.Unlock()

	return result.ID, nil
}
