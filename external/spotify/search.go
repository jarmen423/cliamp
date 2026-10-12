package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// Compile-time interface checks.
var (
	_ provider.MultiSearcher = (*SpotifyProvider)(nil)
)

// searchPlaylist is the simplified playlist object returned by /v1/search.
// The track total lives under "tracks" (classic API) or "items" (post-episode
// rename); whichever is present wins.
type searchPlaylist struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner struct {
		ID string `json:"id"`
	} `json:"owner"`
	Tracks *struct {
		Total int `json:"total"`
	} `json:"tracks"`
	Items *struct {
		Total int `json:"total"`
	} `json:"items"`
	Images []spotifyImage `json:"images"`
}

func (sp searchPlaylist) trackCount() int {
	if sp.Tracks != nil {
		return sp.Tracks.Total
	}
	if sp.Items != nil {
		return sp.Items.Total
	}
	return 0
}

// SearchAll searches tracks, episodes, albums, artists, and playlists in one
// query. limit is the per-type result count, clamped to Spotify's accepted
// range of 1..10 (the server defaults to 5 when omitted). Episodes are merged
// into Tracks after music tracks with their spotify:episode: URI preserved
// (same as SearchTracks). Playlist rows leave Section empty (the UI's search
// tabs provide grouping) and set Owned when the owner is the current user
// (best-effort, like Playlists()).
// Implements provider.MultiSearcher.
func (p *SpotifyProvider) SearchAll(ctx context.Context, query string, limit int) (provider.SearchResults, error) {
	if err := p.ensureSession(); err != nil {
		return provider.SearchResults{}, err
	}

	if limit < 1 {
		limit = 1
	} else if limit > 10 {
		limit = 10
	}
	return p.search(ctx, query, "track,album,artist,playlist,episode", limit, 0)
}

// spotifySearchResult is a decoded /v1/search response.
type spotifySearchResult struct {
	Tracks struct {
		Items []*spotifyItem `json:"items"`
	} `json:"tracks"`
	Episodes struct {
		Items []*spotifyItem `json:"items"`
	} `json:"episodes"`
	Albums struct {
		Items []spotifyAlbum `json:"items"`
	} `json:"albums"`
	Artists struct {
		Items []spotifyArtist `json:"items"`
	} `json:"artists"`
	Playlists struct {
		Items []searchPlaylist `json:"items"`
	} `json:"playlists"`
}

// search runs one /v1/search request for the given comma-separated types and
// converts every result kind present. Callers clamp limit to the Development
// Mode maximum.
func (p *SpotifyProvider) search(ctx context.Context, query, types string, limit, offset int) (provider.SearchResults, error) {
	raw, err := p.searchRaw(ctx, query, types, limit, offset)
	if err != nil {
		return provider.SearchResults{}, err
	}
	return p.convertSearch(ctx, raw), nil
}

func (p *SpotifyProvider) searchRaw(ctx context.Context, query, types string, limit, offset int) (spotifySearchResult, error) {
	// No market parameter: with a user OAuth token Spotify implicitly scopes
	// results to the account's country (same as SearchTracks).
	q := url.Values{
		"q":     {query},
		"type":  {types},
		"limit": {strconv.Itoa(limit)},
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}

	resp, err := p.webAPI(ctx, "GET", "/v1/search", q)
	if err != nil {
		return spotifySearchResult{}, friendlySearchError(err)
	}
	var result spotifySearchResult
	if err := decodeBody(resp, &result); err != nil {
		return spotifySearchResult{}, fmt.Errorf("spotify: parse search: %w", err)
	}
	return result, nil
}

// convertSearch maps a decoded search response to provider results.
func (p *SpotifyProvider) convertSearch(ctx context.Context, result spotifySearchResult) provider.SearchResults {
	var out provider.SearchResults
	for _, items := range [][]*spotifyItem{result.Tracks.Items, result.Episodes.Items} {
		for _, t := range items {
			if t == nil || t.ID == "" {
				continue // skip null/unavailable results
			}
			out.Tracks = append(out.Tracks, trackFromItem(t))
		}
	}
	// Spotify returns null entries for unavailable items; they decode to zero
	// values with no ID.
	for _, a := range result.Albums.Items {
		if a.ID != "" {
			out.Albums = append(out.Albums, albumFromSpotify(a))
		}
	}
	for _, a := range result.Artists.Items {
		if a.ID != "" {
			out.Artists = append(out.Artists, artistFromSpotify(a))
		}
	}
	if len(result.Playlists.Items) == 0 {
		return out
	}
	userID := p.currentUserID(ctx)
	for _, pl := range result.Playlists.Items {
		if pl.ID == "" {
			continue
		}
		out.Playlists = append(out.Playlists, playlist.PlaylistInfo{
			ID:         pl.ID,
			Name:       pl.Name,
			TrackCount: pl.trackCount(),
			Owned:      userID != "" && pl.Owner.ID == userID,
			ImageURL:   pickCoverImage(pl.Images),
		})
	}
	return out
}

