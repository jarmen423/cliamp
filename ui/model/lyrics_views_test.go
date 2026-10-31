package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/lyrics"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/ui"
)

var lyricsTestTrack = playlist.Track{Path: "/a.flac", Title: "Song", Artist: "Band"}

// lyricsTestModel plays lyricsTestTrack at pos.
func lyricsTestModel(pos time.Duration) Model {
	m := keybindingTestModel()
	m.playlist.Replace([]playlist.Track{lyricsTestTrack})
	m.playlist.SetIndex(0)
	m.cachedPos = pos
	return m
}

func syncedLyricsLines(n int) []lyrics.Line {
	lines := make([]lyrics.Line, n)
	for i := range lines {
		lines[i] = lyrics.Line{Start: time.Duration(i+1) * time.Second, Text: "line " + strings.Repeat("x", i%3) + string(rune('a'+i%26))}
	}
	return lines
}

func TestFullVisLyricsCycle(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  []fullVisLyricsMode
	}{
		{"wide terminal splits", 120, []fullVisLyricsMode{fullVisLyricsSplit, fullVisLyricsOnly, fullVisLyricsOff}},
		{"narrow terminal skips the split", 60, []fullVisLyricsMode{fullVisLyricsOnly, fullVisLyricsOff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLayoutTestModel(tt.width, 40)
			m.fullVis = true
			m.recomputeLayout()
			for _, want := range tt.want {
				m.handleFullVisualizerKey(keyMsg("y"))
				if got := m.fullVisLyricsNow(); got != want {
					t.Fatalf("mode = %v, want %v", got, want)
				}
				wantCols, wantRows := ui.PanelWidth, m.layout.fullVisualizerRows
				switch want {
				case fullVisLyricsSplit:
					wantCols = ui.PanelWidth * 3 / 5
				case fullVisLyricsOnly:
					wantRows = 0
				}
				if m.vis.Cols != wantCols || m.vis.Rows != wantRows {
					t.Fatalf("mode %v: vis = %dx%d, want %dx%d", want, m.vis.Cols, m.vis.Rows, wantCols, wantRows)
				}
			}
		})
	}
}

func TestFullVisSplitNeedsVisualizer(t *testing.T) {
	m := newLayoutTestModel(120, 40)
	m.fullVis = true
	m.vis.Mode = ui.VisNone
	m.recomputeLayout()
	m.handleFullVisualizerKey(keyMsg("y"))
	if got := m.fullVisLyricsNow(); got != fullVisLyricsOnly {
		t.Fatalf("mode = %v, want lyrics only with the visualizer off", got)
	}
}

func TestFullVisContentFillsPanel(t *testing.T) {
	for _, mode := range []fullVisLyricsMode{fullVisLyricsSplit, fullVisLyricsOnly} {
		m := newLayoutTestModel(120, 40)
		m.fullVis = true
		m.lyrics.fullVis = mode
		m.lyrics.lines = syncedLyricsLines(80)
		m.recomputeLayout()
		rows := strings.Split(m.fullVisContent(), "\n")
		if len(rows) != m.layout.fullVisualizerRows {
			t.Fatalf("mode %v: %d rows, want %d", mode, len(rows), m.layout.fullVisualizerRows)
		}
		for i, row := range rows {
			if w := lipgloss.Width(row); w != ui.PanelWidth {
				t.Fatalf("mode %v: row %d is %d cells, want %d", mode, i, w, ui.PanelWidth)
			}
		}
	}
}

func TestFullVisPixelModeTextFitsSplit(t *testing.T) {
	m := newLayoutTestModel(120, 40)
	m.fullVis = true
	m.lyrics.fullVis = fullVisLyricsSplit
	m.vis.Mode = ui.VisAurora
	m.recomputeLayout()
	for i, row := range strings.Split(m.fullVisContent(), "\n") {
		if w := lipgloss.Width(row); w != ui.PanelWidth {
			t.Fatalf("row %d is %d cells, want %d", i, w, ui.PanelWidth)
		}
	}
}

