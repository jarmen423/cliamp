package model

import (
	"testing"

	"github.com/bjarneo/cliamp/ipc"
)

// shuffle takes smart as a third mode beside on and off. The phone app cycles
// through the three, so each step has to land in a clean state.
func TestV2ShuffleSmartModes(t *testing.T) {
	state := func(m *Model) (shuffle, smart bool) { return m.playlist.Shuffled(), m.playlist.Smart() }
	for _, tc := range []struct {
		name        string
		name2       string
		smartFirst  bool
		wantOK      bool
		wantShuffle bool
		wantSmart   bool
	}{
		{name: "smart implies shuffle", name2: "smart", wantOK: true, wantShuffle: true, wantSmart: true},
		{name: "smart again stays on", name2: "smart", smartFirst: true, wantOK: true, wantShuffle: true, wantSmart: true},
		{name: "on drops smart and keeps shuffle", name2: "on", smartFirst: true, wantOK: true, wantShuffle: true},
		{name: "off drops both", name2: "off", smartFirst: true, wantOK: true},
		{name: "toggle from smart turns both off", name2: "toggle", smartFirst: true, wantOK: true},
		{name: "toggle from off is plain shuffle", name2: "toggle", wantOK: true, wantShuffle: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRecommenderProvider{recs: smartTracks("rec", 4)}
			m := newSmartTestModel(fake, &smartConfigSaver{})
			if tc.smartFirst {
				runV2(t, &m, "shuffle", ipc.Request{Name: "smart"})
			}
			got := runV2(t, &m, "shuffle", ipc.Request{Name: tc.name2})
			if got.OK != tc.wantOK {
				t.Fatalf("response = %+v, want ok=%v", got, tc.wantOK)
			}
			if shuffle, smart := state(&m); shuffle != tc.wantShuffle || smart != tc.wantSmart {
				t.Fatalf("shuffle=%v smart=%v, want shuffle=%v smart=%v", shuffle, smart, tc.wantShuffle, tc.wantSmart)
			}
		})
	}
}

// A queue whose provider cannot recommend refuses smart and stays as it was.
func TestV2ShuffleSmartUnavailable(t *testing.T) {
	fake := &fakeSpotifyLibProvider{name: "Spotify"} // streams, cannot recommend
	m := newSpotifyTestModel(fake)
	m.playlist.Add(spotifyTracks(2)...)
	if got := runV2(t, &m, "shuffle", ipc.Request{Name: "smart"}); got.Error != ipc.V2ErrorCodeUnavailable {
		t.Fatalf("response = %+v, want %s", got, ipc.V2ErrorCodeUnavailable)
	}
	if m.playlist.Smart() || m.playlist.Shuffled() {
		t.Fatalf("shuffle=%v smart=%v, want both off", m.playlist.Shuffled(), m.playlist.Smart())
	}
	if snap := m.runtimeSnapshot(); snap.SmartAvailable {
		t.Fatalf("snapshot reports smart available: %+v", snap)
	}
}

// The snapshot tells the app which mode is on and whether smart can start.
func TestV2SnapshotReportsSmartShuffle(t *testing.T) {
	fake := &fakeRecommenderProvider{recs: smartTracks("rec", 4)}
	m := newSmartTestModel(fake, &smartConfigSaver{})
	snap := m.runtimeSnapshot()
	if snap.SmartShuffle == nil || *snap.SmartShuffle || !snap.SmartAvailable {
		t.Fatalf("before: smart=%v available=%v, want off and available", snap.SmartShuffle, snap.SmartAvailable)
	}
	runV2(t, &m, "shuffle", ipc.Request{Name: "smart"})
	if snap := m.runtimeSnapshot(); snap.SmartShuffle == nil || !*snap.SmartShuffle {
		t.Fatalf("after: smart=%v, want on", snap.SmartShuffle)
	}
}
