package model

// immersive_keys.go owns every keypress while immersive mode is shown. The
// handler is reached before every other screen handler, so bindings here can
// reuse familiar letters without touching normal-mode dispatch. Playback
// verbs (space, </>, z, r, volume) forward to the same helpers the main
// screen uses.

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// handleImmersiveKey processes a key press inside immersive mode.
func (m *Model) handleImmersiveKey(msg tea.KeyPressMsg) tea.Cmd {
	// The full-screen visualizer still layers on top; delegate while it is up.
	if m.fullVis {
		return m.handleFullVisualizerKey(msg)
	}
	if m.immersive.searching || m.immersive.filtering {
		return m.handleImmersiveInputKey(msg)
	}

	switch msg.String() {
	case "I":
		m.exitImmersive()
		return nil
	case "esc":
		return m.immersiveEscape()
	case "backspace":
		m.immersiveGoBack()
		return nil
	case "tab":
		m.immersive.focus = immersivePane((int(m.immersive.focus) + 1) % int(immPaneCount))
		return nil
	case "shift+tab":
		m.immersive.focus = immersivePane((int(m.immersive.focus) + int(immPaneCount) - 1) % int(immPaneCount))
		return nil

	// — playback —
	case "space":
		return m.immTogglePlay()
	case ">", ".":
		return m.immNext()
	case "<", ",":
		return m.immPrev()
	case "z":
		return m.immToggleShuffle()
	case "r":
		return m.immCycleRepeat()
	case "+", "=":
		if m.player != nil {
			m.player.SetVolume(m.player.Volume() + 1)
		}
		return nil
	case "-":
		if m.player != nil {
			m.player.SetVolume(m.player.Volume() - 1)
		}
		return nil
	case "shift+left":
		return m.doSeek(-m.seekStepLarge)
	case "shift+right":
		return m.doSeek(m.seekStepLarge)

	// — frame —
	case "g", "home":
		m.immersive.focus = immPaneCanvas
		m.immersiveHome()
		return nil
	case "q":
		if m.immersive.focus == immPaneQueue {
			m.immersive.focus = immPaneCanvas
		} else {
			m.immersive.focus = immPaneQueue
		}
		return nil
	case "V":
		m.fullVis = true
		return nil

	// — nav pills —
	case "1", "2", "3", "4", "5":
		m.immersiveSetSection(immSection(msg.String()[0] - '1'))
		if m.immersive.section == immSecSearch {
			return m.openImmersiveSearch()
		}
		return nil

	// — canvas —
	case "v":
		m.immersive.mode = immCanvasMode((int(m.immersive.mode) + 1) % int(immCanvasModeCount))
		m.immersive.scroll = 0
		return nil
	case "e":
		if m.immersive.view == immViewSettings {
			m.immersive.view = m.immersive.settingsReturn
		} else {
			m.immersive.settingsReturn = m.immersive.view
			m.immersive.view = immViewSettings
			m.immersive.focus = immPaneCanvas
		}
		return nil
	case "s":
		m.immersive.sort = immBrowseSort((int(m.immersive.sort) + 1) % int(immBrowseSortCount))
		return nil
	case "f":
		if m.immersive.view == immViewBrowse {
			m.immersive.filtering = true
			m.immersive.filter = ""
		}
		return nil
	case "t":
		if m.immersive.view.isTrackView() {
			m.immersive.trackSort = immersiveTrackSort((int(m.immersive.trackSort) + 1) % int(immSortTrackCount))
			m.immersive.scroll = 0
		}
		return nil
	case "p":
		if m.immersive.view.isTrackView() {
			return m.playImmersiveContext(m.immersive.cursor)
		}
		return nil
	case "n":
		return m.immersiveToggleLike()
	case "a":
		return m.immersiveQueueAppend()

	// — search —
	case "/", "ctrl+f":
		return m.openImmersiveSearch()

	case "enter":
		return m.immersiveActivate()

	// — navigation —
	case "j", "down":
		m.immersiveMove(1, 0)
		return nil
	case "k", "up":
		m.immersiveMove(-1, 0)
		return nil
	case "h", "left":
		m.immersiveMove(0, -1)
		return nil
	case "l", "right":
		m.immersiveMove(0, 1)
		return nil
	case "pgdown":
		m.immersiveMove(m.immersivePageStep(), 0)
		return nil
	case "pgup":
		m.immersiveMove(-m.immersivePageStep(), 0)
		return nil
	case "shift+home":
		m.immersiveCursorHome()
		return nil
	}
	return nil
}