func TestFullVisLyricsKeysOnlyWhileShown(t *testing.T) {
	m := newLayoutTestModel(120, 40)
	m.fullVis = true
	m.recomputeLayout()
	m.lyrics.lines = []lyrics.Line{{Text: "one"}, {Text: "two"}}
	m.handleFullVisualizerKey(keyMsg("j"))
	if m.lyrics.scroll != 0 {
		t.Fatalf("j with lyrics off: scroll = %d, want 0", m.lyrics.scroll)
	}
	m.lyrics.fullVis = fullVisLyricsSplit
	m.handleFullVisualizerKey(keyMsg("j"))
	if m.lyrics.scroll != 1 {
		t.Fatalf("j with lyrics on: scroll = %d, want 1", m.lyrics.scroll)
	}
}

func TestLyricsPaneFollowsPlayback(t *testing.T) {
	const rows, w = 7, 40
	tests := []struct {
		name       string
		pos        time.Duration
		wantActive string // the active row's text; "" when no line is active yet
		wantFirst  string
	}{
		{"before the first line", 0, "", "line a"},
		{"start of the track", 2 * time.Second, "line xb", "line a"},
		{"middle stays centered", 11 * time.Second, "line xk", "line xh"},
		{"end fills from above", 20 * time.Second, "line xt", "line xn"},
	}
	lines := syncedLyricsLines(20)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := lyricsTestModel(tt.pos)
			m.lyrics.lines = lines
			got := m.lyricsPane(w, rows)
			if len(got) != rows {
				t.Fatalf("got %d rows, want %d", len(got), rows)
			}
			if first := strings.TrimSpace(ansi.Strip(got[0])); first != tt.wantFirst {
				t.Fatalf("first row = %q, want %q", first, tt.wantFirst)
			}
			if tt.wantActive != "" {
				found := false
				for _, row := range got {
					found = found || strings.TrimSpace(ansi.Strip(row)) == tt.wantActive
				}
				if !found {
					t.Fatalf("active line %q is not in %q", tt.wantActive, got)
				}
			}
		})
	}
}

func TestLyricsPaneWrapsToWidth(t *testing.T) {
	m := lyricsTestModel(2 * time.Second)
	long := strings.TrimSpace(strings.Repeat("word ", 20))
	m.lyrics.lines = []lyrics.Line{
		{Start: time.Second, Text: long},
		{Start: 3 * time.Second, Text: ""},
		{Start: 4 * time.Second, Text: long},
	}
	const w, rows = 22, 8
	got := m.lyricsPane(w, rows)
	if len(got) != rows {
		t.Fatalf("got %d rows, want %d", len(got), rows)
	}
	sawNote := false
	for _, row := range got {
		if width := lipgloss.Width(row); width > w {
			t.Fatalf("row %q is %d cells, want at most %d", row, width, w)
		}
		sawNote = sawNote || strings.TrimSpace(ansi.Strip(row)) == "♪"
	}
	if !sawNote {
		t.Fatalf("instrumental break missing from %q", got)
	}
}

func TestImmersiveLyricsTab(t *testing.T) {
	m := immersiveModel(t)
	m.immersive.view = immViewPlaylist

	m.handleImmersiveKey(keyMsg("y"))
	if m.immersive.view != immViewLyrics || m.immersive.focus != immPaneCanvas {
		t.Fatalf("y: view = %d focus = %d, want lyrics in the canvas", m.immersive.view, m.immersive.focus)
	}
	if got := m.immersiveResumeView(); got == nil || got.View != "playlist" {
		t.Fatalf("lyrics tab saved %+v, want the playlist under it", got)
	}
	if !m.lyricsWanted() {
		t.Fatal("the lyrics tab must keep lookups running")
	}

	// Switching between the tabs keeps the page under them.
	m.handleImmersiveKey(keyMsg("e"))
	if m.immersive.view != immViewSettings {
		t.Fatalf("e: view = %d, want settings", m.immersive.view)
	}
	m.handleImmersiveKey(keyMsg("y"))
	if m.immersive.view != immViewLyrics {
		t.Fatalf("y from settings: view = %d, want lyrics", m.immersive.view)
	}
	m.handleImmersiveKey(keyMsg("esc"))
	if m.immersive.view != immViewPlaylist {
		t.Fatalf("esc: view = %d, want playlist", m.immersive.view)
	}
	if m.lyricsWanted() {
		t.Fatal("no lyrics view is open")
	}
}

