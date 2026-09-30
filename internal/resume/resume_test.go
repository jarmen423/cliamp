package resume

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
)

// withTempHome sets HOME so appdir.Dir() points inside a temp directory,
// restoring the original on cleanup.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withTempHome(t)

	Save("/music/song.mp3", 42, "main")

	got := Load()
	if got.Path != "/music/song.mp3" {
		t.Errorf("Path = %q, want /music/song.mp3", got.Path)
	}
	if got.PositionSec != 42 {
		t.Errorf("PositionSec = %d, want 42", got.PositionSec)
	}
	if got.Playlist != "main" {
		t.Errorf("Playlist = %q, want main", got.Playlist)
	}
}

func TestSaveStateLoadContextRoundTrip(t *testing.T) {
	withTempHome(t)
	state := State{
		Path:         "https://jf.example/Items/two/Download",
		PositionSec:  95,
		Context:      []playlist.Track{{Path: "one", Title: "One"}, {Path: "https://jf.example/Items/two/Download", Title: "Two", Stream: true}},
		ContextIndex: 1,
	}

	SaveState(state)

	if got := Load(); !reflect.DeepEqual(got, state) {
		t.Fatalf("Load() = %+v, want %+v", got, state)
	}
}

func TestSaveStatePersistsZeroPositionWithContext(t *testing.T) {
	withTempHome(t)
	state := State{
		Path:    "https://jf.example/Items/one/Download",
		Context: []playlist.Track{{Path: "https://jf.example/Items/one/Download", Title: "One"}},
	}

	SaveState(state)

	if got := Load(); !reflect.DeepEqual(got, state) {
		t.Fatalf("Load() = %+v, want %+v", got, state)
	}
}

func TestSaveIgnoresEmptyPath(t *testing.T) {
	home := withTempHome(t)
	Save("", 10, "p")

	f := filepath.Join(home, ".config", "cliamp", "resume.json")
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("resume.json should not exist for empty path, got err=%v", err)
	}
}

func TestSaveIgnoresNonPositivePosition(t *testing.T) {
	home := withTempHome(t)
	Save("/music/song.mp3", 0, "p")
	Save("/music/song.mp3", -5, "p")

	f := filepath.Join(home, ".config", "cliamp", "resume.json")
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("resume.json should not exist for non-positive position, got err=%v", err)
	}
}

func TestLoadMissingFileReturnsZero(t *testing.T) {
	withTempHome(t)
	got := Load()
	if !reflect.DeepEqual(got, State{}) {
		t.Errorf("Load() = %+v, want zero State", got)
	}
}

func TestLoadCorruptFileReturnsZero(t *testing.T) {
	home := withTempHome(t)
	dir := filepath.Join(home, ".config", "cliamp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resume.json"), []byte("not json {{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := Load()
	if !reflect.DeepEqual(got, State{}) {
		t.Errorf("Load() = %+v, want zero State for corrupt file", got)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	home := withTempHome(t)

	// Parent directory doesn't exist yet.
	parent := filepath.Join(home, ".config", "cliamp")
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("precondition: parent should not exist, got err=%v", err)
	}

	Save("/music/song.mp3", 1, "")

	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		t.Errorf("Save should create parent directory, err=%v", err)
	}
}

func TestSaveWriteFileIsReadable(t *testing.T) {
	home := withTempHome(t)
	Save("/music/a.mp3", 77, "pl")

	f := filepath.Join(home, ".config", "cliamp", "resume.json")
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("resume.json is empty")
	}
}

func tracksN(n int) []playlist.Track {
	out := make([]playlist.Track, n)
	for i := range out {
		out[i] = playlist.Track{Path: fmt.Sprintf("t%d", i)}
	}
	return out
}

func TestSaveStateSessionRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state State
	}{
		{"track, queue and immersive page", State{
			Path: "spotify:track:2", PositionSec: 61,
			Context:      []playlist.Track{{Path: "spotify:track:1"}, {Path: "spotify:track:2", Title: "Two"}},
			ContextIndex: 1,
			Queue:        []playlist.Track{{Path: "spotify:track:9", Title: "Next"}},
			Immersive: &View{
				Provider: "Spotify", Section: "playlists", View: "playlist", Kind: "playlist",
				ID: "pl1", Name: "Mix", Sub: "Playlist", Cursor: 7, Scroll: 3,
			},
		}},
		{"immersive page without a track", State{
			Immersive: &View{Provider: "Radio", Section: "search", View: "search", Query: "lofi"},
		}},
		{"queue without a track", State{Queue: []playlist.Track{{Path: "/a.mp3"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withTempHome(t)
			SaveState(tt.state)
			if got := Load(); !reflect.DeepEqual(got, tt.state) {
				t.Fatalf("Load() = %+v, want %+v", got, tt.state)
			}
		})
	}
}

// Files written before the queue and immersive fields existed still load.
func TestLoadLegacyFile(t *testing.T) {
	home := withTempHome(t)
	dir := filepath.Join(home, ".config", "cliamp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"path":"/music/a.mp3","position_sec":42,"playlist":"main"}`
	if err := os.WriteFile(filepath.Join(dir, "resume.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	want := State{Path: "/music/a.mp3", PositionSec: 42, Playlist: "main"}
	if got := Load(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveStateCapsLists(t *testing.T) {
	for _, tt := range []struct {
		name               string
		contextLen, index  int
		wantFirst, wantIdx int // first kept track in the original list, and the saved index
	}{
		{"short list kept whole", 10, 4, 0, 4},
		{"active near the start", MaxContextTracks * 3, 20, 0, 20},
		{"active in the middle keeps the lookback", MaxContextTracks * 3, 700, 700 - contextLookback, contextLookback},
		{"active near the end", MaxContextTracks * 3, MaxContextTracks*3 - 1, MaxContextTracks * 2, MaxContextTracks - 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withTempHome(t)
			all := tracksN(tt.contextLen)
			SaveState(State{Path: all[tt.index].Path, PositionSec: 5, Context: all, ContextIndex: tt.index, Queue: tracksN(MaxQueueTracks + 50)})
			got := Load()
			if len(got.Context) != min(tt.contextLen, MaxContextTracks) || got.ContextIndex != tt.wantIdx {
				t.Fatalf("context len %d index %d, want len %d index %d", len(got.Context), got.ContextIndex, min(tt.contextLen, MaxContextTracks), tt.wantIdx)
			}
			if got.Context[0].Path != all[tt.wantFirst].Path || got.Context[got.ContextIndex].Path != got.Path {
				t.Fatalf("window starts at %q (want %q); active %q, want %q", got.Context[0].Path, all[tt.wantFirst].Path, got.Context[got.ContextIndex].Path, got.Path)
			}
			if len(got.Queue) != MaxQueueTracks {
				t.Fatalf("queue len %d, want %d", len(got.Queue), MaxQueueTracks)
			}
		})
	}
}

func TestSaveOverwritesPrevious(t *testing.T) {
	withTempHome(t)

	Save("/a.mp3", 10, "one")
	Save("/b.mp3", 20, "two")

	got := Load()
	if got.Path != "/b.mp3" || got.PositionSec != 20 || got.Playlist != "two" {
		t.Errorf("Load() = %+v, want Path=/b.mp3 PositionSec=20 Playlist=two", got)
	}
}
