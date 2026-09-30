package model

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/playlist"
)

// immersiveMouseModel builds an immersive model with mouse state attached and
// records a frame, so clicks hit the geometry View just drew.
func immersiveMouseModel(t *testing.T) *Model {
	t.Helper()
	m := immersiveModel(t)
	m.mouse = &mouseState{}
	m.immMouse = &immMouseGeom{}
	m.player = &playbackFakeEngine{seekable: true}
	m.cachedDur = 3 * time.Minute
	m.View()
	if m.immMouse == nil || !m.immMouse.valid {
		t.Fatal("View did not record immersive mouse geometry")
	}
	return m
}

// immAt converts content coordinates to screen coordinates of the recorded frame.
func immAt(m *Model, cx, cy int) (int, int) {
	return m.immMouse.frameX + cx, m.immMouse.topRow + cy
}

func immClickAt(m *Model, cx, cy int, btn tea.MouseButton) tea.Cmd {
	x, y := immAt(m, cx, cy)
	return m.handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: btn})
}

func TestImmersiveSeekClickAndDrag(t *testing.T) {
	m := immersiveMouseModel(t)
	fake := m.player.(*playbackFakeEngine)
	col := m.mouse.seekW / 2
	want := time.Duration(float64(col) / float64(m.mouse.seekW-1) * float64(m.cachedDur))

	m.handleMouseClick(tea.MouseClickMsg{X: m.mouse.seekX + col, Y: m.mouse.seekRow, Button: tea.MouseLeft})

	if len(fake.seekCalls) != 1 {
		t.Fatalf("seek calls = %d, want 1", len(fake.seekCalls))
	}
	if got := fake.seekCalls[0]; got != want {
		t.Fatalf("seek target = %s, want %s", got, want)
	}
	if !m.mouse.dragging {
		t.Fatal("click on immersive seek bar did not start a drag")
	}
	m.handleMouseMotion(tea.MouseMotionMsg{X: m.mouse.seekX + m.mouse.seekW - 1, Y: m.mouse.seekRow})
	if got := fake.seekCalls[len(fake.seekCalls)-1]; got != m.cachedDur {
		t.Fatalf("drag target = %s, want %s", got, m.cachedDur)
	}
	m.handleMouseRelease()
	if m.mouse.dragging {
		t.Fatal("release did not end the drag")
	}
}

func TestImmersivePillClicks(t *testing.T) {
	m := immersiveMouseModel(t)
	for _, p := range m.immMouse.pills {
		if p.section == immSecSearch || p.section < 0 {
			continue // search opens the input and history buttons navigate; checked below
		}
		x, y := immAt(m, p.box.X+1, p.box.Y+1)
		m.handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if m.immersive.section != p.section {
			t.Fatalf("pill %d: section = %d", p.section, m.immersive.section)
		}
	}
	// The Search pill opens the input field (the stub implements Searcher).
	for _, p := range m.immMouse.pills {
		if p.section == immSecSearch {
			x, y := immAt(m, p.box.X+1, p.box.Y+1)
			m.handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if m.immersive.section != immSecSearch || !m.immersive.searching {
				t.Fatal("search pill click did not open the search input")
			}
		}
	}
}

func TestImmersiveCanvasClickOpensPlaylist(t *testing.T) {
	m := immersiveMouseModel(t)
	if len(m.immMouse.items) == 0 {
		t.Fatal("no canvas items recorded")
	}
	it := m.immMouse.items[0]
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseLeft)
	if m.immersive.view != immViewPlaylist {
		t.Fatalf("view = %d, want playlist", m.immersive.view)
	}
	if m.immersive.ctxID != "pl1" || !m.immersive.tracksLoading {
		t.Fatalf("ctxID = %q loading=%v", m.immersive.ctxID, m.immersive.tracksLoading)
	}
}

func TestImmersiveCanvasClickPlaysTrack(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind = immKindTrack
	m.immersive.tracks = []playlist.Track{
		{Title: "one", Path: "/1"}, {Title: "two", Path: "/2"},
	}
	m.View()
	fake := m.player.(*playbackFakeEngine)
	it := m.immMouse.items[1]
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseLeft)
	if len(fake.playCalls) != 0 || m.immersive.cursor != 1 {
		t.Fatalf("single click: playCalls = %v cursor = %d, want selection only", fake.playCalls, m.immersive.cursor)
	}
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseLeft)
	if len(fake.playCalls) != 1 || fake.playCalls[0] != "/2" {
		t.Fatalf("double click: playCalls = %v, want [/2]", fake.playCalls)
	}
}

