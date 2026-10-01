// Package resume persists the last session: the selected track and its
// position, the list it was playing from, the play-next queue, and the
// immersive page on screen, so the next launch picks up where the user left.
package resume

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/internal/fileutil"
	"github.com/bjarneo/cliamp/playlist"
)

// State holds enough information to resume a previous playback session.
// Files written before Queue and Immersive existed still load; the missing
// fields stay empty.
type State struct {
	Path         string           `json:"path"`
	PositionSec  int              `json:"position_sec"`
	Playlist     string           `json:"playlist,omitempty"`
	Context      []playlist.Track `json:"context,omitempty"`
	ContextIndex int              `json:"context_index,omitempty"`
	Queue        []playlist.Track `json:"queue,omitempty"`     // play-next entries, in order
	Immersive    *View            `json:"immersive,omitempty"` // nil when the classic layout was up
}

// View is the immersive page on screen at exit. Sections, views and kinds
// are stored by name rather than number, so a renumbered constant never
// reopens the wrong page; an unknown name falls back to the section root.
type View struct {
	Provider string `json:"provider"` // provider name the page belongs to
	Section  string `json:"section"`
	View     string `json:"view"`
	Kind     string `json:"kind,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Sub      string `json:"sub,omitempty"`
	Query    string `json:"query,omitempty"` // search view: the query to re-run
	Cursor   int    `json:"cursor,omitempty"`
	Scroll   int    `json:"scroll,omitempty"`
}

// The session is checkpointed every few seconds while playing, so the saved
// lists are capped: a 5,000-song library as context would rewrite megabytes
// each time. MaxContextTracks keeps a window around the active track (a few
// hundred KB at most); MaxQueueTracks keeps the head of the play-next queue.
const (
	MaxContextTracks = 500
	MaxQueueTracks   = 200
	contextLookback  = 100 // tracks kept before the active one
)

// capped returns s with Context windowed around ContextIndex and Queue
// truncated to their limits.
func (s State) capped() State {
	if n := len(s.Context); n > MaxContextTracks {
		idx := min(max(s.ContextIndex, 0), n-1)
		start := min(max(0, idx-contextLookback), n-MaxContextTracks)
		s.Context = s.Context[start : start+MaxContextTracks]
		s.ContextIndex = idx - start
	}
	if len(s.Queue) > MaxQueueTracks {
		s.Queue = s.Queue[:MaxQueueTracks]
	}
	return s
}

// empty reports whether s carries nothing worth resuming.
func (s State) empty() bool {
	hasTrack := s.Path != "" && (s.PositionSec > 0 || len(s.Context) > 0)
	return !hasTrack && len(s.Queue) == 0 && s.Immersive == nil
}

func stateFile() (string, error) {
	dir, err := appdir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "resume.json"), nil
}

// Save writes the resume state to disk. No-ops for empty path or zero/negative
// position to avoid overwriting a valid resume file with useless data.
func Save(path string, positionSec int, playlist string) {
	SaveState(State{Path: path, PositionSec: positionSec, Playlist: playlist})
}

// SaveState writes a complete resume state: the track, its playback context,
// the queue and the immersive page. A state with nothing to resume is not
// written, so it never replaces a useful file. Errors are silently ignored so
// a failed write never disrupts normal exit.
func SaveState(state State) {
	if state.PositionSec < 0 || state.empty() {
		return
	}
	state = state.capped()
	f, err := stateFile()
	if err != nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = fileutil.WriteFileAtomic(f, data, 0o600)
}

// Load reads the resume state from disk. Returns a zero State if the file
// does not exist or cannot be parsed.
func Load() State {
	f, err := stateFile()
	if err != nil {
		return State{}
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}
	}
	return s
}