// friendlySearchError rewrites Spotify's misleading 400 "Invalid limit" reply
// from /v1/search into something a user can act on. Since Nov 27, 2024 Spotify
// returns this error for developer apps registered in Development Mode — the
// rest of the API (playback, playlists, library) keeps working, but the
// catalog endpoints (/v1/search etc.) are blocked. The limit value is fine.
func friendlySearchError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "400") && strings.Contains(msg, "Invalid limit") {
		return fmt.Errorf("spotify: search blocked — your client_id is too new. Spotify's Nov 27 2024 change blocks /v1/search for apps in Development Mode (the rest of cliamp still works on your app). Remove client_id from [spotify] in config.toml to use the built-in fallback for search, or apply for Extended Quota Mode")
	}
	return fmt.Errorf("spotify: search: %w", err)
}

// devModeSearchLimit is the largest per-request limit /v1/search accepts for an
// app in Spotify's Development Mode. SearchTracks uses it for every request so
// personal client IDs never need a rejected probe before pagination starts.
const devModeSearchLimit = 10

func isInvalidLimit(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.status == http.StatusBadRequest && strings.Contains(apiErr.message, "Invalid limit")
}

// spotifySearchPage is one page of /v1/search results.
type spotifySearchPage struct {
	Albums struct {
		Items []*spotifyAlbumItem `json:"items"`
	} `json:"albums"`
	Tracks struct {
		Items []*spotifyItem `json:"items"`
	} `json:"tracks"`
	Episodes struct {
		Items []*spotifyItem `json:"items"`
	} `json:"episodes"`
}

// searchPage runs a single /v1/search request.
//
// No market parameter: when the request carries a user OAuth token, Spotify
// implicitly scopes results to the account's country.
func (p *SpotifyProvider) searchPage(ctx context.Context, query string, limit, offset int) (*spotifySearchPage, error) {
	q := url.Values{
		"q":     {query},
		"type":  {"album,track,episode"},
		"limit": {strconv.Itoa(limit)},
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}

	resp, err := p.webAPI(ctx, "GET", "/v1/search", q)
	if err != nil {
		return nil, err
	}

	var page spotifySearchPage
	if err := decodeBody(resp, &page); err != nil {
		return nil, fmt.Errorf("spotify: parse search: %w", err)
	}
	return &page, nil
}

// searchPaged collects up to limit results in pages of devModeSearchLimit, for
// apps that cannot ask for more in one go. Any page failure aborts the search so
// callers never mistake partial results for a complete response.
func (p *SpotifyProvider) searchPaged(ctx context.Context, query string, limit int) (*spotifySearchPage, error) {
	combined := &spotifySearchPage{}
	for offset := 0; offset < limit; offset += devModeSearchLimit {
		size := min(devModeSearchLimit, limit-offset)
		page, err := p.searchPage(ctx, query, size, offset)
		if err != nil {
			return nil, fmt.Errorf("page at offset %d: %w", offset, err)
		}
		combined.Albums.Items = append(combined.Albums.Items, page.Albums.Items...)
		combined.Tracks.Items = append(combined.Tracks.Items, page.Tracks.Items...)
		combined.Episodes.Items = append(combined.Episodes.Items, page.Episodes.Items...)
		// Every result kind exhausted, so further pages are empty.
		if len(page.Albums.Items) < size && len(page.Tracks.Items) < size && len(page.Episodes.Items) < size {
			break
		}
	}
	return combined, nil
}

// SearchTracks searches Spotify for albums, tracks and podcast episodes,
// returning up to limit results of each. Episodes (e.g. podcasts) are routed
// through their spotify:episode: URI so they play correctly.
// limit is clamped to Spotify's accepted range of 1..50.
//
// Album hits lead the results as album placeholders (playlist.Track.IsAlbum),
// because a query is usually an artist or record name and the album is the
// more useful answer than whichever of its tracks Spotify ranks highest. They
// are not playable as-is: the caller expands the chosen one with AlbumTracks.
//
// Apps in Development Mode cap /v1/search at devModeSearchLimit results per
// request, so larger result sets always use offset pagination.
func (p *SpotifyProvider) SearchTracks(ctx context.Context, query string, limit int) ([]playlist.Track, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}

	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}

	var result *spotifySearchPage
	var err error
	if p.clientID == DefaultClientID {
		// Preserve one-request searches for the shared legacy client. Fall back
		// to Development Mode pages if Spotify applies the new cap to it later.
		result, err = p.searchPage(ctx, query, limit, 0)
		if err != nil && isInvalidLimit(err) && limit > devModeSearchLimit {
			result, err = p.searchPaged(ctx, query, limit)
		}
	} else {
		result, err = p.searchPaged(ctx, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("spotify: search: %w", err)
	}

	var tracks []playlist.Track
	for _, a := range result.Albums.Items {
		if a == nil || a.ID == "" {
			continue // skip null/unavailable results
		}
		tracks = append(tracks, albumFromItem(a))
	}
	for _, items := range [][]*spotifyItem{result.Tracks.Items, result.Episodes.Items} {
		for _, t := range items {
			if t == nil || t.ID == "" {
				continue // skip null/unavailable results
			}
			tracks = append(tracks, trackFromItem(t))
		}
	}
	return tracks, nil
}