func TestImmersiveLyricsTabScrollsUnsynced(t *testing.T) {
	m := immersiveModel(t)
	m.lyrics.lines = []lyrics.Line{{Text: "one"}, {Text: "two"}, {Text: "three"}}
	m.handleImmersiveKey(keyMsg("y"))
	m.handleImmersiveKey(keyMsg("j"))
	if m.lyrics.scroll != 1 {
		t.Fatalf("scroll = %d, want 1", m.lyrics.scroll)
	}
	inner := m.renderImmCanvasInner(40, 5)
	if len(inner) == 0 || !strings.Contains(ansi.Strip(inner[0]), "two") {
		t.Fatalf("canvas = %q, want it to start at the scrolled line", inner)
	}
}

func TestEnsureLyrics(t *testing.T) {
	query := lyricsLookupKey(lyricsTestTrack, lyricsTestTrack.Artist, lyricsTestTrack.Title)
	on := fullVisLyricsSplit
	tests := []struct {
		name  string
		state lyricsState
		fetch bool
	}{
		{"no view open", lyricsState{}, false},
		{"view open with nothing loaded", lyricsState{fullVis: on}, true},
		{"already loaded", lyricsState{fullVis: on, query: query, lines: []lyrics.Line{{Text: "one"}}}, false},
		{"already loading", lyricsState{fullVis: on, query: query, loading: true}, false},
		{"known to have none", lyricsState{fullVis: on, query: query, err: lyrics.ErrNotFound}, false},
		{"last lookup failed", lyricsState{fullVis: on, query: query, err: errors.New("offline")}, true},
		{"loaded for another track", lyricsState{fullVis: on, query: "other", lines: []lyrics.Line{{Text: "one"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := lyricsTestModel(0)
			m.fullVis = true
			m.lyrics = tt.state
			if got := m.ensureLyrics() != nil; got != tt.fetch {
				t.Fatalf("fetch = %v, want %v", got, tt.fetch)
			}
		})
	}
}

func TestUpdateLooksUpLyricsWhenAViewOpens(t *testing.T) {
	m := newLayoutTestModel(120, 40)
	m.playlist.Replace([]playlist.Track{lyricsTestTrack})
	m.playlist.SetIndex(0)
	m.fullVis = true
	m.recomputeLayout()

	next, cmd := m.Update(keyMsg("y"))
	got := next.(Model)
	if !got.lyrics.loading || cmd == nil {
		t.Fatalf("loading = %v, cmd nil = %v, want a lookup started", got.lyrics.loading, cmd == nil)
	}

	// The view stays open with no lyrics: later messages must not look again.
	got.lyrics.loading = false
	got.lyrics.err = lyrics.ErrNotFound
	next, _ = got.Update(keyMsg("y"))
	if got = next.(Model); got.lyrics.loading {
		t.Fatal("moving between lyrics modes started another lookup")
	}
}

func TestLyricsLoadedAfterViewClosedAreKept(t *testing.T) {
	m := newLayoutTestModel(120, 40)
	m.lyrics.query = "q"
	m.lyrics.loading = true

	next, _ := m.Update(lyricsLoadedMsg{gen: m.requests.lyrics, query: "q", lines: []lyrics.Line{{Text: "one"}}})
	got := next.(Model)
	if got.lyrics.loading || len(got.lyrics.lines) != 1 {
		t.Fatalf("loading = %v lines = %v, want the track's lyrics kept for the next view", got.lyrics.loading, got.lyrics.lines)
	}
}
