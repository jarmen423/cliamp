package model

// session.go builds the resume checkpoint (internal/resume): the active track
// and the list it plays from, the play-next queue, and the immersive page.
// The same state is written every few seconds while playing and once at exit.

import (
	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

// seekableOnResume reports whether a saved position means anything for
// track: local files and finite streams resume where they stopped, while
// live streams and most yt-dlp sites restart from the beginning.
func seekableOnResume(track playlist.Track) bool {
	return !track.IsLive() && (!playlist.IsYTDL(track.Path) || playlist.IsMixcloudURL(track.Path))
}

// sessionState assembles the checkpoint for track at positionSec, chosen
// from context[contextIndex]. A zero track records only the queue and page.
func (m Model) sessionState(track playlist.Track, positionSec int, context []playlist.Track, contextIndex int) resume.State {
	state := resume.State{Playlist: m.loadedPlaylist, Immersive: m.immersiveResumeView()}
	if track.Path != "" {
		if !seekableOnResume(track) {
			positionSec = 0
		}
		state.Path, state.PositionSec = track.Path, positionSec
		if contextIndex >= 0 && contextIndex < len(context) {
			state.Context, state.ContextIndex = cloneTracks(context), contextIndex
		}
	}
	if m.playlist != nil {
		state.Queue = m.playlist.QueueTracks()
	}
	return state
}

// captureExitSession records the session at quit, before the player closes.
// A restored track that was never played keeps its saved position, so
// opening and closing cliamp does not lose where the user left off.
func (m *Model) captureExitSession() {
	track, _ := m.currentPlaybackTrack()
	switch {
	case track.Path != "" && m.player.IsPlaying() && !m.buffering && !m.player.GaplessAdvanced():
		context, index := m.playbackContextFor(track)
		m.exitResume = m.sessionState(track, max(0, int(m.player.Position().Seconds())), context, index)
	case m.resume.path != "":
		if i := trackIndexByPath(m.playbackContext, m.resume.path); i >= 0 {
			m.exitResume = m.sessionState(m.playbackContext[i], m.resume.secs, m.playbackContext, i)
			return
		}
		m.exitResume = m.tracklessSessionState()
	default:
		m.exitResume = m.tracklessSessionState()
	}
}

// tracklessSessionState records the queue and page at quit while keeping the
// track and context the last checkpoint saved; without them the exit write
// would erase the resume the checkpoint file already holds.
func (m *Model) tracklessSessionState() resume.State {
	state := m.sessionState(playlist.Track{}, 0, nil, 0)
	state.Path = m.lastSessionState.Path
	state.PositionSec = m.lastSessionState.PositionSec
	state.Context = m.lastSessionState.Context
	state.ContextIndex = m.lastSessionState.ContextIndex
	return state
}

// ExitSession returns the session captured at quit. Called after the program
// returns (player already closed).
func (m Model) ExitSession() resume.State {
	state := m.exitResume
	state.Context = cloneTracks(state.Context)
	state.Queue = cloneTracks(state.Queue)
	return state
}
