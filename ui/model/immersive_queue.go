package model

// immersive_queue.go is the Queue page in the immersive canvas, modelled on
// Spotify's: "Now playing", "Next in queue" (the play-next queue) and "Next
// up from <context>" (the rest of the context in play order, shuffled order
// when shuffle is on). Rows are rebuilt from the live playlist every frame,
// so the page follows playback. Section labels are header items the cursor
// skips; the page always renders as a list.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
)

// immQueueViewUpcoming caps the "Next up" rows the page lists.
const immQueueViewUpcoming = 200

// Queue-page row ids: section, then the queue position (queued rows) or the
// playlist track index (upcoming rows).
const (
	immQRowPlaying  = "np"
	immQRowQueued   = "q"
	immQRowUpcoming = "u"
)

// immQueueViewItems lists the queue page's sections and rows.
func (m Model) immQueueViewItems() []immItem {
	var items []immItem
	if t, _ := m.currentPlaybackTrack(); t.Path != "" {
		items = append(items, immItem{kind: immKindHeader, title: "Now playing"}, queueRowItem(immQRowPlaying, 0, t))
	}
	if queued := m.playlist.QueueEntries(); len(queued) > 0 {
		items = append(items, immItem{kind: immKindHeader, title: "Next in queue"})
		for i, e := range queued {
			items = append(items, queueRowItem(immQRowQueued, i, e.Track))
		}
	}
	if next := m.playlist.Upcoming(immQueueViewUpcoming); len(next) > 0 {
		title := "Next up"
		if ctx := m.playingContextName(); ctx != "" {
			title = "Next up from " + ctx
		}
		items = append(items, immItem{kind: immKindHeader, title: title})
		for _, e := range next {
			items = append(items, queueRowItem(immQRowUpcoming, e.TrackIndex, e.Track))
		}
	}
	return items
}

func queueRowItem(section string, n int, t playlist.Track) immItem {
	return immItem{
		kind:  immKindTrack,
		id:    section + ":" + strconv.Itoa(n),
		title: firstNonEmpty(t.Title, trackViewName(t)),
		sub:   t.Artist,
		sub2:  t.Album,
		dur:   t.DurationSecs,
		path:  t.Path,
		art:   t.AlbumArtURL,
	}
}

// queueRowRef splits a queue-page row id into its section and number.
func queueRowRef(id string) (string, int) {
	section, num, ok := strings.Cut(id, ":")
	n, err := strconv.Atoi(num)
	if !ok || err != nil {
		return "", -1
	}
	return section, n
}

// openImmersiveQueueView shows the queue page, recording history so Back
// and Esc return to the previous view. Q again goes back.
func (m *Model) openImmersiveQueueView() tea.Cmd {
	if m.immersive.view == immViewQueue {
		return m.immersiveGoBack()
	}
	m.pushImmersiveBack()
	m.dropImmersiveFetches()
	m.immersive.view = immViewQueue
	m.immersive.ctxID, m.immersive.ctxName, m.immersive.ctxSub = "", "Queue", ""
	m.immersive.focus = immPaneCanvas
	m.immersive.cursor, m.immersive.scroll = 0, 0
	m.immQueueViewSkipHeader(1)
	return nil
}

// immQueueViewSkipHeader moves the cursor off a section label, in direction
// dir first and the other way at the ends of the list.
func (m *Model) immQueueViewSkipHeader(dir int) {
	items := m.canvasItems()
	c := clampInt(m.immersive.cursor, 0, max(0, len(items)-1))
	for _, d := range []int{dir, -dir} {
		for i := c; i >= 0 && i < len(items); i += d {
			if items[i].kind != immKindHeader {
				m.immersive.cursor = i
				m.clampCanvasScroll()
				return
			}
		}
	}
}

// immQueueViewActivate plays the row: a queued or upcoming track jumps the
// live queue there; the playing row and labels do nothing.
func (m *Model) immQueueViewActivate(item immItem) tea.Cmd {
	section, n := queueRowRef(item.id)
	switch section {
	case immQRowQueued:
		entries := m.playlist.QueueEntries()
		if n < 0 || n >= len(entries) {
			return nil
		}
		return m.immJumpToTrack(entries[n].TrackIndex)
	case immQRowUpcoming:
		return m.immJumpToTrack(n)
	}
	return nil
}

// immQueueViewFocused resolves the focused queue-page row for the track
// menu; queued rows can be removed from the queue.
func (m Model) immQueueViewFocused() (playlist.Track, menuRemoveKind, int, bool) {
	items := m.canvasItems()
	c := m.immersive.cursor
	if c < 0 || c >= len(items) {
		return playlist.Track{}, menuRemoveNone, 0, false
	}
	section, n := queueRowRef(items[c].id)
	switch section {
	case immQRowPlaying:
		t, _ := m.currentPlaybackTrack()
		return t, menuRemoveNone, 0, t.Path != ""
	case immQRowQueued:
		if entries := m.playlist.QueueEntries(); n >= 0 && n < len(entries) {
			return entries[n].Track, menuRemoveQueue, n, true
		}
	case immQRowUpcoming:
		if t, ok := m.playlist.Track(n); ok {
			return t, menuRemoveNone, 0, true
		}
	}
	return playlist.Track{}, menuRemoveNone, 0, false
}

// immQueueViewRemove is x: drop the focused row from the play-next queue.
func (m *Model) immQueueViewRemove() tea.Cmd {
	items := m.canvasItems()
	c := m.immersive.cursor
	if c < 0 || c >= len(items) {
		return nil
	}
	section, n := queueRowRef(items[c].id)
	if section != immQRowQueued {
		m.status.Show("Only tracks in Next in queue can be removed", statusTTLShort)
		return nil
	}
	cmd := m.removeQueuedAt(n)
	m.immQueueViewSkipHeader(1)
	return cmd
}

// immQueueViewMove is Shift+Up/Down: reorder a queued row within the queue.
func (m *Model) immQueueViewMove(dir int) tea.Cmd {
	items := m.canvasItems()
	c := m.immersive.cursor
	if c < 0 || c >= len(items) {
		return nil
	}
	section, n := queueRowRef(items[c].id)
	if section != immQRowQueued || !m.playlist.MoveQueue(n, n+dir) {
		return nil
	}
	m.immersive.cursor += dir // queued rows are contiguous under one label
	m.clampCanvasScroll()
	return m.rearmPreload()
}
