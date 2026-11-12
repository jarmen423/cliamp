package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// savedShows returns the user's saved podcast shows as playlist rows in
// the Podcasts section. Best-effort, unlike the playlist rows proper: a
// failure (for example an app Spotify bars from the shows endpoints)
// omits the section instead of failing the whole library listing.
func (p *SpotifyProvider) savedShows() []playlist.PlaylistInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := p.fetchSavedShows(ctx)
	if err != nil {
		applog.Debug("spotify: saved shows unavailable: %v", err)
		return nil
	}
	return rows
}

func (p *SpotifyProvider) fetchSavedShows(ctx context.Context) ([]playlist.PlaylistInfo, error) {
	var rows []playlist.PlaylistInfo
	for offset := 0; ; offset += spotifyTrackPageSize {
		query := url.Values{"limit": {strconv.Itoa(spotifyTrackPageSize)}, "offset": {strconv.Itoa(offset)}}
		resp, err := p.webAPI(ctx, "GET", "/v1/me/shows", query)
		if err != nil {
			return nil, fmt.Errorf("spotify: list saved shows: %w", err)
		}
		var result struct {
			Items []struct {
				Show *spotifyShow `json:"show"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := decodeBody(resp, &result); err != nil {
			return nil, fmt.Errorf("spotify: parse saved shows: %w", err)
		}
		for _, item := range result.Items {
			if item.Show == nil || item.Show.ID == "" {
				continue
			}
			rows = append(rows, playlist.PlaylistInfo{
				ID:         savedShowIDPrefix + item.Show.ID,
				Name:       item.Show.Name,
				TrackCount: item.Show.TotalEpisodes,
				Section:    savedShowSection,
				ImageURL:   pickCoverImage(item.Show.Images),
			})
		}
		if len(result.Items) == 0 || offset+spotifyTrackPageSize >= result.Total {
			return rows, nil
		}
	}
}

// spotifyShow is the show object shared by /v1/me/shows and /v1/shows/{id}.
type spotifyShow struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	TotalEpisodes int            `json:"total_episodes"`
	Images        []spotifyImage `json:"images"`
}

// showHeader reads a show's name and image: opening a show link shows the
// name before any episode loads.
func (p *SpotifyProvider) showHeader(ctx context.Context, showID string) (name, image string) {
	resp, err := p.webAPI(ctx, "GET", "/v1/shows/"+showID, url.Values{"fields": {"name,images"}})
	if err != nil {
		return "", ""
	}
	var show spotifyShow
	if err := decodeBody(resp, &show); err != nil {
		return "", ""
	}
	return show.Name, pickCoverImage(show.Images)
}

// showEpisodes returns every episode of a show, newest first as Spotify
// orders them. The show name fills the artist and album slots, as it does
// for episodes met in playlists.
func (p *SpotifyProvider) showEpisodes(showID string) ([]playlist.Track, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name, image := p.showHeader(ctx, showID)
	var tracks []playlist.Track
	for offset := 0; ; offset += spotifyTrackPageSize {
		query := url.Values{"limit": {strconv.Itoa(spotifyTrackPageSize)}, "offset": {strconv.Itoa(offset)}}
		resp, err := p.webAPI(ctx, "GET", "/v1/shows/"+showID+"/episodes", query)
		if err != nil {
			return nil, fmt.Errorf("spotify: list episodes: %w", err)
		}
		var result struct {
			Items []struct {
				ID          string         `json:"id"`
				Name        string         `json:"name"`
				DurationMS  int            `json:"duration_ms"`
				ReleaseDate string         `json:"release_date"`
				Images      []spotifyImage `json:"images"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := decodeBody(resp, &result); err != nil {
			return nil, fmt.Errorf("spotify: parse episodes: %w", err)
		}
		for _, ep := range result.Items {
			if ep.ID == "" {
				continue
			}
			art := pickCoverImage(ep.Images)
			if art == "" {
				art = image
			}
			tracks = append(tracks, playlist.Track{
				Path:         "spotify:episode:" + ep.ID,
				Title:        ep.Name,
				Artist:       name,
				Album:        name,
				AlbumArtURL:  art,
				Year:         provider.YearFromDate(ep.ReleaseDate),
				DurationSecs: ep.DurationMS / 1000,
			})
		}
		if len(result.Items) == 0 || offset+spotifyTrackPageSize >= result.Total {
			return tracks, nil
		}
	}
}
