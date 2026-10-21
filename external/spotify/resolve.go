package spotify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	extmetadatapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	"github.com/devgianlu/go-librespot/spclient"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/playlist"
)

const (
	// maxResolvedTracks bounds how much of a playlist the spclient fallback
	// loads. Editorial playlists run 50-100 tracks; this only caps outliers.
	maxResolvedTracks = 1000
	// extMetadataBatch is the entity count per extended-metadata request.
	extMetadataBatch = 50
	// spotifyImageBase turns a metadata image file ID into a CDN URL.
	spotifyImageBase = "https://i.scdn.co/image/"
)

// isForeignPlaylistErr reports whether the Web API refused to list a
// playlist's items: Development Mode apps get 403 for other users' playlists
// and 404 for Spotify's editorial and algorithmic ones.
func isForeignPlaylistErr(playlistID string, err error) bool {
	if playlistID == savedTracksPlaylistID {
		return false
	}
	return hasStatus(err, http.StatusForbidden) || hasStatus(err, http.StatusNotFound)
}

// resolveForeignPlaylist loads a playlist the Web API will not list and caches
// it like any fully loaded list. A failed fallback reports the original Web
// API error, which carries the actionable hint; its own error is only logged.
func (p *SpotifyProvider) resolveForeignPlaylist(ctx context.Context, playlistID string, apiErr error) ([]playlist.Track, error) {
	resolve := p.resolvePlaylist
	if resolve == nil {
		p.mu.Lock()
		sess := p.session
		p.mu.Unlock()
		if sess == nil {
			return nil, apiErr
		}
		resolve = sess.resolvePlaylistTracks
	}
	tracks, err := resolve(ctx, playlistID)
	if err != nil {
		applog.Warn("spotify: resolve playlist %s over spclient: %v", playlistID, err)
		return nil, apiErr
	}
	p.mu.Lock()
	p.cacheTracksLocked(playlistID, tracks, len(tracks))
	p.mu.Unlock()
	return slices.Clone(tracks), nil
}

// resolvePlaylistTracks loads a playlist through spclient's context-resolve
// (the endpoint Spotify Connect uses), then fills track metadata with batched
// extended-metadata lookups. Neither call goes through the Web API, so this
// reaches the editorial playlists Development Mode apps cannot read.
func (s *Session) resolvePlaylistTracks(ctx context.Context, playlistID string) ([]playlist.Track, error) {
	sess := s.innerSession()
	if sess == nil {
		return nil, fmt.Errorf("spotify: resolve playlist: %w", playlist.ErrNeedsAuth)
	}
	sp := sess.Spclient()
	uris, err := resolveContextURIs(ctx, sp, "spotify:playlist:"+playlistID, maxResolvedTracks)
	if err != nil {
		return nil, err
	}
	return fetchItemMetadata(ctx, sp, uris)
}

// resolveContextURIs returns up to limit track and episode URIs of a context,
// following the page_url / next_page_url chain context-resolve hands back.
// spclient.ContextResolver walks the same chain, but its Page indexes the last
// page without a bounds check and panics on a context with no pages.
func resolveContextURIs(ctx context.Context, sp *spclient.Spclient, uri string, limit int) ([]string, error) {
	cx, err := sp.ContextResolve(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("spotify: context resolve %s: %w", uri, err)
	}
	if cx.GetLoading() {
		return nil, fmt.Errorf("spotify: context resolve %s: still loading", uri)
	}
	pages := cx.GetPages()
	var uris []string
	for i := 0; i < len(pages) && len(uris) < limit; i++ {
		page := pages[i]
		if len(page.GetTracks()) == 0 && page.GetPageUrl() != "" {
			if page, err = loadContextPage(ctx, sp, page.GetPageUrl()); err != nil {
				return nil, err
			}
		}
		for _, t := range page.GetTracks() {
			if typ, _, ok := splitSpotifyURI(t.GetUri()); ok && (typ == "track" || typ == "episode") {
				uris = append(uris, t.GetUri())
			}
		}
		if i == len(pages)-1 && page.GetNextPageUrl() != "" {
			next, err := loadContextPage(ctx, sp, page.GetNextPageUrl())
			if err != nil {
				return nil, err
			}
			pages = append(pages, next)
		}
	}
	if len(uris) > limit {
		uris = uris[:limit]
	}
	return uris, nil
}

