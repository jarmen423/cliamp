package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
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
	m := newMockAPI(t)
	m.handlers["/v1/tracks"] = func(t *testing.T, query url.Values) string {
		ids := strings.Split(query.Get("ids"), ",")
		if want := []string{"t2", "t1", "t3"}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v (deduped tracks, capped at the limit)", ids, want)
		}
		// The API omits tracks it no longer has, as null.
		return fmt.Sprintf(`{"tracks":[%s,null,%s]}`, trackJSON("t2"), trackJSON("t3"))
	}

	seed := playlist.Track{Path: "spotify:track:seed"}
	got, err := newTestProvider().RadioTracks(t.Context(), provider.RadioSeed{Tracks: []playlist.Track{seed}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "spotify:track:t2" || got[1].Path != "spotify:track:t3" {
		t.Fatalf("tracks = %v, want t2 then t3 in station order", got)
	}
	if got[0].Title != "Track t2" || got[0].Artist != "Ringo, Guest" {
		t.Errorf("track = %+v, want full metadata from the Web API", got[0])
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
