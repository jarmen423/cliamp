package spotify

// radio.go builds radio stations: the tracks Spotify itself plays around a
// track, artist, album or playlist. The Web API has no station endpoint, so
// the station comes from the session's context resolver (the one autoplay
// uses), which lists track URIs only; the Web API then fills in the tracks.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	playerpb "github.com/devgianlu/go-librespot/proto/spotify/player"
	"google.golang.org/protobuf/proto"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// Compile-time interface check.
var _ provider.RadioBuilder = (*SpotifyProvider)(nil)

// errNoStationSeed means the seed names nothing Spotify builds a station
// around (a synthetic library row with no tracks, say).
var errNoStationSeed = errors.New("spotify: radio: no track, artist, album or playlist to build a station from")

// stationTrackURIs asks Spotify for the station around contextURI and
// returns its first page of track URIs. A var so tests can stand in for the
// live session.
var stationTrackURIs = func(ctx context.Context, s *Session, contextURI string, recent []string) ([]string, error) {
	sess := s.innerSession()
	if sess == nil {
		return nil, fmt.Errorf("spotify: radio: not connected: %w", playlist.ErrNeedsAuth)
	}
	station, err := sess.Spclient().ContextResolveAutoplay(ctx, &playerpb.AutoplayContextRequest{
		ContextUri:     proto.String(contextURI),
		RecentTrackUri: recent,
	})
	if err != nil {
		return nil, fmt.Errorf("spotify: radio: resolve station: %w", err)
	}
	var uris []string
	for _, page := range station.Pages {
		for _, t := range page.Tracks {
			uris = append(uris, t.Uri)
		}
	}
	return uris, nil
}

// isSpotifyID reports whether id is a bare base62 Spotify ID, which rules
// out the synthetic library rows ("YOUR MUSIC").
func isSpotifyID(id string) bool {
	if len(id) != 22 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// stationContextURI is the URI a station is asked for: the seed's artist,
// album or playlist (the seed kinds are Spotify's URI types), or else its
// first track.
func stationContextURI(seed provider.RadioSeed) string {
	kind, id := seed.Kind, seed.ID
	if albumID, ok := isSavedAlbumID(id); ok {
		kind, id = provider.RadioSeedAlbum, albumID // a saved album listed among the playlists
	}
	switch kind {
	case provider.RadioSeedArtist, provider.RadioSeedAlbum, provider.RadioSeedPlaylist:
		if isSpotifyID(id) {
			return "spotify:" + kind + ":" + id
		}
	}
	for _, t := range seed.Tracks {
		if id := spotifyTrackID(t); id != "" {
			return "spotify:track:" + id
		}
	}
	return ""
}

// RadioTracks returns up to limit tracks of the station Spotify builds
// around seed, at most one station page (50). Implements
// provider.RadioBuilder.
func (p *SpotifyProvider) RadioTracks(ctx context.Context, seed provider.RadioSeed, limit int) ([]playlist.Track, error) {
	if limit < 1 {
		return nil, nil
	}
	contextURI := stationContextURI(seed)
	if contextURI == "" {
		return nil, errNoStationSeed
	}
	if err := p.ensureSession(); err != nil {
		return nil, err
	}
	var recent []string
	for _, t := range seed.Tracks {
		if uri := spotifyTrackURI(t); uri != "" {
			recent = append(recent, uri)
		}
	}

	p.mu.Lock()
	sess := p.session
	p.mu.Unlock()
	uris, err := stationTrackURIs(ctx, sess, contextURI, recent)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, limit)
	for _, uri := range uris {
		if len(ids) == limit {
			break
		}
		if typ, id, ok := splitSpotifyURI(uri); ok && typ == "track" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return p.tracksByID(ctx, ids)
}

// tracksByID loads full track objects for ids, in order, skipping any the
// catalog no longer has.
func (p *SpotifyProvider) tracksByID(ctx context.Context, ids []string) ([]playlist.Track, error) {
	tracks := make([]playlist.Track, 0, len(ids))
	for start := 0; start < len(ids); start += connectEnrichBatch {
		chunk := ids[start:min(start+connectEnrichBatch, len(ids))]
		resp, err := p.webAPI(ctx, "GET", "/v1/tracks", url.Values{"ids": {strings.Join(chunk, ",")}})
		if err != nil {
			return nil, fmt.Errorf("spotify: tracks: %w", err)
		}
		var result struct {
			Tracks []*spotifyItem `json:"tracks"`
		}
		if err := decodeBody(resp, &result); err != nil {
			return nil, fmt.Errorf("spotify: parse tracks: %w", err)
		}
		for _, item := range result.Tracks {
			if item == nil || item.ID == "" {
				continue
			}
			tracks = append(tracks, trackFromItem(item))
		}
	}
	return tracks, nil
}