// Two clicks further apart than the double-click window are two single
// clicks: the track stays selected and nothing plays.
func TestImmersiveSlowSecondClickDoesNotPlay(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind = immKindTrack
	m.immersive.tracks = []playlist.Track{{Title: "one", Path: "/1"}}
	m.View()
	now := time.Unix(1000, 0)
	old := clickNow
	clickNow = func() time.Time { return now }
	t.Cleanup(func() { clickNow = old })

	it := m.immMouse.items[0]
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseLeft)
	now = now.Add(doubleClickWindow + time.Millisecond)
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseLeft)
	if fake := m.player.(*playbackFakeEngine); len(fake.playCalls) != 0 {
		t.Fatalf("playCalls = %v, want none for two slow clicks", fake.playCalls)
	}
}

func TestImmersiveVisualizerNeedsDoubleClick(t *testing.T) {
	m := immersiveMouseModel(t)
	if m.immMouse.geom.visH == 0 {
		t.Skip("no visualizer band at this size")
	}
	immClickAt(m, 2, 0, tea.MouseLeft)
	if m.fullVis {
		t.Fatal("single click opened the full-screen visualizer")
	}
	immClickAt(m, 2, 0, tea.MouseLeft)
	if !m.fullVis {
		t.Fatal("double click did not open the full-screen visualizer")
	}
}

func TestImmersiveRightClickOpensTrackMenu(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind = immKindTrack
	m.immersive.tracks = []playlist.Track{
		{Title: "one", Path: "/1"}, {Title: "two", Path: "/2"},
	}
	m.View()
	it := m.immMouse.items[1]
	immClickAt(m, it.box.X+1, it.box.Y, tea.MouseRight)
	if !m.trackMenu.visible || m.trackMenu.track.Path != "/2" {
		t.Fatalf("menu visible=%v track=%q, want the clicked track /2", m.trackMenu.visible, m.trackMenu.track.Path)
	}
	if m.activeScreen() != screenTrackMenu {
		t.Fatalf("active screen = %d, want the track menu over immersive", m.activeScreen())
	}
}

func TestImmersiveControlsClick(t *testing.T) {
	m := immersiveMouseModel(t)
	fake := m.player.(*playbackFakeEngine)
	fake.playing = true
	fake.paused = false
	var clicked bool
	for _, b := range m.immMouse.ctrls {
		if b.key != " " {
			continue
		}
		immClickAt(m, b.box.X+2, b.box.Y+1, tea.MouseLeft)
		clicked = true
	}
	if !clicked {
		t.Fatal("no play button recorded")
	}
	if !fake.paused {
		t.Fatal("play/pause button click did not toggle pause")
	}
}

// The player reports IsPlaying while paused, so the button must also check
// IsPaused: a paused track shows the play glyph.
func TestImmersivePlayButtonGlyphFollowsPause(t *testing.T) {
	m := immersiveMouseModel(t)
	fake := m.player.(*playbackFakeEngine)
	gl := m.immGlyphs()
	btns, _ := m.immControlsGeom(m.immGeom().w)
	var play immRect
	for _, b := range btns {
		if b.key == " " {
			play = b.box
		}
	}
	for _, tt := range []struct {
		playing, paused bool
		want            string
	}{
		{true, false, gl.pause},
		{true, true, gl.play},
		{false, false, gl.play},
	} {
		fake.playing, fake.paused = tt.playing, tt.paused
		mid := []rune(ansi.Strip(m.renderImmControls(m.immGeom())[1]))
		got := strings.TrimSpace(string(mid[play.X+1 : play.X+play.W-1]))
		if got != tt.want {
			t.Errorf("playing=%v paused=%v: play button shows %q, want %q", tt.playing, tt.paused, got, tt.want)
		}
	}
}

