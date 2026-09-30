package model

// collection_radio.go is artist, album and playlist radio, next to song
// radio (track_menu.go). Recommenders suggest tracks around a seed rather
// than more of it (Spotify's leaves the seed's artists out), so a
// collection radio mixes a spread of the collection's own tracks with the
// recommendations: one of its tracks, then recommendations, and so on.

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// radioSeedMax is how many of a collection's tracks seed its radio.
const radioSeedMax = 10

// collectionRadioMsg carries a collection radio's seeds and recommendations.
type collectionRadioMsg struct {
	label        string
	seeds        []playlist.Track
	tracks       []playlist.Track
	err          error
	providerName string
	gen          uint64
}

// radioLabel names a radio by the kind of collection it starts from; ""
// for kinds that have none.
func radioLabel(kind immItemKind) string {
	switch kind {
	case immKindArtist:
		return "Artist radio"
	case immKindAlbum:
		return "Album radio"
	case immKindPlaylist:
		return "Playlist radio"
	}
	return ""
}

// collectionTracksLoader returns how to load a collection's tracks on prov,
// or nil when prov cannot.
func collectionTracksLoader(prov playlist.Provider, kind immItemKind, id string) func() ([]playlist.Track, error) {
	switch kind {
	case immKindArtist:
		if l, ok := prov.(provider.ArtistDetailLoader); ok {
			return func() ([]playlist.Track, error) {
				d, err := l.ArtistDetail(id)
				return d.Popular, err
			}
		}
	case immKindAlbum:
		if l, ok := prov.(provider.AlbumTrackLoader); ok {
			return func() ([]playlist.Track, error) { return l.AlbumTracks(id) }
		}
	case immKindPlaylist:
		return func() ([]playlist.Track, error) { return prov.Tracks(id) }
	}
	return nil
}

// sampleSeeds picks up to n playable tracks spread evenly across tracks,
// so a long playlist seeds from its whole length, not just its head.
func sampleSeeds(tracks []playlist.Track, n int) []playlist.Track {
	playable := make([]playlist.Track, 0, len(tracks))
	for _, t := range tracks {
		if t.Path != "" && !isSearchPlaceholder(t) {
			playable = append(playable, t)
		}
	}
	if len(playable) <= n {
		return playable
	}
	out := make([]playlist.Track, 0, n)
	for i := range n {
		out = append(out, playable[i*len(playable)/n])
	}
	return out
}

// mixRadio interleaves seeds and recommendations, a seed first, dropping
// repeats by path.
func mixRadio(seeds, recs []playlist.Track) []playlist.Track {
	out := make([]playlist.Track, 0, len(seeds)+len(recs))
	seen := make(map[string]bool, cap(out))
	add := func(t playlist.Track) {
		if t.Path != "" && !seen[t.Path] {
			seen[t.Path] = true
			out = append(out, t)
		}
	}
	perSeed := max(1, (len(recs)+len(seeds)-1)/max(1, len(seeds)))
	r := 0
	for _, s := range seeds {
		add(s)
		for end := min(len(recs), r+perSeed); r < end; r++ {
			add(recs[r])
		}
	}
	for ; r < len(recs); r++ {
		add(recs[r])
	}
	return out
}

// startCollectionRadio loads the collection's tracks, then asks prov's
// recommender for tracks around a sample of them.
func (m *Model) startCollectionRadio(prov playlist.Provider, kind immItemKind, id, name string) tea.Cmd {
	rec, ok := prov.(provider.Recommender)
	label := radioLabel(kind)
	load := collectionTracksLoader(prov, kind, id)
	if !ok || label == "" || id == "" || load == nil {
		m.status.Show("No radio for this item", statusTTLDefault)
		return nil
	}
	m.status.Activityf(statusTTLDefault, "Building radio from %s…", name)
	gen := nextRequest(&m.requests.trackMenu)
	providerName := prov.Name()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := collectionRadioMsg{label: label, providerName: providerName, gen: gen}
		tracks, err := load()
		if err != nil {
			msg.err = err
			return msg
		}
		msg.seeds = sampleSeeds(tracks, radioSeedMax)
		if len(msg.seeds) == 0 {
			msg.err = fmt.Errorf("%s has no playable tracks", name)
			return msg
		}
		msg.tracks, msg.err = rec.RecommendTracks(ctx, msg.seeds, trackRadioLimit)
		return msg
	}
}

// handleCollectionRadio plays the mixed radio batch.
func (m *Model) handleCollectionRadio(msg collectionRadioMsg) tea.Cmd {
	if msg.gen != m.requests.trackMenu {
		return nil
	}
	if msg.err != nil {
		m.status.Errorf(statusTTLDefault, "%s failed: %s", msg.label, msg.err)
		return nil
	}
	return m.playRadio(msg.label, mixRadio(msg.seeds, msg.tracks))
}

// immersiveRadio is R: radio for the open playlist, album or artist, or in
// lists for the focused row (its collection's radio, or song radio for a
// track). Radio needs a provider with recommendations.
func (m *Model) immersiveRadio() tea.Cmd {
	im := m.immersive
	if _, ok := im.prov.(provider.Recommender); !ok {
		m.status.Show("Radio needs a provider with recommendations", statusTTLShort)
		return nil
	}
	if im.focus != immPaneQueue {
		switch im.view {
		case immViewPlaylist, immViewAlbum, immViewArtist:
			return m.startCollectionRadio(im.prov, im.ctxKind, im.ctxID, im.ctxName)
		}
		items := m.canvasItems()
		if im.cursor >= 0 && im.cursor < len(items) {
			if it := items[im.cursor]; radioLabel(it.kind) != "" {
				return m.startCollectionRadio(im.prov, it.kind, it.id, it.title)
			}
		}
	}
	if t, _, _, ok := m.immersiveFocusedTrack(); ok {
		return m.startTrackRadio(t)
	}
	return nil
}

// artistRadioTarget resolves the track's artist on a provider that can load
// the artist and recommend, for the track menu's artist radio.
func (m Model) artistRadioTarget(t playlist.Track) (playlist.Provider, provider.ArtistInfo, bool) {
	target, ok := m.trackArtistTarget(t)
	if !ok || target.artist.ID == "" {
		return nil, provider.ArtistInfo{}, false
	}
	if _, ok := target.prov.(provider.Recommender); !ok {
		return nil, provider.ArtistInfo{}, false
	}
	if _, ok := target.prov.(provider.ArtistDetailLoader); !ok {
		return nil, provider.ArtistInfo{}, false
	}
	return target.prov, target.artist, true
}
