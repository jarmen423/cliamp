package model

// immersive_resume.go saves the immersive page into the session checkpoint
// (see session.go) and reopens it on the next launch. The page reopens once
// the provider has answered its first playlist fetch, so a provider that is
// still signing in (Spotify) gets the page back after sign-in rather than an
// error. A page from another provider, an unknown name, or a collection that
// no longer loads falls back to the section root.

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

// Stable names for the saved page, indexed by the matching constants.
var (
	immViewNames = []string{"browse", "playlist", "album", "artist", "show", "search", "settings", "queue", "radio"}
	immKindNames = []string{"playlist", "album", "artist", "show", "track"}
)

// immSearchCtxPrefix starts the search view's context name (runImmersiveSearch).
const immSearchCtxPrefix = "Search: "

// immRestorePos is a saved cursor and scroll waiting for its list.
type immRestorePos struct{ cursor, scroll int }

func nameAt(names []string, i int) string {
	if i < 0 || i >= len(names) {
		return ""
	}
	return names[i]
}

func indexOfName(names []string, name string) (int, bool) {
	for i, n := range names {
		if n == name {
			return i, true
		}
	}
	return 0, false
}

func immSectionByName(name string) (immSection, bool) {
	for i, label := range immSectionLabels {
		if strings.EqualFold(label, name) {
			return immSection(i), true
		}
	}
	return 0, false
}

// immersiveResumeView is the page to save, or nil outside immersive. The
// settings tab and open inputs are transient: the page under them is saved.
func (m Model) immersiveResumeView() *resume.View {
	im := m.immersive
	if !im.active || im.prov == nil || im.section < 0 || im.section >= immSecCount {
		return nil
	}
	view := im.view
	if view == immViewSettings {
		view = im.settingsReturn
	}
	v := &resume.View{
		Provider: im.prov.Name(),
		Section:  strings.ToLower(immSectionLabels[im.section]),
		View:     nameAt(immViewNames, int(view)),
		Cursor:   im.cursor,
		Scroll:   im.scroll,
	}
	switch {
	case view == immViewSearch:
		v.Query = strings.TrimPrefix(im.ctxName, immSearchCtxPrefix)
	case view.isTrackView():
		v.Kind, v.ID, v.Name, v.Sub = nameAt(immKindNames, int(im.ctxKind)), im.ctxID, im.ctxName, im.ctxSub
	}
	return v
}

// SetImmersiveRestore reopens the saved immersive page on start.
func (m *Model) SetImmersiveRestore(v *resume.View) {
	if v == nil {
		return
	}
	m.immRestore = v
	m.openImmersiveOnce = true
}

// immersiveRestoreListsLoaded runs the pending restore after the first
// playlist fetch: now once it succeeded, later when it asked for sign-in
// (startImmersive runs again after it), and never after any other error.
func (m *Model) immersiveRestoreListsLoaded(err error) tea.Cmd {
	switch {
	case m.immRestore == nil:
		return nil
	case errors.Is(err, playlist.ErrNeedsAuth):
		return nil
	case err != nil:
		m.immRestore = nil
		return nil
	}
	return m.applyImmersiveRestore()
}

// applyImmersiveRestore reopens the saved page on the immersive provider.
func (m *Model) applyImmersiveRestore() tea.Cmd {
	r := m.immRestore
	m.immRestore = nil
	if r == nil || m.immersive.prov == nil || r.Provider != m.immersive.prov.Name() {
		return nil
	}
	sec, ok := immSectionByName(r.Section)
	if !ok {
		return nil
	}
	m.immersive.section = sec
	m.immersiveResetRoot()
	vi, ok := indexOfName(immViewNames, r.View)
	if !ok {
		return nil // unknown or newer view name: stay at the section root
	}
	pos := &immRestorePos{cursor: max(0, r.Cursor), scroll: max(0, r.Scroll)}
	switch view := immersiveView(vi); {
	case view == immViewQueue:
		m.openImmersiveQueueView()
		m.immRestorePos = pos
		m.applyImmersiveRestorePos()
		m.immRestorePos = nil // the live queue needs no asynchronous list fetch
		m.immQueueViewSkipHeader(1)
	case view == immViewSearch:
		if r.Query == "" {
			return nil
		}
		m.immersive.searchQuery = r.Query
		cmd := m.runImmersiveSearch()
		m.immRestorePos = pos
		return cmd
	case view.isTrackView():
		kind, ok := indexOfName(immKindNames, r.Kind)
		if !ok || r.ID == "" {
			return nil
		}
		cmd := m.openImmersiveItem(immItem{kind: immItemKind(kind), id: r.ID, title: r.Name, sub: r.Sub})
		m.immRestorePos = pos
		return cmd
	case view == immViewBrowse:
		m.immRestorePos = pos
		m.applyImmersiveRestorePos()
	}
	return nil
}

// applyImmersiveRestorePos puts the saved cursor and scroll back once the
// restored page lists its items.
func (m *Model) applyImmersiveRestorePos() {
	p := m.immRestorePos
	n := len(m.canvasItems())
	if p == nil || n == 0 {
		return
	}
	m.immRestorePos = nil
	m.immersive.cursor = min(p.cursor, n-1)
	m.immersive.scroll = min(p.scroll, m.immersive.cursor)
	m.clampCanvasScroll()
}

// immersiveRestoreFailed falls back to the section root when the restored
// collection no longer loads.
func (m *Model) immersiveRestoreFailed() {
	if m.immRestorePos == nil {
		return
	}
	m.immRestorePos = nil
	m.immersiveResetRoot()
}
