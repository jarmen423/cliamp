package spotify

// radio.go builds radio stations: the tracks Spotify itself plays around a
// track, artist, album or playlist. The Web API has no station endpoint, so
// the station comes from the session's context resolver (the one autoplay
// uses), which lists track URIs only; spclient's extended metadata then fills
// in the tracks.

import (
	"context"
	"errors"
	"fmt"
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
	if isStationNotFound(err) {
		return nil, fmt.Errorf("spotify: radio: %s: %w", contextURI, provider.ErrNoStation)
	}
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

// isStationNotFound reports whether the autoplay resolver answered 404,
// which is how Spotify says it builds no station for a context (every
// R. Kelly track and his artist page, for one). go-librespot reports the
// status only in its error text.
func isStationNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "context resolve autoplay: 404")
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
	if len(uris) == 0 {
		return nil, fmt.Errorf("spotify: radio: %s: empty station: %w", contextURI, provider.ErrNoStation)
	}

	uris = stationTracks(uris, limit)
	byURI, err := itemMetadata(sess, ctx, uris)
	if err != nil {
		return nil, fmt.Errorf("spotify: radio: %w", err)
	}
	tracks := make([]playlist.Track, 0, len(uris))
	for _, uri := range uris {
		if t, ok := byURI[uri]; ok { // the catalog may no longer have it
			tracks = append(tracks, t)
		}
	}
	return tracks, nil
}

// stationTracks keeps the first limit distinct track URIs of a station,
// in order, dropping episodes and anything else that is not a track.
func stationTracks(uris []string, limit int) []string {
	seen := make(map[string]bool, limit)
	out := make([]string, 0, limit)
	for _, uri := range uris {
		if len(out) == limit {
			break
		}
		if typ, _, ok := splitSpotifyURI(uri); ok && typ == "track" && !seen[uri] {
			seen[uri] = true
			out = append(out, uri)
		}
	}
	return out
}
