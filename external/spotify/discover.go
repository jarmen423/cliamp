package spotify

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/provider"
)

// Compile-time interface checks.
var (
	_ provider.CategoryBrowser     = (*SpotifyProvider)(nil)
	_ provider.BrowseEntryProvider = (*SpotifyProvider)(nil)
)

// The Browse page mirrors Spotify's Browse tab with what a Development Mode
// app can reach: the category list and new releases from /v1/browse, category
// contents from /v1/search (category playlists are blocked), and Spotify's
// editorial playlists through the spclient fallback in resolve.go.

const (
	browseEntryID        = "spotify:browse"
	madeForYouCategoryID = "cliamp:made-for-you"
	newReleasesID        = "cliamp:new-releases"
	// builtinCategoryPrefix marks the IDs of fallback genres used when
	// /v1/browse/categories is unavailable.
	builtinCategoryPrefix = "cliamp:genre:"
	// spotifyOwnerID owns Spotify's editorial and algorithmic playlists.
	spotifyOwnerID = "spotify"

	categoryGroupForYou = "For you"
	categoryGroupGenres = "Genres & moods"

	categoryCacheTTL    = time.Hour
	categoryPageTTL     = 5 * time.Minute
	browseCategoryPage  = 50
	newReleasesLimit    = 50
	categoryPlaylistMax = 20
	madeForYouTracks    = 50
)

// builtinCategories stands in for /v1/browse/categories, which Spotify lists
// as removed for Development Mode apps even though many still reach it.
var builtinCategories = []string{
	"Pop", "Hip-Hop", "Rock", "Dance/Electronic", "R&B", "Indie", "Country",
	"Latin", "K-pop", "Afro", "Jazz", "Classical", "Metal", "Soul", "Funk",
	"Folk & Acoustic", "Blues", "Punk", "Reggae", "Ambient", "Chill", "Mood",
	"Workout", "Focus", "Sleep", "Party", "Romance", "Decades",
}

// skippedCategories are API categories with no searchable music behind them:
// personal or podcast pages, and the two sections the "For you" group covers.
var skippedCategories = []string{
	"made for you", "new releases", "discover", "live events", "podcast", "audiobook",
}

// BrowseEntries adds the Browse page to the provider pane after the Library
// section. Implements provider.BrowseEntryProvider.
func (p *SpotifyProvider) BrowseEntries() []provider.BrowseEntry {
	return []provider.BrowseEntry{{
		ID:           browseEntryID,
		Name:         "Browse",
		Section:      "Discover",
		Mode:         provider.BrowseCategories,
		AfterSection: "Library",
	}}
}

// BrowseCategories returns the "For you" shortcuts followed by Spotify's
// browse categories, or the built-in genre list when the endpoint fails.
// Implements provider.CategoryBrowser.
func (p *SpotifyProvider) BrowseCategories(ctx context.Context) ([]provider.CategoryInfo, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.categories != nil && time.Since(p.categoriesAt) < categoryCacheTTL {
		cats := slices.Clone(p.categories)
		p.mu.Unlock()
		return cats, nil
	}
	p.mu.Unlock()

	cats := []provider.CategoryInfo{
		{ID: madeForYouCategoryID, Name: "Made for you", Group: categoryGroupForYou},
		{ID: newReleasesID, Name: "New releases", Group: categoryGroupForYou},
	}
	api, err := p.fetchBrowseCategories(ctx)
	if err != nil {
		applog.Warn("spotify: browse categories unavailable, using built-in genres: %v", err)
		for _, name := range builtinCategories {
			cats = append(cats, provider.CategoryInfo{ID: builtinCategoryPrefix + name, Name: name, Group: categoryGroupGenres})
		}
	} else {
		cats = append(cats, api...)
	}

	p.mu.Lock()
	p.categories = cats
	p.categoriesAt = time.Now()
	p.mu.Unlock()
	return slices.Clone(cats), nil
}

