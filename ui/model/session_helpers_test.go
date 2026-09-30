package model

import (
	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

// trackSaver adapts a (track, position, context, index) callback to a
// ResumeSaver, for tests that only look at the playback part of a checkpoint.
func trackSaver(f func(track playlist.Track, positionSec int, context []playlist.Track, contextIndex int)) ResumeSaver {
	return func(s resume.State) {
		track := playlist.Track{Path: s.Path}
		if s.ContextIndex >= 0 && s.ContextIndex < len(s.Context) {
			track = s.Context[s.ContextIndex]
		}
		f(track, s.PositionSec, s.Context, s.ContextIndex)
	}
}

// exitContext returns the context captured at quit.
func exitContext(m Model) ([]playlist.Track, int) {
	s := m.ExitSession()
	return s.Context, s.ContextIndex
}
