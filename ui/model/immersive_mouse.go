package model

// immersive_mouse.go wires pointer input into the immersive frame: clicks on
// nav pills, canvas items in all three canvas modes, queue rows, transport
// buttons and the volume bar, plus click/drag seeking on the progress bar.
// Hit-testing reads the geometry recorded each frame into m.immMouse by
// recordImmersiveMouseGeometry — the same layout helpers the renderer uses —
// so a click always lands on the cell the frame drew.
//
// Geometry lives on m.immMouse (a shared pointer, like m.mouse) because
// View() renders from a value copy. The seek row mirrors into
// m.mouse.seekRow/seekX/seekW so the existing drag machinery in mouse.go
// (motion + release) works unchanged.

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bjarneo/cliamp/ui"
)

// immMouseGeom is the per-frame hit geometry of the immersive frame, all in
// content coordinates unless noted: the band rows, pill boxes, body pane
// edges, canvas item boxes, controls buttons, and the art rectangles kept
// for the step-2 image layer.
type immMouseGeom struct {
	valid       bool
	frameX      int // screen cell of content column 0
	topRow      int // screen row of content row 0
	geom        immGeom
	pills       []immPillGeom
	items       []immItemGeom
	ctrls       []immCtrlGeom
	volBar      immRect
	queueY0     int // first queue item row (content row)
	queueN      int // queue rows drawn
	queueScroll int
	artRects    []immRect // now-playing + canvas art boxes, content coords
}

