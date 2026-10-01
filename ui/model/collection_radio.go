package model

// collection_radio.go is artist, album and playlist radio, next to song
// radio (track_menu.go). A provider that builds stations (RadioBuilder)
// supplies the radio whole. Otherwise it comes from the Recommender, which
// suggests tracks around a seed rather than more of it, so the radio mixes
// a spread of the collection's own tracks with the recommendations: one of
// its tracks, then recommendations, and so on.

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// radioSeedMax is how many of a collection's tracks seed its radio.
const radioSeedMax = 10

// collectionRadioMsg carries a collection radio's tracks, ready to play.
type collectionRadioMsg struct {
	label        string
	name         string // the collection the radio is built from
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

// radioSeedKind is the provider's name for the kind of collection a station
// is built around.
func radioSeedKind(kind immItemKind) string {
	switch kind {
	case immKindArtist:
		return provider.RadioSeedArtist
	case immKindAlbum:
		return provider.RadioSeedAlbum
	case immKindPlaylist:
		return provider.RadioSeedPlaylist
	}
	return ""
}

// stationTracks is prov's station around seed; nil when it builds none, and
// the radio then comes from its recommendations.
func stationTracks(ctx context.Context, prov any, seed provider.RadioSeed, limit int) []playlist.Track {
	rb, ok := prov.(provider.RadioBuilder)
	if !ok {
		return nil
	}
	tracks, err := rb.RadioTracks(ctx, seed, limit)
	if err != nil {
		applog.Debug("radio: no station, using recommendations: %v", err)
	}
	return tracks
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

// startCollectionRadio asks prov for the collection's station. Without one
// it loads the collection's tracks and mixes a sample of them with the
// recommender's tracks around that sample.
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
		msg := collectionRadioMsg{label: label, name: name, providerName: providerName, gen: gen}
		// A station needs only the collection's ID, not its tracks.
		seed := provider.RadioSeed{Kind: radioSeedKind(kind), ID: id}
		if msg.tracks = stationTracks(ctx, prov, seed, trackRadioLimit); len(msg.tracks) > 0 {
			return msg
		}
		tracks, err := load()
		if err != nil {
			msg.err = err
			return msg
		}
		seeds := sampleSeeds(tracks, radioSeedMax)
		if len(seeds) == 0 {
			msg.err = fmt.Errorf("%s has no playable tracks", name)
			return msg
		}
		recs, err := rec.RecommendTracks(ctx, seeds, trackRadioLimit)
		msg.tracks, msg.err = mixRadio(seeds, recs), err
		return msg
	}
}

// handleCollectionRadio plays the radio batch.
func (m *Model) handleCollectionRadio(msg collectionRadioMsg) tea.Cmd {
	if msg.gen != m.requests.trackMenu {
		return nil
	}
	if msg.err != nil {
		m.status.Errorf(statusTTLDefault, "%s failed: %s", msg.label, msg.err)
		return nil
	}
	return m.playRadio(msg.label, msg.name, msg.tracks)
}

// showImmersiveRadio opens a started radio in the canvas as the playlist it
// now is, one history step from where it was started.
func (m *Model) showImmersiveRadio(name string, tracks []playlist.Track) {
	if !m.immersive.active {
		return
	}
	m.pushImmersiveBack()
	m.dropImmersiveFetches()
	im := &m.immersive
	im.view = immViewRadio
	im.ctxID, im.ctxName, im.ctxSub, im.ctxKind = "", name, "", immKindTrack
	im.tracks = tracks
	im.trackSort = immSortTrackOrder
	im.cursor, im.scroll = 0, 0
	im.queueCursor, im.queueScroll = 0, 0
	im.focus = immPaneCanvas
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

// albumRadioTarget resolves the track's album on a provider that can load
// the album and recommend, for the track menu's album radio.
func (m Model) albumRadioTarget(t playlist.Track) (playlist.Provider, bool) {
	id := t.AlbumID()
	if id == "" {
		return nil, false
	}
	prov := m.providerForTrack(t.Path)
	if prov == nil {
		return nil, false
	}
	if _, ok := prov.(provider.Recommender); !ok {
		return nil, false
	}
	if _, ok := prov.(provider.AlbumTrackLoader); !ok {
		return nil, false
	}
	return prov, true
}
