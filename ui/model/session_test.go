package model

import (
	"testing"
	"time"

	"github.com/bjarneo/cliamp/internal/playback"
	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

func TestRemoteQuitCapturesSession(t *testing.T) {
	m := immQueuePageModel(t)
	m.player.(*playbackFakeEngine).position = 42 * time.Second
	updated, _ := m.Update(playback.QuitMsg{})
	state := updated.(Model).ExitSession()
	if state.Path != "/a" || state.PositionSec != 42 || len(state.Queue) != 1 || state.Immersive == nil || state.Immersive.View != "queue" {
		t.Fatalf("remote quit captured %+v; want active track at 42s, queue and queue page", state)
	}
}

func TestSessionStateContents(t *testing.T) {
	local := playlist.Track{Path: "/music/a.mp3"}
	live := playlist.Track{Path: "https://radio.example/stream", Stream: true, Realtime: true}
	yt := playlist.Track{Path: "https://www.youtube.com/watch?v=x"}
	for _, tt := range []struct {
		name        string
		track       playlist.Track
		context     []playlist.Track
		index, pos  int
		wantPos     int
		wantContext int
	}{
		{"local file keeps its position", local, []playlist.Track{local}, 0, 42, 42, 1},
		{"live stream is reselected from the start", live, []playlist.Track{live}, 0, 3600, 0, 1},
		{"yt-dlp site is reselected from the start", yt, []playlist.Track{yt}, 0, 90, 0, 1},
		{"unknown index drops the context", local, []playlist.Track{local}, -1, 42, 42, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pl := playlist.New()
			pl.Add(playlist.Track{Path: "/music/next.mp3"})
			pl.Queue(0)
			m := immersiveModel(t)
			m.playlist = pl
			s := m.sessionState(tt.track, tt.pos, tt.context, tt.index)
			if s.Path != tt.track.Path || s.PositionSec != tt.wantPos || len(s.Context) != tt.wantContext {
				t.Fatalf("state = path %q pos %d context %d, want pos %d context %d", s.Path, s.PositionSec, len(s.Context), tt.wantPos, tt.wantContext)
			}
			if len(s.Queue) != 1 || s.Queue[0].Path != "/music/next.mp3" {
				t.Fatalf("queue = %+v, want the queued track", s.Queue)
			}
			if s.Immersive == nil || s.Immersive.Section != "playlists" {
				t.Fatalf("immersive page = %+v, want the playlists page", s.Immersive)
			}
		})
	}
}

// Quitting before a restored track was played keeps its saved spot, so a
// launch and quit does not lose where the user left off.
func TestQuitKeepsUnplayedRestoredTrack(t *testing.T) {
	context := []playlist.Track{{Path: "/music/a.mp3"}, {Path: "/music/b.mp3"}}
	pl := playlist.New()
	pl.Add(context...)
	m := Model{player: &playbackFakeEngine{}, playlist: pl}
	m.SetInitialTrack(1)
	m.SetResume("/music/b.mp3", 95)
	m.quit()
	s := m.ExitSession()
	if s.Path != "/music/b.mp3" || s.PositionSec != 95 || s.ContextIndex != 1 || len(s.Context) != 2 {
		t.Fatalf("exit session = %+v, want b.mp3 at 95s in its list", s)
	}
}

// A live station that is playing is saved without a position, so the next
// launch reselects it rather than dropping it (quit used to skip streams).
func TestQuitSavesLiveStationWithoutPosition(t *testing.T) {
	station := playlist.Track{Path: "https://radio.example/stream", Stream: true, Realtime: true}
	m := Model{
		player:       &playbackFakeEngine{playing: true, position: 20 * time.Minute},
		playingTrack: station, playingTrackActive: true,
		playbackContext: []playlist.Track{station},
	}
	m.quit()
	if s := m.ExitSession(); s.Path != station.Path || s.PositionSec != 0 || len(s.Context) != 1 {
		t.Fatalf("exit session = %+v, want the station at 0s", s)
	}
}

// Playing a different track than the restored one retires the armed resume,
// so a later quit exits onto the last checkpoint, not the stale startup spot.
func TestOtherTrackDisplacesArmedResume(t *testing.T) {
	context := []playlist.Track{{Path: "/music/a.mp3"}, {Path: "/music/b.mp3"}}
	pl := playlist.New()
	pl.Add(context...)
	m := Model{player: &playbackFakeEngine{}, playlist: pl}
	m.resumeSaver = func(resume.State) {}
	m.SetInitialTrack(0)
	m.SetResume("/music/a.mp3", 95)

	m.beginPlaybackTrack(context[1])
	m.persistPlaybackContext(context[1], 30, time.Now())
	m.quit()
	s := m.ExitSession()
	if s.Path != "/music/b.mp3" || s.PositionSec != 30 {
		t.Fatalf("exit session = %+v, want b.mp3 at 30s", s)
	}
}

// Quitting while stopped keeps the last checkpoint's track and context on the
// exit write instead of clobbering them with a trackless state.
func TestQuitStoppedKeepsCheckpoint(t *testing.T) {
	context := []playlist.Track{{Path: "/music/a.mp3"}, {Path: "/music/b.mp3"}}
	pl := playlist.New()
	pl.Add(context...)
	m := Model{player: &playbackFakeEngine{}, playlist: pl}
	m.resumeSaver = func(resume.State) {}
	m.persistPlaybackContext(context[1], 30, time.Now())
	pl.Add(playlist.Track{Path: "/music/c.mp3"})
	pl.Queue(2)

	m.quit()
	s := m.ExitSession()
	if s.Path != "/music/b.mp3" || s.PositionSec != 30 || len(s.Context) != 2 {
		t.Fatalf("exit session = %+v, want b.mp3 at 30s in its list", s)
	}
	if len(s.Queue) != 1 || s.Queue[0].Path != "/music/c.mp3" {
		t.Fatalf("queue = %+v, want the queued track", s.Queue)
	}
}