func TestImmersiveQueueClickJumps(t *testing.T) {
	m := immersiveMouseModel(t)
	tracks := []playlist.Track{
		{Title: "one", Path: "/1"}, {Title: "two", Path: "/2"}, {Title: "three", Path: "/3"},
	}
	for i := range tracks {
		m.playlist.Add(tracks[i])
	}
	m.playlist.Queue(0)
	m.playlist.Queue(1)
	m.View()
	im := m.immMouse
	if im.queueN < 2 {
		t.Fatalf("queue rows drawn = %d, want >= 2", im.queueN)
	}
	fake := m.player.(*playbackFakeEngine)
	immClickAt(m, 2, im.queueY0+1, tea.MouseLeft)
	if len(fake.playCalls) != 0 || m.immersive.queueCursor != 1 {
		t.Fatalf("single click: playCalls = %v queueCursor = %d, want selection only", fake.playCalls, m.immersive.queueCursor)
	}
	immClickAt(m, 2, im.queueY0+1, tea.MouseLeft)
	if len(fake.playCalls) == 0 {
		t.Fatal("queue double click did not play")
	}
	if fake.playCalls[len(fake.playCalls)-1] != "/2" {
		t.Fatalf("played %q, want /2", fake.playCalls[len(fake.playCalls)-1])
	}
}

func TestImmersiveWheelSnap(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind = immKindTrack
	m.immersive.tracks = make([]playlist.Track, 40)
	for i := range m.immersive.tracks {
		m.immersive.tracks[i] = playlist.Track{Title: "t", Path: "/t"}
	}
	m.View()
	g := m.immMouse.geom
	// wheel down once over the canvas: cursor moves exactly one item.
	x, y := immAt(m, g.canvasX+2, g.bodyY+1)
	m.handleMouseWheel(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if m.immersive.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (snap scroll)", m.immersive.cursor)
	}
	// wheel up over the queue column, empty queue: no crash, cursor clamps.
	m.handleMouseWheel(tea.MouseWheelMsg{X: m.immMouse.frameX + 1, Y: y, Button: tea.MouseWheelUp})
	if m.immersive.queueCursor < 0 {
		t.Fatal("queue cursor underflowed")
	}
}

func TestImmersiveWheelGridStepsTileRow(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.mode = immCanvasGrid
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind = immKindTrack
	m.immersive.tracks = make([]playlist.Track, 40)
	for i := range m.immersive.tracks {
		m.immersive.tracks[i] = playlist.Track{Title: "t", Path: "/t"}
	}
	m.View()
	g := m.immMouse.geom
	cols := m.immGridCols(g.canvasIW)
	x, y := immAt(m, g.canvasX+2, g.bodyY+1)
	m.handleMouseWheel(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if m.immersive.cursor != cols {
		t.Fatalf("cursor = %d, want %d (one tile row)", m.immersive.cursor, cols)
	}
}

func TestImmersiveClickOutsideFrame(t *testing.T) {
	m := immersiveMouseModel(t)
	m.handleMouseClick(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m.handleMouseClick(tea.MouseClickMsg{X: m.width - 1, Y: m.height - 1, Button: tea.MouseMiddle})
	if m.immersive.view != immViewBrowse {
		t.Fatal("stray click mutated state")
	}
}

func TestImmersiveHistoryButtons(t *testing.T) {
	m := immersiveMouseModel(t)
	click := func(section immSection) {
		t.Helper()
		for _, p := range m.immMouse.pills {
			if p.section == section {
				x, y := immAt(m, p.box.X+1, p.box.Y+1)
				m.handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m.View()
				return
			}
		}
		t.Fatalf("no pill %d", section)
	}
	click(immSecAlbums)
	click(immSecArtists)
	click(immNavBack)
	if m.immersive.section != immSecAlbums {
		t.Fatalf("back landed on section %d, want Albums", m.immersive.section)
	}
	click(immNavBack)
	if m.immersive.section != immSecPlaylists {
		t.Fatalf("second back landed on section %d, want Playlists", m.immersive.section)
	}
	click(immNavForward)
	click(immNavForward)
	if m.immersive.section != immSecArtists {
		t.Fatalf("forward landed on section %d, want Artists", m.immersive.section)
	}
	click(immSecPodcasts)
	if len(m.immersive.fwd) != 0 {
		t.Fatal("a new navigation must clear the forward history")
	}
}
