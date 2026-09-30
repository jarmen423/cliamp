package main

import (
	"testing"

	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

func TestRestoreSessionContext(t *testing.T) {
	jf := "https://jf.example.com/Items/two/Download?api_key=old"
	for _, tt := range []struct {
		name      string
		state     resume.State
		wantOK    bool
		wantIndex int
	}{
		{"spotify context", resume.State{
			Path: "spotify:track:b", PositionSec: 30, ContextIndex: 1,
			Context: []playlist.Track{{Path: "spotify:track:a"}, {Path: "spotify:track:b"}},
		}, true, 1},
		{"radio station at position zero", resume.State{
			Path:    "https://radio.example/stream",
			Context: []playlist.Track{{Path: "https://radio.example/other"}, {Path: "https://radio.example/stream"}},
		}, true, 1},
		{"legacy file without context", resume.State{Path: "/music/a.mp3", PositionSec: 42}, false, 0},
		{"jellyfin track with the provider gone", resume.State{
			Path: jf, PositionSec: 10, Context: []playlist.Track{{Path: jf}},
		}, false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tracks, index, active, ok := restoreResumeContext(tt.state, sessionTrackRestorer(nil))
			if ok != tt.wantOK {
				t.Fatalf("restored = %v, want %v", ok, tt.wantOK)
			}
			if ok && (index != tt.wantIndex || active != tt.state.Path || len(tracks) != len(tt.state.Context)) {
				t.Fatalf("restore = index %d active %q len %d", index, active, len(tracks))
			}
		})
	}
}

// Saved queue entries already in the list are queued in place; others are
// appended, and entries that cannot be restored are skipped.
func TestRequeueSession(t *testing.T) {
	pl := playlist.New()
	pl.Add(playlist.Track{Path: "/a.mp3"}, playlist.Track{Path: "/b.mp3"})
	requeueSession(pl, []playlist.Track{
		{Path: "/b.mp3"},
		{Path: "https://jf.example.com/Items/x/Download?api_key=old"}, // no Jellyfin provider
		{Path: "/c.mp3"},
	}, sessionTrackRestorer(nil))
	queue := pl.QueueTracks()
	if pl.Len() != 3 || len(queue) != 2 || queue[0].Path != "/b.mp3" || queue[1].Path != "/c.mp3" {
		t.Fatalf("playlist len %d, queue %+v; want b queued in place, c appended", pl.Len(), queue)
	}
}
