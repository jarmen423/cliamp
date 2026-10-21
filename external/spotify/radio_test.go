package spotify

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// stubStation replaces the live station lookup, recording what it was asked.
func stubStation(t *testing.T, uris []string, err error) (contextURI *string, recent *[]string) {
	t.Helper()
	original := stationTrackURIs
	t.Cleanup(func() { stationTrackURIs = original })
	contextURI, recent = new(string), new([]string)
	stationTrackURIs = func(_ context.Context, _ *Session, uri string, rec []string) ([]string, error) {
		*contextURI, *recent = uri, rec
		return uris, err
	}
	return contextURI, recent
}

// stubItemMetadata replaces the spclient metadata lookup with a fixed
// catalog, recording the URIs each call asked for.
func stubItemMetadata(t *testing.T, catalog map[string]playlist.Track, err error) *[][]string {
	t.Helper()
	original := itemMetadata
	t.Cleanup(func() { itemMetadata = original })
	asked := new([][]string)
	itemMetadata = func(_ *Session, _ context.Context, uris []string) (map[string]playlist.Track, error) {
		*asked = append(*asked, uris)
		if err != nil {
			return nil, err
		}
		out := map[string]playlist.Track{}
		for _, u := range uris {
			if tr, ok := catalog[u]; ok {
				out[u] = tr
			}
		}
		return out, nil
	}
	return asked
}

func TestStationContextURI(t *testing.T) {
	const id = "56oDRnqbIiwx4mymNEv7dS"
	track := playlist.Track{Path: "spotify:track:4uLU6hMCjMI75M1A2tKUQC"}
	for _, tt := range []struct {
		name string
		seed provider.RadioSeed
		want string
	}{
		{"artist", provider.RadioSeed{Kind: provider.RadioSeedArtist, ID: id}, "spotify:artist:" + id},
		{"album", provider.RadioSeed{Kind: provider.RadioSeedAlbum, ID: id}, "spotify:album:" + id},
		{"playlist", provider.RadioSeed{Kind: provider.RadioSeedPlaylist, ID: id}, "spotify:playlist:" + id},
		{"saved album listed as a playlist", provider.RadioSeed{Kind: provider.RadioSeedPlaylist, ID: savedAlbumIDPrefix + id}, "spotify:album:" + id},
		{"track", provider.RadioSeed{Tracks: []playlist.Track{track}}, track.Path},
		{"synthetic row falls back to its tracks", provider.RadioSeed{Kind: provider.RadioSeedPlaylist, ID: savedTracksPlaylistID, Tracks: []playlist.Track{track}}, track.Path},
		{"synthetic row without tracks", provider.RadioSeed{Kind: provider.RadioSeedPlaylist, ID: savedTracksPlaylistID}, ""},
		{"local file", provider.RadioSeed{Tracks: []playlist.Track{{Path: "/music/a.mp3"}}}, ""},
	} {
		if got := stationContextURI(tt.seed); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRadioTracksLoadsStationInOrder(t *testing.T) {
	contextURI, recent := stubStation(t, []string{
		"spotify:track:t2", "spotify:track:t1", "spotify:track:t2", // repeat dropped
		"spotify:episode:e1", // not a track
		"spotify:track:t3", "spotify:track:t4",
	}, nil)
	newMockAPI(t) // no handlers: the Web API must not be reached
	// The catalog no longer has t1, so its lookup comes back without it.
	asked := stubItemMetadata(t, map[string]playlist.Track{
		"spotify:track:t2": {Path: "spotify:track:t2", Title: "Track t2", Artist: "Ringo, Guest"},
		"spotify:track:t3": {Path: "spotify:track:t3", Title: "Track t3"},
	}, nil)

	seed := playlist.Track{Path: "spotify:track:seed"}
	got, err := newTestProvider().RadioTracks(t.Context(), provider.RadioSeed{Tracks: []playlist.Track{seed}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"spotify:track:t2", "spotify:track:t1", "spotify:track:t3"}
	if len(*asked) != 1 || !slices.Equal((*asked)[0], want) {
		t.Errorf("looked up %v, want %v (deduped tracks, capped at the limit)", *asked, want)
	}
	if len(got) != 2 || got[0].Path != "spotify:track:t2" || got[1].Path != "spotify:track:t3" {
		t.Fatalf("tracks = %v, want t2 then t3 in station order", got)
	}
	if got[0].Title != "Track t2" || got[0].Artist != "Ringo, Guest" {
		t.Errorf("track = %+v, want full metadata", got[0])
	}
	if *contextURI != seed.Path || !slices.Equal(*recent, []string{seed.Path}) {
		t.Errorf("station asked for %q with recent %v, want the seed track", *contextURI, *recent)
	}
}

func TestRadioTracksErrors(t *testing.T) {
	stationErr := errors.New("station down")
	stubStation(t, nil, stationErr)
	newMockAPI(t) // no handlers: the Web API must not be reached
	p := newTestProvider()
	seed := provider.RadioSeed{Kind: provider.RadioSeedArtist, ID: "56oDRnqbIiwx4mymNEv7dS"}
	if _, err := p.RadioTracks(t.Context(), seed, 10); !errors.Is(err, stationErr) {
		t.Errorf("err = %v, want the station failure", err)
	}
	if _, err := p.RadioTracks(t.Context(), provider.RadioSeed{}, 10); !errors.Is(err, errNoStationSeed) {
		t.Errorf("err = %v, want errNoStationSeed for an empty seed", err)
	}
}

func TestRadioTracksMetadataFailure(t *testing.T) {
	stubStation(t, []string{"spotify:track:t1"}, nil)
	lookupErr := errors.New("spclient down")
	stubItemMetadata(t, nil, lookupErr)
	seed := provider.RadioSeed{Tracks: []playlist.Track{{Path: "spotify:track:seed"}}}
	if _, err := newTestProvider().RadioTracks(t.Context(), seed, 10); !errors.Is(err, lookupErr) {
		t.Errorf("err = %v, want the metadata failure", err)
	}
}

// A station with no tracks is no station: the UI must not open an empty
// radio or fill it with recommendations.
func TestRadioTracksEmptyStation(t *testing.T) {
	stubStation(t, nil, nil)
	newMockAPI(t) // no handlers: the Web API must not be reached
	seed := provider.RadioSeed{Tracks: []playlist.Track{{Path: "spotify:track:seed"}}}
	if _, err := newTestProvider().RadioTracks(t.Context(), seed, 10); !errors.Is(err, provider.ErrNoStation) {
		t.Errorf("err = %v, want provider.ErrNoStation", err)
	}
}

// Spotify answers 404 for contexts it builds no station around; go-librespot
// carries the status only in the error text.
func TestIsStationNotFound(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want bool
	}{
		{errors.New("invalid status code from context resolve autoplay: 404"), true},
		{errors.New("invalid status code from context resolve autoplay: 500"), false},
		{errors.New("dial tcp: i/o timeout"), false},
		{nil, false},
	} {
		if got := isStationNotFound(tt.err); got != tt.want {
			t.Errorf("isStationNotFound(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