// Playback verbs shared by the key handler and the controls-row click
// targets (a click on a box-drawn button is dispatched as its key).
func (m *Model) immTogglePlay() tea.Cmd {
	cmd := m.togglePlayPause()
	m.notifyPlayback()
	return cmd
}

func (m *Model) immNext() tea.Cmd {
	refresh := m.scrobbleCurrent()
	cmd := m.nextTrack()
	m.notifyPlayback()
	return tea.Batch(refresh, cmd)
}

func (m *Model) immPrev() tea.Cmd {
	refresh := m.scrobbleCurrent()
	cmd := m.prevTrack()
	m.notifyPlayback()
	return tea.Batch(refresh, cmd)
}

func (m *Model) immToggleShuffle() tea.Cmd {
	m.playlist.ToggleShuffle()
	m.saveConfigKey("shuffle", boolString(m.playlist.Shuffled()))
	return m.rearmPreload()
}

func (m *Model) immCycleRepeat() tea.Cmd {
	const repeatModes = playlist.RepeatOne + 1
	m.playlist.SetRepeat((m.playlist.Repeat() + 1) % repeatModes)
	m.saveConfigKey("repeat", repeatLabel(m.playlist.Repeat()))
	return nil
}

// openImmersiveSearch activates the Search pill's input field.
func (m *Model) openImmersiveSearch() tea.Cmd {
	if _, ok := m.immersive.prov.(provider.Searcher); !ok {
		m.immersiveSetSection(immSecSearch)
		return nil
	}
	m.immersiveSetSection(immSecSearch)
	m.immersive.searching = true
	m.immersive.searchQuery = ""
	return nil
}

// immersiveEscape peels the innermost state: the settings tab, one
// back-stack level, then the mode itself.
func (m *Model) immersiveEscape() tea.Cmd {
	switch {
	case m.immersive.view == immViewSettings:
		m.immersive.view = m.immersive.settingsReturn
	case len(m.immersive.back) > 0 || m.immersive.view != immViewBrowse && m.immersive.view != immViewSearch:
		m.immersiveGoBack()
	default:
		m.exitImmersive()
	}
	return nil
}

// immersivePageStep is the page-scroll step in the active cursor domain.
func (m Model) immersivePageStep() int {
	g := m.immGeom()
	switch m.immersive.mode {
	case immCanvasRows:
		return max(1, g.canvasIH/immRowsItemH-1)
	case immCanvasGrid:
		tileH := max(3, (g.canvasIW/m.immGridCols(g.canvasIW))/2+2)
		return max(1, g.canvasIH/(tileH+1)-1) * m.immGridCols(g.canvasIW)
	default:
		return max(3, g.canvasIH-1)
	}
}

// immersiveMove applies a (vertical, horizontal) step to the focused region.
func (m *Model) immersiveMove(dy, dx int) {
	switch m.immersive.focus {
	case immPaneNav:
		if dx != 0 {
			sec := immSection(wrapIndex(int(m.immersive.section)+dx, int(immSecCount)))
			m.immersiveSetSection(sec)
		}
		if dy > 0 {
			m.immersive.focus = immPaneCanvas
		}
	case immPaneQueue:
		total := len(m.immQueueRows())
		m.immersive.queueCursor = clampInt(m.immersive.queueCursor+dy+dx, 0, max(0, total-1))
	default:
		m.immersiveMoveCanvas(dy, dx)
	}
}