// loadContextPage fetches one context page by its hm:// URL.
func loadContextPage(ctx context.Context, sp *spclient.Spclient, hmURL string) (*connectpb.ContextPage, error) {
	resp, err := sp.RequestHm(ctx, http.MethodGet, hmURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("spotify: context page: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("spotify: context page: status %d", resp.StatusCode)
	}
	var page connectpb.ContextPage
	if err := decodeBody(resp, &page); err != nil {
		return nil, fmt.Errorf("spotify: parse context page: %w", err)
	}
	return &page, nil
}

// itemMetadata looks up tracks and episodes by URI. The Web API's batch
// /v1/tracks and /v1/episodes lookups are gone for Development Mode apps
// since February 2026, so callers go through spclient instead. A var so
// tests can stand in for the live session.
var itemMetadata = (*Session).lookupItemMetadata

// lookupItemMetadata is fetchItemMetadataByURI over the session's spclient.
func (s *Session) lookupItemMetadata(ctx context.Context, uris []string) (map[string]playlist.Track, error) {
	sess := s.innerSession()
	if sess == nil {
		return nil, fmt.Errorf("spotify: item metadata: %w", playlist.ErrNeedsAuth)
	}
	return fetchItemMetadataByURI(ctx, sess.Spclient(), uris)
}

// fetchItemMetadata looks up tracks and episodes in batches and returns them
// in uris order. Items whose metadata is unavailable are dropped.
func fetchItemMetadata(ctx context.Context, sp *spclient.Spclient, uris []string) ([]playlist.Track, error) {
	byURI, err := fetchItemMetadataByURI(ctx, sp, uris)
	if err != nil {
		return nil, err
	}
	tracks := make([]playlist.Track, 0, len(byURI))
	for _, u := range uris {
		if t, ok := byURI[u]; ok {
			tracks = append(tracks, t)
		}
	}
	return tracks, nil
}

// fetchItemMetadataByURI looks up tracks and episodes in batches, keyed by
// URI. It errors when none of the requested items came back.
func fetchItemMetadataByURI(ctx context.Context, sp *spclient.Spclient, uris []string) (map[string]playlist.Track, error) {
	byURI := make(map[string]playlist.Track, len(uris))
	for start := 0; start < len(uris); start += extMetadataBatch {
		chunk := uris[start:min(start+extMetadataBatch, len(uris))]
		req := &extmetadatapb.BatchedEntityRequest{}
		for _, u := range chunk {
			kind := extmetadatapb.ExtensionKind_TRACK_V4
			if typ, _, _ := splitSpotifyURI(u); typ == "episode" {
				kind = extmetadatapb.ExtensionKind_EPISODE_V4
			}
			req.EntityRequest = append(req.EntityRequest, &extmetadatapb.EntityRequest{
				EntityUri: u,
				Query:     []*extmetadatapb.ExtensionQuery{{ExtensionKind: kind}},
			})
		}
		resp, err := sp.ExtendedMetadata(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("spotify: extended metadata: %w", err)
		}
		for _, item := range resp.GetExtendedMetadata() {
			for _, d := range item.GetExtensionData() {
				if t, ok := trackFromExtension(item.GetExtensionKind(), d); ok {
					byURI[d.GetEntityUri()] = t
				}
			}
		}
	}
	if len(uris) > 0 && len(byURI) == 0 {
		return nil, errors.New("spotify: extended metadata returned no items")
	}
	return byURI, nil
}

// trackFromExtension decodes one extended-metadata entry into a track.
func trackFromExtension(kind extmetadatapb.ExtensionKind, d *extmetadatapb.EntityExtensionData) (playlist.Track, bool) {
	if d.GetHeader().GetStatusCode() != http.StatusOK || d.GetExtensionData() == nil {
		return playlist.Track{}, false
	}
	switch kind {
	case extmetadatapb.ExtensionKind_TRACK_V4:
		var t metadatapb.Track
		if d.GetExtensionData().UnmarshalTo(&t) == nil {
			return trackFromMetadata(d.GetEntityUri(), &t), true
		}
	case extmetadatapb.ExtensionKind_EPISODE_V4:
		var e metadatapb.Episode
		if d.GetExtensionData().UnmarshalTo(&e) == nil {
			return episodeFromMetadata(d.GetEntityUri(), &e), true
		}
	}
	return playlist.Track{}, false
}

// trackFromMetadata mirrors trackFromItem for spclient track metadata.
func trackFromMetadata(uri string, t *metadatapb.Track) playlist.Track {
	names := make([]string, 0, len(t.GetArtist()))
	ids := make([]string, 0, len(t.GetArtist()))
	for _, a := range t.GetArtist() {
		names = append(names, a.GetName())
		if len(a.GetGid()) > 0 {
			ids = append(ids, librespot.SpotifyIdFromGid(librespot.SpotifyIdTypeArtist, a.GetGid()).Base62())
		}
	}
	meta := map[string]string{}
	album := t.GetAlbum()
	if len(album.GetGid()) > 0 {
		meta[playlist.MetaAlbumID] = librespot.SpotifyIdFromGid(librespot.SpotifyIdTypeAlbum, album.GetGid()).Base62()
	}
	if len(ids) > 0 {
		meta[metaSpotifyArtistIDs] = strings.Join(ids, ",")
	}
	return playlist.Track{
		Path:         uri,
		Title:        t.GetName(),
		Artist:       strings.Join(names, ", "),
		Album:        album.GetName(),
		AlbumArtURL:  coverFromImageGroup(album.GetCoverGroup()),
		Year:         int(album.GetDate().GetYear()),
		DurationSecs: int(t.GetDuration()) / 1000,
		TrackNumber:  int(t.GetNumber()),
		ProviderMeta: meta,
	}
}

// episodeFromMetadata mirrors trackFromItem's episode handling: the show name
// fills the artist and album slots.
func episodeFromMetadata(uri string, e *metadatapb.Episode) playlist.Track {
	show := e.GetShow().GetName()
	art := coverFromImageGroup(e.GetCoverImage())
	if art == "" {
		art = coverFromImageGroup(e.GetShow().GetCoverImage())
	}
	return playlist.Track{
		Path:         uri,
		Title:        e.GetName(),
		Artist:       show,
		Album:        show,
		AlbumArtURL:  art,
		Year:         int(e.GetPublishTime().GetYear()),
		DurationSecs: int(e.GetDuration()) / 1000,
	}
}

// coverFromImageGroup picks the default-size (300px) image, matching
// pickCoverImage's target, falling back to the first image present.
func coverFromImageGroup(g *metadatapb.ImageGroup) string {
	var fallback string
	for _, img := range g.GetImage() {
		if len(img.GetFileId()) == 0 {
			continue
		}
		u := spotifyImageBase + hex.EncodeToString(img.GetFileId())
		if img.GetSize() == metadatapb.Image_DEFAULT {
			return u
		}
		if fallback == "" {
			fallback = u
		}
	}
	return fallback
}
