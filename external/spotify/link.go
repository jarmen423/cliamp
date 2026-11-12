package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// Compile-time interface checks.
var (
	_ provider.LinkResolver    = (*SpotifyProvider)(nil)
	_ provider.AccountSwitcher = (*SpotifyProvider)(nil)
)

// A share link arrives as an open.spotify.com URL (with an optional locale
// segment such as /intl-en/), as a spotify: URI, or embedded in shared text
// ("Check this out https://open.spotify.com/playlist/…"). Both patterns
// search the whole text, so tracking parameters (si, utm, pi) never reach
// the ID.
var (
	spotifyLinkURL = regexp.MustCompile(`(?:https?://)?open\.spotify\.com/(?:intl-[a-z]{2}(?:-[A-Z]{2})?/)?(?:embed/)?(playlist|album|track|artist|episode|show)/([A-Za-z0-9]+)`)
	spotifyLinkURI = regexp.MustCompile(`spotify:(?:user:[^:\s]+:)?(playlist|album|track|artist|episode|show):([A-Za-z0-9]+)`)
	shortLinkURL   = regexp.MustCompile(`https?://spotify\.link/[A-Za-z0-9_-]+`)
)

// parseSpotifyLink extracts the entity kind and ID of the first Spotify
// link in text. A kind of "show" is reported as is; ResolveLink maps it.
func parseSpotifyLink(text string) (kind, id string, ok bool) {
	for _, re := range []*regexp.Regexp{spotifyLinkURL, spotifyLinkURI} {
		if m := re.FindStringSubmatch(text); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// ResolveShortLink follows a spotify.link short URL to the open.spotify.com
// URL it redirects to. It is a var so tests never touch the network.
var resolveShortLink = func(ctx context.Context, raw string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: webHTTPClient.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	return resp.Request.URL.String(), nil
}

// ResolveLink turns a pasted or shared Spotify link into what it points
// at. Playlists, albums and artists come back by ID with a best-effort
// name, so the caller can open the matching page even when the name
// lookup fails (editorial playlists are unreadable for some apps). A
// show opens as its synthetic episode playlist. Tracks and episodes
// carry the full track, ready to play. Implements provider.LinkResolver.
func (p *SpotifyProvider) ResolveLink(ctx context.Context, text string) (provider.Link, error) {
	if err := p.ensureSession(); err != nil {
		return provider.Link{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	kind, id, ok := parseSpotifyLink(text)
	if !ok {
		if short := shortLinkURL.FindString(text); short != "" {
			long, err := resolveShortLink(ctx, short)
			if err != nil {
				return provider.Link{}, fmt.Errorf("spotify: open link: %w", err)
			}
			kind, id, ok = parseSpotifyLink(long)
		}
	}
	if !ok {
		return provider.Link{}, fmt.Errorf("spotify: no Spotify link found in %q", text)
	}

	switch kind {
	case "show":
		name, image := p.showHeader(ctx, id)
		return provider.Link{Kind: provider.LinkPlaylist, ID: savedShowIDPrefix + id, Name: name, ImageURL: image}, nil
	case "playlist":
		name, image := p.linkName(ctx, "/v1/playlists/"+id, url.Values{"fields": {"name,images"}})
		return provider.Link{Kind: provider.LinkPlaylist, ID: id, Name: name, ImageURL: image}, nil
	case "album":
		name, image := p.linkName(ctx, "/v1/albums/"+id, nil)
		return provider.Link{Kind: provider.LinkAlbum, ID: id, Name: name, ImageURL: image}, nil
	case "artist":
		name, image := p.linkName(ctx, "/v1/artists/"+id, nil)
		return provider.Link{Kind: provider.LinkArtist, ID: id, Name: name, ImageURL: image}, nil
	case "track", "episode":
		uri := "spotify:" + kind + ":" + id
		track := playlist.Track{Path: uri}
		if sess := p.currentSession(); sess != nil {
			if byURI, err := itemMetadata(sess, ctx, []string{uri}); err == nil {
				if full, ok := byURI[uri]; ok {
					track = full
				}
			}
		}
		linkKind := provider.LinkTrack
		if kind == "episode" {
			linkKind = provider.LinkEpisode
		}
		return provider.Link{Kind: linkKind, ID: id, Name: track.Title, ImageURL: track.AlbumArtURL, Track: &track}, nil
	}
	return provider.Link{}, fmt.Errorf("spotify: unsupported link kind %q", kind)
}

// linkName reads the name and image of a playlist, album or artist. A
// failed lookup is not an error: the link still opens by ID.
func (p *SpotifyProvider) linkName(ctx context.Context, path string, query url.Values) (name, image string) {
	resp, err := p.webAPI(ctx, "GET", path, query)
	if err != nil {
		return "", ""
	}
	var result struct {
		Name   string         `json:"name"`
		Images []spotifyImage `json:"images"`
	}
	if err := decodeBody(resp, &result); err != nil {
		return "", ""
	}
	return result.Name, pickCoverImage(result.Images)
}

// currentSession returns the live session, or nil before sign-in.
func (p *SpotifyProvider) currentSession() *Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session
}