// inside reports whether (x, y) falls in rect r.
func inside(r immRect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// recordImmersiveMouseGeometry maps the immersive frame to absolute screen
// coordinates with the same centering math recordMouseGeometry uses, then
// resolves every hit region through the shared layout helpers.
func (m *Model) recordImmersiveMouseGeometry(content string) {
	ms := m.mouse
	if ms == nil {
		return
	}
	ms.seekRow, ms.bodyRow = -1, -1
	im := m.immMouse
	if im == nil {
		return
	}
	im.valid = false
	if m.fullVis || !m.immersiveShown() {
		return
	}
	frame := ui.FrameStyle.Render(content)
	padTop := max(0, (m.height-lipgloss.Height(frame))/2)
	padLeft := max(0, (m.width-lipgloss.Width(frame))/2)
	im.frameX = padLeft + ui.PaddingH
	im.topRow = padTop + ui.VerticalPadding()

	g := m.immGeom()
	im.geom = g
	im.pills = m.immNavGeom(g.w)
	im.items = m.immCanvasItemsGeom(g.canvasIW, g.canvasIH)
	im.ctrls, im.volBar = m.immControlsGeom(g.w)

	// Queue rows sit inside the queue panel below its border and the
	// "Next from" header.
	total := m.playlist.QueueLen()
	budget := max(1, g.queueH-3)
	im.queueScroll = clampedScroll(m.immersive.queueScroll, m.immersive.queueCursor, total, budget)
	im.queueY0 = g.bodyY + g.queueY + 2
	im.queueN = clampInt(total-im.queueScroll, 0, budget)

	im.artRects = im.artRects[:0]
	if g.npArt.W > 0 && g.npArt.H > 0 {
		im.artRects = append(im.artRects, g.npArt)
	}
	for _, it := range im.items {
		if it.art.W > 0 && it.art.H > 0 {
			im.artRects = append(im.artRects, it.art)
		}
	}

	// The progress bar sits inset between the position and duration labels;
	// record its exact cells so a drag maps 1:1 onto the drawn bar.
	posText := formatTrackTime(int(m.cachedPos.Seconds()))
	durText := formatTrackTime(int(m.cachedDur.Seconds()))
	if durText == "" {
		durText = "--:--"
	}
	if posText == "" {
		posText = "0:00"
	}
	ms.seekRow = im.topRow + g.seekY
	ms.seekX = im.frameX + lipgloss.Width(posText) + 1
	ms.seekW = max(4, g.w-lipgloss.Width(posText)-lipgloss.Width(durText)-2)

	im.valid = true
}

// handleImmersiveClick dispatches a button-down event inside immersive mode.
// Left clicks act (open/play/seek); right clicks select without acting, and
// queue the focused canvas track.
func (m *Model) handleImmersiveClick(msg tea.MouseClickMsg) tea.Cmd {
	im := m.immMouse
	if im == nil || !im.valid {
		return nil
	}
	if msg.Button != tea.MouseLeft && msg.Button != tea.MouseRight {
		return nil
	}
	cx, cy := msg.X-im.frameX, msg.Y-im.topRow
	g := im.geom
	if cx < 0 || cx >= g.w || cy < 0 || cy >= g.h {
		return nil
	}
	right := msg.Button == tea.MouseRight
	switch {
	case cy < immVisRows: // visualizer band → full-screen visualizer
		if right {
			return nil
		}
		m.fullVis = true
		return nil
	case cy == g.seekY:
		if right {
			return nil
		}
		m.mouse.dragging = true
		return m.seekToBarCell(msg.X - m.mouse.seekX)
	case cy >= g.ctrlY && cy < g.ctrlY+immCtrlRows:
		return m.immClickControls(cx, cy)
	case cy >= immNavY && cy < immNavY+immNavRows:
		return m.immClickNav(cx, right)
	case cy >= g.bodyY && cy < g.bodyY+g.bodyH:
		switch {
		case cx < g.leftW:
			return m.immClickLeft(cx, cy, right)
		case cx >= g.canvasX:
			return m.immClickCanvas(cx, cy, right)
		}
	}
	return nil
}

// immClickNav applies a pill click: collections switch the canvas, the
// search pill opens its input.
func (m *Model) immClickNav(cx int, right bool) tea.Cmd {
	if right {
		return nil
	}
	for _, p := range m.immMouse.pills {
		if cx >= p.box.X && cx < p.box.X+p.box.W {
			if p.section == immSecSearch {
				return m.openImmersiveSearch()
			}
			m.immersiveSetSection(p.section)
			return nil
		}
	}
	return nil
}

// immersiveControl runs a transport control by its key label; the buttons in
// the controls row call this directly so clicks share the key handlers' path
// without depending on the msg's String() form.
func (m *Model) immersiveControl(key string) tea.Cmd {
	switch key {
	case " ":
		return m.immTogglePlay()
	case ">":
		return m.immNext()
	case "<":
		return m.immPrev()
	case "z":
		return m.immToggleShuffle()
	case "r":
		return m.immCycleRepeat()
	}
	return nil
}

// immClickControls routes a transport-button click through the matching key
// so clicks and keys share one code path; the volume bar sets directly.
func (m *Model) immClickControls(cx, cy int) tea.Cmd {
	im := m.immMouse
	for _, b := range im.ctrls {
		if inside(b.box, cx, cy) {
			return m.immersiveControl(b.key)
		}
	}
	if inside(im.volBar, cx, cy) && m.player != nil {
		frac := float64(cx-im.volBar.X) / float64(max(1, im.volBar.W-1))
		m.player.SetVolume(m.player.VolumeMin() + frac*(6-m.player.VolumeMin()))
	}
	return nil
}

// immClickLeft hits the Now Playing panel (selects nothing) or a queue row
// (left click jumps the queue there).
func (m *Model) immClickLeft(cx, cy int, right bool) tea.Cmd {
	im := m.immMouse
	if cy < im.queueY0 || cy >= im.queueY0+im.queueN {
		return nil
	}
	idx := im.queueScroll + cy - im.queueY0
	m.immersive.focus = immPaneQueue
	m.immersive.queueCursor = idx
	if right {
		return nil
	}
	return m.immersiveQueueJump()
}

// immClickCanvas hits a drawn canvas item: collections open, tracks play,
// right click queues a track. In the settings tab a click selects (and
// adjusts) the row.
func (m *Model) immClickCanvas(cx, cy int, right bool) tea.Cmd {
	im := m.immMouse
	g := im.geom
	if cx < g.canvasX+1 || cy < g.bodyY+1 || cy >= g.bodyY+1+g.canvasIH {
		return nil
	}
	m.immersive.focus = immPaneCanvas
	if m.immersive.view == immViewSettings {
		m.immersive.settingsCursor = clampInt(cy-(g.bodyY+1), 0, immSetCount-1)
		if right {
			return nil
		}
		m.immersiveAdjustSetting(m.immersive.settingsCursor, 1)
		return nil
	}
	for _, it := range im.items {
		if inside(it.box, cx, cy) {
			m.immersive.cursor = it.idx
			m.clampCanvasScroll()
			if right {
				return m.immersiveQueueAppend()
			}
			return m.activateItem(m.canvasItems()[it.idx])
		}
	}
	return nil
}

// immersiveWheel implements snap scrolling: one wheel notch moves the
// cursor under the pointer by one item (one tile row in grid mode). The
// queue column scrolls its own list; everything else is a no-op.
func (m *Model) immersiveWheel(msg tea.MouseWheelMsg) tea.Cmd {
	im := m.immMouse
	if im == nil || !im.valid {
		return nil
	}
	dy := 1
	if msg.Button == tea.MouseWheelUp {
		dy = -1
	}
	cx, cy := msg.X-im.frameX, msg.Y-im.topRow
	g := im.geom
	if cy < g.bodyY || cy >= g.bodyY+g.bodyH {
		return nil
	}
	switch {
	case cx < g.leftW:
		total := m.playlist.QueueLen()
		m.immersive.focus = immPaneQueue
		m.immersive.queueCursor = clampInt(m.immersive.queueCursor+dy, 0, max(0, total-1))
		m.immersive.queueScroll = clampedScroll(m.immersive.queueScroll, m.immersive.queueCursor, total, max(1, g.queueH-3))
	case cx >= g.canvasX:
		if m.immersive.view == immViewSettings {
			m.immersive.settingsCursor = clampInt(m.immersive.settingsCursor+dy, 0, immSetCount-1)
			return nil
		}
		step := dy
		if m.immersive.mode == immCanvasGrid {
			step = dy * m.immGridCols(g.canvasIW)
		}
		n := len(m.canvasItems())
		m.immersive.focus = immPaneCanvas
		m.immersive.cursor = clampInt(m.immersive.cursor+step, 0, max(0, n-1))
		m.clampCanvasScroll()
	}
	return nil
}