// immersiveMoveCanvas routes a step inside the canvas: j/k move by item
// (tile row in grid mode); h/l move by item (or tile row in grid mode), and
// adjust the focused value in the settings tab.
func (m *Model) immersiveMoveCanvas(dy, dx int) {
	im := &m.immersive
	if im.view == immViewSettings {
		if dy != 0 {
			im.settingsCursor = clampInt(im.settingsCursor+dy, 0, immSetCount-1)
		}
		if dx != 0 {
			m.immersiveAdjustSetting(im.settingsCursor, dx)
		}
		return
	}
	n := len(m.canvasItems())
	if n == 0 {
		return
	}
	step := dy
	if im.mode == immCanvasGrid {
		cols := m.immGridCols(m.immGeom().canvasIW)
		step = dy*cols + dx
	} else {
		step += dx
	}
	im.cursor = clampInt(im.cursor+step, 0, n-1)
	m.clampCanvasScroll()
}

// clampCanvasScroll re-derives scroll so the cursor row stays visible. In
// grid mode scroll is in tile rows; in list/rows it is in items.
func (m *Model) clampCanvasScroll() {
	im := &m.immersive
	g := m.immGeom()
	n := len(m.canvasItems())
	switch im.mode {
	case immCanvasRows:
		per := max(1, g.canvasIH/immRowsItemH)
		im.scroll = clampedScroll(im.scroll, im.cursor, n, per)
	case immCanvasGrid:
		cols := m.immGridCols(g.canvasIW)
		tileH := max(3, (g.canvasIW/cols)/2+2)
		visible := max(1, g.canvasIH/(tileH+1))
		tileRows := (n + cols - 1) / cols
		im.scroll = clampedScroll(im.scroll, im.cursor/cols, tileRows, visible)
	default:
		im.scroll = clampedScroll(im.scroll, im.cursor, n, g.canvasIH)
	}
}

// immersiveActivate is Enter: pills apply, collections open, tracks play,
// queue rows jump the live queue, and settings rows adjust +1.
func (m *Model) immersiveActivate() tea.Cmd {
	switch m.immersive.focus {
	case immPaneNav:
		if m.immersive.section == immSecSearch {
			return m.openImmersiveSearch()
		}
		return nil
	case immPaneQueue:
		return m.immersiveQueueJump()
	default:
		im := &m.immersive
		if im.view == immViewSettings {
			m.immersiveAdjustSetting(im.settingsCursor, 1)
			return nil
		}
		items := m.canvasItems()
		if im.cursor < 0 || im.cursor >= len(items) {
			return nil
		}
		return m.activateItem(items[im.cursor])
	}
}

// activateItem runs a canvas item: collections open, tracks play.
func (m *Model) activateItem(item immItem) tea.Cmd {
	if item.kind == immKindTrack {
		idx, err := strconv.Atoi(item.id)
		if err != nil || idx < 0 || idx >= len(m.sortedTracks()) {
			return nil
		}
		m.immersive.cursor = idx
		return m.playImmersiveTrack(idx)
	}
	return m.openImmersiveItem(item)
}

// immersiveAdjustSetting applies a +/- adjustment to a settings row.
func (m *Model) immersiveAdjustSetting(row, dir int) {
	if m.player == nil {
		return
	}
	switch {
	case row == immSetPreset:
		m.cycleEQPreset()
	case row >= immSetBand0 && row < immSetBand0+eqBandCount:
		bands := m.player.EQBands()
		m.setCustomEQBand(row-immSetBand0, bands[row-immSetBand0]+float64(dir))
	case row == immSetVol:
		m.player.SetVolume(m.player.Volume() + float64(dir))
	case row == immSetSpeed:
		m.changeSpeed(0.25 * float64(dir))
	case row == immSetVis:
		if m.vis != nil {
			m.vis.CycleMode()
		}
	}
}

// immersiveQueueJump plays the Queue panel row under the queue cursor.
func (m *Model) immersiveQueueJump() tea.Cmd {
	entries := m.immQueueRows()
	if m.immersive.queueCursor < 0 || m.immersive.queueCursor >= len(entries) {
		return nil
	}
	idx := entries[m.immersive.queueCursor].TrackIndex
	if idx < 0 || idx >= m.playlist.Len() {
		return nil
	}
	m.plCursor = idx
	m.playlist.SetIndex(idx)
	m.playlist.Dequeue(idx) // no-op for upcoming (unqueued) rows
	cmd := m.playCurrentTrack()
	m.notifyPlayback()
	return cmd
}

