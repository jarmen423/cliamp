package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// A late artist result must not land in the playlist view opened after it.
func TestImmersiveStaleArtistResultDropped(t *testing.T) {
	m := immersiveModel(t)
	m.openImmersiveItem(immItem{kind: immKindArtist, id: "ar1", title: "Lizzo"})
	artistGen := m.requests.immersiveArtist
	m.immersiveGoBack()
	m.openImmersiveItem(immItem{kind: immKindPlaylist, id: "pl1", title: "Stuff"})
	updated, _ := m.Update(immersiveArtistMsg{
		detail:       provider.ArtistDetail{Popular: []playlist.Track{{Title: "Wrong"}}},
		providerName: "stub",
		gen:          artistGen,
	})
	got := updated.(Model)
	if got.immersive.view != immViewPlaylist || len(got.immersive.tracks) != 0 {
		t.Fatalf("stale artist result applied: view=%d tracks=%v", got.immersive.view, got.immersive.tracks)
	}
}

// Switching pills drops the opened context, whatever the target section.
func TestImmersiveSectionSwitchClearsContext(t *testing.T) {
	for _, sec := range []immSection{immSecSearch, immSecAlbums} {
		m := immersiveModel(t)
		m.immersive.view = immViewPlaylist
		m.immersive.ctxName = "Stuff"
		m.immersive.tracks = []playlist.Track{{Title: "Song A"}}
		m.immersive.tracksLoading = true
		m.immersiveSetSection(sec)
		im := m.immersive
		if im.ctxName != "" || im.tracks != nil || im.tracksLoading {
			t.Fatalf("section %d kept context: name=%q tracks=%v loading=%v", sec, im.ctxName, im.tracks, im.tracksLoading)
		}
	}
}

// Esc cancels the filter; the filter is shown in the canvas title.
func TestImmersiveFilterVisibleAndCancelled(t *testing.T) {
	m := immersiveModel(t)
	m.handleImmersiveKey(keyMsg("f"))
	m.handleImmersiveKey(keyMsg("z"))
	if title := m.immCanvasTitle(); !strings.Contains(title, "filter: z") {
		t.Fatalf("canvas title %q does not show the filter", title)
	}
	m.handleImmersiveKey(keyMsg("esc"))
	if m.immersive.filter != "" || m.immersive.filtering {
		t.Fatalf("esc left filter=%q filtering=%v", m.immersive.filter, m.immersive.filtering)
	}
}

func TestImmersiveInputBackspaceIsRuneAware(t *testing.T) {
	m := immersiveModel(t)
	m.immersive.searching = true
	m.immersive.searchQuery = "café"
	m.handleImmersiveInputKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.immersive.searchQuery != "caf" {
		t.Fatalf("backspace left %q, want %q", m.immersive.searchQuery, "caf")
	}
}

// The volume hit rect is where the bar is drawn.
func TestImmersiveVolumeBarHitMatchesRender(t *testing.T) {
	m := immersiveMouseModel(t)
	g := m.immGeom()
	_, vol := m.immControlsGeom(g.w)
	row := []rune(stripAnsi(m.renderImmControls(g)[1]))
	bar := string(row[vol.X : vol.X+vol.W])
	if strings.Trim(bar, "▮▯") != "" {
		t.Fatalf("volume rect covers %q, want only bar cells", bar)
	}
}

// With the full-screen visualizer up, the wheel must not move the hidden canvas.
func TestImmersiveWheelIgnoredUnderFullVis(t *testing.T) {
	m := immersiveMouseModel(t)
	m.fullVis = true
	m.handleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.immersive.cursor != 0 {
		t.Fatalf("wheel moved the hidden canvas cursor to %d", m.immersive.cursor)
	}
}

// At the minimum frame the queue panel shows no rows, so it has no hit rows.
func TestImmersiveQueueHitRowsMatchPanel(t *testing.T) {
	m := immersiveMouseModel(t)
	m.width, m.height = immMinWidth, immMinHeight
	m.recomputeLayout()
	m.playlist.Replace([]playlist.Track{{Title: "One"}, {Title: "Two"}, {Title: "Three"}})
	m.View()
	g := m.immGeom()
	if want := max(0, g.queueH-3); m.immMouse.queueN > want {
		t.Fatalf("queue hit rows = %d, panel shows at most %d", m.immMouse.queueN, want)
	}
}