// fetchBrowseCategories pages through /v1/browse/categories.
func (p *SpotifyProvider) fetchBrowseCategories(ctx context.Context) ([]provider.CategoryInfo, error) {
	var out []provider.CategoryInfo
	for offset := 0; ; offset += browseCategoryPage {
		q := url.Values{
			"limit":  {strconv.Itoa(browseCategoryPage)},
			"offset": {strconv.Itoa(offset)},
		}
		resp, err := p.webAPI(ctx, "GET", "/v1/browse/categories", q)
		if err != nil {
			return nil, fmt.Errorf("spotify: browse categories: %w", err)
		}
		var page struct {
			Categories struct {
				Items []struct {
					ID    string         `json:"id"`
					Name  string         `json:"name"`
					Icons []spotifyImage `json:"icons"`
				} `json:"items"`
				Next string `json:"next"`
			} `json:"categories"`
		}
		if err := decodeBody(resp, &page); err != nil {
			return nil, fmt.Errorf("spotify: parse browse categories: %w", err)
		}
		for _, c := range page.Categories.Items {
			if c.ID == "" || c.Name == "" || skipCategory(c.Name) {
				continue
			}
			out = append(out, provider.CategoryInfo{
				ID:       c.ID,
				Name:     c.Name,
				Group:    categoryGroupGenres,
				ImageURL: pickCoverImage(c.Icons),
			})
		}
		if page.Categories.Next == "" || len(page.Categories.Items) == 0 {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("spotify: browse categories: empty list")
	}
	return out, nil
}

func skipCategory(name string) bool {
	lower := strings.ToLower(name)
	return slices.ContainsFunc(skippedCategories, func(s string) bool {
		return strings.Contains(lower, s)
	})
}

// CategoryContent returns one category's page as multi-type results, cached
// briefly so stepping in and out of categories does not refetch them.
// Implements provider.CategoryBrowser.
func (p *SpotifyProvider) CategoryContent(ctx context.Context, cat provider.CategoryInfo) (provider.SearchResults, error) {
	if err := p.ensureSession(); err != nil {
		return provider.SearchResults{}, err
	}
	p.mu.Lock()
	if page, ok := p.categoryPages[cat.ID]; ok && time.Since(page.at) < categoryPageTTL {
		p.mu.Unlock()
		return page.results, nil
	}
	p.mu.Unlock()

	var res provider.SearchResults
	var err error
	switch cat.ID {
	case madeForYouCategoryID:
		res, err = p.madeForYou(ctx)
	case newReleasesID:
		res, err = p.newReleases(ctx)
	default:
		res, err = p.categorySearch(ctx, cat.Name)
	}
	if err != nil {
		return provider.SearchResults{}, err
	}
	p.mu.Lock()
	if p.categoryPages == nil {
		p.categoryPages = make(map[string]categoryPage)
	}
	p.categoryPages[cat.ID] = categoryPage{results: res, at: time.Now()}
	p.mu.Unlock()
	return res, nil
}

// categoryPage is a cached CategoryContent result; callers treat it as
// read-only.
type categoryPage struct {
	results provider.SearchResults
	at      time.Time
}

// categorySearch fills a category page from /v1/search, since Development
// Mode blocks /v1/browse/categories/{id}/playlists: playlists matching the
// category name, Spotify's own first, plus artists and tracks tagged with it
// as a genre. A name that is not a genre (moods, activities) falls back to a
// plain search for artists and tracks.
func (p *SpotifyProvider) categorySearch(ctx context.Context, name string) (provider.SearchResults, error) {
	var editorial, others []searchPlaylist
	seen := make(map[string]bool)
	var firstErr error
	for offset := 0; offset < categoryPlaylistMax; offset += devModeSearchLimit {
		raw, err := p.searchRaw(ctx, name, "playlist", devModeSearchLimit, offset)
		if err != nil {
			firstErr = err
			break
		}
		items := raw.Playlists.Items
		for _, pl := range items {
			if pl.ID == "" || seen[pl.ID] {
				continue
			}
			seen[pl.ID] = true
			if pl.Owner.ID == spotifyOwnerID {
				editorial = append(editorial, pl)
			} else {
				others = append(others, pl)
			}
		}
		// Null rows make a short page ambiguous, so only an empty one ends
		// the run early.
		if len(items) == 0 {
			break
		}
	}
	var lists spotifySearchResult
	lists.Playlists.Items = append(editorial, others...)
	out := p.convertSearch(ctx, lists)

	genre := fmt.Sprintf("genre:%q", strings.ToLower(name))
	res, err := p.search(ctx, genre, "artist,track", devModeSearchLimit, 0)
	if err == nil && len(res.Artists) == 0 && len(res.Tracks) == 0 {
		res, err = p.search(ctx, name, "artist,track", devModeSearchLimit, 0)
	}
	if err != nil && firstErr == nil {
		firstErr = err
	}
	out.Artists = res.Artists
	out.Tracks = res.Tracks

	if len(out.Playlists) == 0 && len(out.Artists) == 0 && len(out.Tracks) == 0 && firstErr != nil {
		return provider.SearchResults{}, firstErr
	}
	return out, nil
}

// madeForYou builds a personal page from the user's listening: recommended
// tracks (top tracks mixed with top artists' new albums) and top artists.
func (p *SpotifyProvider) madeForYou(ctx context.Context) (provider.SearchResults, error) {
	var out provider.SearchResults
	top, artistsErr := p.fetchRecommendTopArtists(ctx)
	for _, a := range top {
		if a.ID != "" {
			out.Artists = append(out.Artists, artistFromSpotify(a))
		}
	}
	tracks, tracksErr := p.recommendTracks(ctx, nil, madeForYouTracks, top)
	out.Tracks = tracks
	if tracksErr != nil && artistsErr != nil {
		return provider.SearchResults{}, tracksErr
	}
	return out, nil
}

// newReleases lists /v1/browse/new-releases, paging in search-sized steps
// when the app's mode rejects a full page. A failed later page keeps the
// albums already loaded.
func (p *SpotifyProvider) newReleases(ctx context.Context) (provider.SearchResults, error) {
	albums, err := p.fetchNewReleases(ctx, newReleasesLimit, 0)
	if isInvalidLimit(err) {
		albums, err = nil, nil
		for offset := 0; offset < newReleasesLimit; offset += devModeSearchLimit {
			page, pageErr := p.fetchNewReleases(ctx, devModeSearchLimit, offset)
			if pageErr != nil {
				if len(albums) == 0 {
					err = pageErr
				}
				break
			}
			if len(page) == 0 {
				break
			}
			albums = append(albums, page...)
		}
	}
	if err != nil {
		return provider.SearchResults{}, err
	}
	return provider.SearchResults{Albums: albums}, nil
}

func (p *SpotifyProvider) fetchNewReleases(ctx context.Context, limit, offset int) ([]provider.AlbumInfo, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	resp, err := p.webAPI(ctx, "GET", "/v1/browse/new-releases", q)
	if err != nil {
		return nil, fmt.Errorf("spotify: new releases: %w", err)
	}
	var result struct {
		Albums struct {
			Items []spotifyAlbum `json:"items"`
		} `json:"albums"`
	}
	if err := decodeBody(resp, &result); err != nil {
		return nil, fmt.Errorf("spotify: parse new releases: %w", err)
	}
	albums := make([]provider.AlbumInfo, 0, len(result.Albums.Items))
	for _, a := range result.Albums.Items {
		if a.ID != "" {
			albums = append(albums, albumFromSpotify(a))
		}
	}
	return albums, nil
}