// immersiveQueueAppend adds the focused track to the live queue.
func (m *Model) immersiveQueueAppend() tea.Cmd {
	if !m.immersive.view.isTrackView() {
		return nil
	}
	tracks := m.sortedTracks()
	if m.immersive.cursor < 0 || m.immersive.cursor >= len(tracks) {
		return nil
	}
	track := tracks[m.immersive.cursor]
	if track.Path == "" {
		return nil
	}
	m.playlist.Add(track)
	m.playlist.Queue(m.playlist.Len() - 1)
	m.status.Showf(statusTTLShort, "Queued %s", trackViewName(track))
	return m.rearmPreload()
}

// immersiveToggleLike hearts the focused track or, lacking a track context,
// the playing track — mirroring `n` on the playlist pane.
func (m *Model) immersiveToggleLike() tea.Cmd {
	if m.favMgr == nil {
		return nil
	}
	var track playlist.Track
	var ok bool
	if m.immersive.view.isTrackView() {
		tracks := m.sortedTracks()
		if m.immersive.cursor < len(tracks) {
			track, ok = tracks[m.immersive.cursor], true
		}
	} else {
		track, _ = m.currentPlaybackTrack()
		ok = track.Path != ""
	}
	if !ok {
		return nil
	}
	added, err := m.favMgr.ToggleFavorite(track)
	if err != nil {
		m.status.Errorf(statusTTLDefault, "Favorite failed: %s", err)
		return nil
	}
	m.refreshFavSet()
	if added {
		m.status.Show("Added to favorites "+favAddedMark(), statusTTLShort)
	} else {
		m.status.Show("Removed from favorites "+favRemovedMark(), statusTTLShort)
	}
	return m.fetchProviderPlaylists()
}

// handleImmersiveInputKey drives the two text fields (search pill input and
// the canvas filter). Both are simple line editors.
func (m *Model) handleImmersiveInputKey(msg tea.KeyPressMsg) tea.Cmd {
	field := &m.immersive.searchQuery
	if m.immersive.filtering {
		field = &m.immersive.filter
	}
	switch msg.String() {
	case "esc":
		m.immersive.searching = false
		m.immersive.filtering = false
		return nil
	case "enter":
		if m.immersive.searching {
			m.immersive.searching = false
			return m.runImmersiveSearch()
		}
		m.immersive.filtering = false
		m.immersive.cursor, m.immersive.scroll = 0, 0
		return nil
	case "backspace":
		if *field != "" {
			*field = (*field)[:len(*field)-1]
		}
		return nil
	case "ctrl+u":
		*field = ""
		return nil
	}
	if msg.Text != "" {
		*field += msg.Text
	}
	return nil
}

// runImmersiveSearch fires the provider's track search into the canvas.
func (m *Model) runImmersiveSearch() tea.Cmd {
	s, ok := m.immersive.prov.(provider.Searcher)
	if !ok || m.immersive.searchQuery == "" {
		return nil
	}
	m.pushImmersiveBack()
	m.immersive.view = immViewSearch
	m.immersive.ctxID, m.immersive.ctxName = "", "Search: "+m.immersive.searchQuery
	m.immersive.ctxSub = "Results"
	m.immersive.ctxKind = immKindTrack
	m.immersive.searchLoading = true
	m.immersive.tracks = nil
	m.immersive.tracksLoading = true
	m.immersive.cursor, m.immersive.scroll = 0, 0
	m.immersive.focus = immPaneCanvas
	return fetchImmersiveSearchCmd(context.Background(), s, m.immersive.prov.Name(), m.immersive.searchQuery, nextRequest(&m.requests.immersiveSearch))
}

// immersiveCursorHome snaps the active cursor back to the first row.
func (m *Model) immersiveCursorHome() {
	switch m.immersive.focus {
	case immPaneQueue:
		m.immersive.queueCursor = 0
	default:
		m.immersive.cursor, m.immersive.scroll = 0, 0
		m.immersive.settingsCursor = 0
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// boolString persists a bool config value the same way keys.go does.
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func repeatLabel(mode playlist.RepeatMode) string {
	return "\"" + mode.String() + "\""
}
