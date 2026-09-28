package model

// immersive.go implements the v1 immersive frame: a full-width visualizer
// band, a nav-pill row, a two-column body (Now Playing + Queue on the left,
// the section canvas on the right), a centered controls row, and an
// eighth-block progress bar — over the same provider data and player the
// normal TUI already drives. No new data plumbing: the canvas reads the
// provider's existing playlist/artist/album/show/search surfaces.
//
// The mode stays opt-in: `I` toggles it (or `immersive = true` in config),
// every key and renderer lives behind m.immersiveShown(), and the classic
// layout takes over whenever the terminal is too small for the wireframe.
// Cover art is still the shade-glyph placeholder (immArtBlock); every box
// art occupies is a fixed cell-aligned rect exposed via ImmersiveArtRects()
// so a later image layer can blit into the same rectangles.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// immersivePane identifies which region of the frame holds keyboard focus.
type immersivePane int

const (
	immPaneNav    immersivePane = iota // nav pill row
	immPaneCanvas                      // section canvas (right column)
	immPaneQueue                       // queue panel (left column)
	immPaneCount
)

// immSection is a nav-pill destination — what the canvas browses.
type immSection int

const (
	immSecPlaylists immSection = iota
	immSecArtists
	immSecSearch
	immSecAlbums
	immSecPodcasts
	immSecCount
)

// immSectionOrder is the visual pill order: two pills left of the wide
// center search pill, two pills right of it.
var immSectionOrder = [immSecCount]immSection{
	immSecPlaylists, immSecArtists, immSecSearch, immSecAlbums, immSecPodcasts,
}

var immSectionLabels = [immSecCount]string{
	"Playlists", "Artists", "Search", "Albums", "Podcasts",
}

// immersiveView identifies what the canvas is showing.
type immersiveView int

const (
	immViewBrowse   immersiveView = iota // item list of the active section
	immViewPlaylist                      // opened playlist's tracks
	immViewAlbum                         // opened album's tracks
	immViewArtist                        // artist detail (popular tracks)
	immViewShow                          // podcast show episodes
	immViewSearch                        // search results
	immViewSettings                      // settings/EQ tab (key-only, no pill)
)

// isTrackView reports whether the canvas shows a track list (detail or
// search results) rather than a collection browse or the settings tab.
func (v immersiveView) isTrackView() bool {
	switch v {
	case immViewPlaylist, immViewAlbum, immViewArtist, immViewShow, immViewSearch:
		return true
	}
	return false
}

// immCanvasMode is the canvas presentation style cycled by `v`.
type immCanvasMode int

const (
	immCanvasList immCanvasMode = iota // one line per item
	immCanvasRows                      // 3-row blocks with an art box
	immCanvasGrid                      // art tiles with titles below
	immCanvasModeCount
)

var immCanvasModeNames = [immCanvasModeCount]string{"list", "rows", "grid"}

// immersiveTrackSort is the client-side track-list ordering.
type immersiveTrackSort int

const (
	immSortTrackOrder immersiveTrackSort = iota // provider order
	immSortTrackTitle
	immSortTrackAlbum
	immSortTrackDuration
	immSortTrackCount
)

var immSortTrackLabels = [immSortTrackCount]string{"#", "title", "album", "time"}

// immBrowseSort cycles the browse canvas's client-side ordering.
type immBrowseSort int

const (
	immBrowseSortRecents immBrowseSort = iota // provider order
	immBrowseSortAlpha                        // case-insensitive by name
	immBrowseSortCount
)

var immBrowseSortLabels = [immBrowseSortCount]string{"recents", "a-z"}

// immItemKind distinguishes what a canvas item opens into.
type immItemKind int

const (
	immKindPlaylist immItemKind = iota
	immKindAlbum
	immKindArtist
	immKindShow // podcast show
	immKindTrack
)

// immItem is one selectable canvas row: a collection in a browse view or a
// track in a detail/search view (id then holds its index in sortedTracks()).
type immItem struct {
	kind  immItemKind
	id    string
	title string
	sub   string // artist / playlist summary / show author
	sub2  string // album (tracks only)
	dur   int    // seconds; tracks only
	path  string // tracks only: used to mark the playing row
}

// Minimum terminal size for the immersive frame; below it the classic
// layout takes over automatically.
const (
	immMinWidth  = 80
	immMinHeight = 24
)

// immNavSnap is one entry on the canvas back stack.
type immNavSnap struct {
	view      immersiveView
	ctxID     string
	ctxName   string
	ctxSub    string
	ctxKind   immItemKind
	tracks    []playlist.Track
	trackSort immersiveTrackSort
	cursor    int
	scroll    int
}

// immersiveState holds every piece of the immersive mode. It is the one
// state block the mode touches, so leaving immersive never disturbs normal
// state.
type immersiveState struct {
	active bool
	prov   playlist.Provider // provider the canvas is browsing
	focus  immersivePane

	// — nav pills —
	section immSection

	// — section data —
	lists          []playlist.PlaylistInfo
	albums         []provider.AlbumInfo
	artists        []provider.ArtistInfo
	loadingLists   bool
	loadingAlbums  bool
	loadingArtists bool

	// — canvas —
	view      immersiveView
	mode      immCanvasMode
	ctxID     string // playlist/album/artist/show id the open view belongs to
	ctxName   string
	ctxSub    string
	ctxKind   immItemKind
	cursor    int // item index into canvasItems()/sortedTracks()
	scroll    int // first visible item (list/rows) or tile row (grid)
	filtering bool
	filter    string
	sort      immBrowseSort

	tracks        []playlist.Track
	tracksLoading bool
	trackSort     immersiveTrackSort
	back          []immNavSnap

	settingsReturn immersiveView // view `e` returns to
	settingsCursor int           // row inside the settings tab

	// — queue panel —
	queueCursor int
	queueScroll int

	// — search pill input —
	searching   bool
	searchQuery string

	searchLoading bool

	// spin advances on every tick while any immersive fetch is in flight,
	// driving the loading indicators.
	spin int
}

// immSpinFrames is the braille spinner cycled by loading indicators.
var immSpinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// immSpin returns the current spinner frame for a loading indicator.
func (m Model) immSpin() string {
	return immSpinFrames[m.immersive.spin%len(immSpinFrames)]
}

// immLoadingActive reports whether any immersive fetch is in flight.
func (m Model) immLoadingActive() bool {
	im := m.immersive
	return im.loadingLists || im.loadingAlbums || im.loadingArtists ||
		im.tracksLoading || im.searchLoading
}

// — messages —

type immersiveListsMsg struct {
	lists        []playlist.PlaylistInfo
	providerName string
	gen          uint64
	err          error
}

type immersiveAlbumsMsg struct {
	albums       []provider.AlbumInfo
	providerName string
	gen          uint64
	err          error
}

type immersiveArtistsMsg struct {
	artists      []provider.ArtistInfo
	providerName string
	gen          uint64
	err          error
}

// immersiveContentMsg carries the track list for an opened playlist, album,
// or podcast show.
type immersiveContentMsg struct {
	kind         immItemKind
	id           string
	name         string
	sub          string
	tracks       []playlist.Track
	providerName string
	gen          uint64
	err          error
}

// immersiveArtistMsg carries an artist profile for the artist view.
type immersiveArtistMsg struct {
	detail       provider.ArtistDetail
	providerName string
	gen          uint64
	err          error
}

// immersiveSearchMsg carries global-search results into the canvas.
type immersiveSearchMsg struct {
	tracks       []playlist.Track
	providerName string
	gen          uint64
	err          error
}

// openImmersiveMsg asks Update to enter immersive mode once the program is
// running (Init has a value receiver and cannot mutate the model).
type openImmersiveMsg struct{}

// — command constructors —

func fetchImmersiveListsCmd(prov playlist.Provider, gen uint64) tea.Cmd {
	return func() tea.Msg {
		lists, err := prov.Playlists()
		return immersiveListsMsg{lists: lists, providerName: prov.Name(), gen: gen, err: err}
	}
}

func fetchImmersiveAlbumsCmd(b provider.AlbumBrowser, providerName, sortType string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		albums, err := b.AlbumList(sortType, 0, navAlbumPageSize)
		return immersiveAlbumsMsg{albums: albums, providerName: providerName, gen: gen, err: err}
	}
}

func fetchImmersiveArtistsCmd(ab provider.ArtistBrowser, providerName string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		artists, err := ab.Artists()
		return immersiveArtistsMsg{artists: artists, providerName: providerName, gen: gen, err: err}
	}
}

func fetchImmersiveContentCmd(prov playlist.Provider, kind immItemKind, id, name, sub string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		tracks, err := prov.Tracks(id)
		return immersiveContentMsg{kind: kind, id: id, name: name, sub: sub, tracks: tracks, providerName: prov.Name(), gen: gen, err: err}
	}
}

func fetchImmersiveAlbumCmd(l provider.AlbumTrackLoader, providerName string, kind immItemKind, id, name, sub string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		tracks, err := l.AlbumTracks(id)
		return immersiveContentMsg{kind: kind, id: id, name: name, sub: sub, tracks: tracks, providerName: providerName, gen: gen, err: err}
	}
}

func fetchImmersiveArtistCmd(l provider.ArtistDetailLoader, providerName, id string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		detail, err := l.ArtistDetail(id)
		return immersiveArtistMsg{detail: detail, providerName: providerName, gen: gen, err: err}
	}
}

func fetchImmersiveSearchCmd(ctx context.Context, s provider.Searcher, providerName, query string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		tracks, err := s.SearchTracks(ctx, query, 50)
		return immersiveSearchMsg{tracks: tracks, providerName: providerName, gen: gen, err: err}
	}
}

// — open / close —

// immersiveFits reports whether the terminal can hold the immersive frame.
// The classic layout takes over below this size.
func (m Model) immersiveFits() bool {
	return m.width >= immMinWidth && m.height >= immMinHeight
}

// immersiveShown reports whether the immersive frame is the one on screen:
// the mode is active and the terminal is currently big enough.
func (m Model) immersiveShown() bool {
	return m.immersive.active && m.immersiveFits()
}

// toggleImmersive enters immersive when off, exits when on.
func (m *Model) toggleImmersive() tea.Cmd {
	if m.immersive.active {
		m.exitImmersive()
		return nil
	}
	return m.enterImmersive()
}

// enterImmersive switches to the immersive frame and kicks the section
// fetches. The provider's existing playlist cache seeds Playlists when it
// is already settled for this provider.
func (m *Model) enterImmersive() tea.Cmd {
	if !m.immersiveFits() {
		m.status.Showf(statusTTLDefault, "Immersive needs a %dx%d terminal", immMinWidth, immMinHeight)
		return nil
	}
	m.immersive = immersiveState{
		active:         true,
		prov:           m.provider,
		focus:          immPaneCanvas,
		section:        immSecPlaylists,
		view:           immViewBrowse,
		mode:           immCanvasList,
		sort:           immBrowseSortRecents,
		settingsReturn: immViewBrowse,
	}
	if m.provider == nil {
		return nil
	}
	if _, ok := m.provider.(provider.AlbumBrowser); ok {
		m.immersive.loadingAlbums = true
	}
	if _, ok := m.provider.(provider.ArtistBrowser); ok {
		m.immersive.loadingArtists = true
	}
	if len(m.providerLists) > 0 {
		// Reuse the already-fetched list; browse-route pseudo rows are
		// filtered out of the canvas.
		m.immersive.lists = m.filterImmersivePlaylists(m.providerLists)
	} else {
		m.immersive.loadingLists = true
	}
	return m.fetchImmersiveSidebar()
}

// exitImmersive leaves the mode and supersedes every in-flight fetch.
func (m *Model) exitImmersive() {
	nextRequest(&m.requests.immersiveLists)
	nextRequest(&m.requests.immersiveAlbums)
	nextRequest(&m.requests.immersiveArtists)
	nextRequest(&m.requests.immersiveContent)
	nextRequest(&m.requests.immersiveArtist)
	nextRequest(&m.requests.immersiveSearch)
	m.immersive = immersiveState{}
}

// fetchImmersiveSidebar dispatches the outstanding section fetches, one
// generation per section like the Home overlay.
func (m *Model) fetchImmersiveSidebar() tea.Cmd {
	prov := m.immersive.prov
	if prov == nil {
		return nil
	}
	providerName := prov.Name()
	var cmds []tea.Cmd
	if m.immersive.loadingLists {
		cmds = append(cmds, fetchImmersiveListsCmd(prov, nextRequest(&m.requests.immersiveLists)))
	}
	if ab, ok := prov.(provider.AlbumBrowser); ok && m.immersive.loadingAlbums {
		sortType := ab.DefaultAlbumSort()
		cmds = append(cmds, fetchImmersiveAlbumsCmd(ab, providerName, sortType, nextRequest(&m.requests.immersiveAlbums)))
	}
	if ab, ok := prov.(provider.ArtistBrowser); ok && m.immersive.loadingArtists {
		cmds = append(cmds, fetchImmersiveArtistsCmd(ab, providerName, nextRequest(&m.requests.immersiveArtists)))
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// isCurrentImmersiveRequest reports whether a completion still belongs to
// the mode's current provider session.
func (m Model) isCurrentImmersiveRequest(gen uint64, providerName string, cur uint64) bool {
	return m.immersive.active &&
		m.immersive.prov != nil &&
		m.immersive.prov.Name() == providerName &&
		gen == cur
}

// filterImmersivePlaylists drops UI-only browse-route pseudo entries; real
// provider rows — including synthetic library rows like "Liked Songs" —
// stay.
func (m Model) filterImmersivePlaylists(lists []playlist.PlaylistInfo) []playlist.PlaylistInfo {
	out := make([]playlist.PlaylistInfo, 0, len(lists))
	for _, l := range lists {
		if _, browse := providerBrowseEntryForID(m.immersive.prov, l.ID); browse {
			continue
		}
		out = append(out, l)
	}
	return out
}

// — canvas items —

// browseItems returns the item rows for the active browse section with
// filter and sort applied. Fetched data is never mutated.
func (m Model) browseItems() []immItem {
	im := m.immersive
	var items []immItem
	switch im.section {
	case immSecPlaylists:
		for _, l := range im.lists {
			items = append(items, immItem{
				kind: immKindPlaylist, id: l.ID, title: l.Name,
				sub: playlistRowSub(l),
			})
		}
	case immSecArtists:
		for _, a := range im.artists {
			items = append(items, immItem{
				kind: immKindArtist, id: a.ID, title: a.Name,
				sub: "Artist",
			})
		}
	case immSecAlbums:
		for _, a := range im.albums {
			items = append(items, immItem{
				kind: immKindAlbum, id: a.ID, title: a.Name,
				sub: firstNonEmpty(a.Artist, "Album"),
			})
		}
	case immSecPodcasts:
		if l, ok := im.prov.(provider.SubscriptionLister); ok {
			for _, s := range l.Subscriptions() {
				items = append(items, immItem{
					kind: immKindShow, id: s.ID, title: s.Name,
					sub: firstNonEmpty(s.Author, "Podcast"),
				})
			}
		}
	}
	if im.filter != "" {
		q := strings.ToLower(im.filter)
		kept := items[:0]
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.title), q) ||
				strings.Contains(strings.ToLower(it.sub), q) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	if im.sort == immBrowseSortAlpha {
		sort.SliceStable(items, func(i, j int) bool {
			return strings.ToLower(items[i].title) < strings.ToLower(items[j].title)
		})
	}
	return items
}

// trackItems wraps a track list as canvas items; id is the track's index in
// the list so activation maps back to the same sort order.
func trackItems(tracks []playlist.Track) []immItem {
	items := make([]immItem, 0, len(tracks))
	for i, t := range tracks {
		title := t.Title
		if title == "" {
			title = trackViewName(t)
		}
		items = append(items, immItem{
			kind:  immKindTrack,
			id:    strconv.Itoa(i),
			title: title,
			sub:   t.Artist,
			sub2:  t.Album,
			dur:   t.DurationSecs,
			path:  t.Path,
		})
	}
	return items
}

// canvasItems returns the items the canvas lists in its current view.
// Track views wrap sortedTracks(); the settings tab has no items.
func (m Model) canvasItems() []immItem {
	im := m.immersive
	if im.view == immViewSettings {
		return nil
	}
	if im.view.isTrackView() {
		return trackItems(m.sortedTracks())
	}
	return m.browseItems()
}

func playlistRowSub(l playlist.PlaylistInfo) string {
	sub := "Playlist"
	if l.TrackCount > 0 {
		sub += " · " + plural(l.TrackCount, "song")
	}
	return sub
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// — section / view switching —

// immRootView is the view a section lands on: the search pill opens the
// results view, everything else browses its collection list.
func immRootView(s immSection) immersiveView {
	if s == immSecSearch {
		return immViewSearch
	}
	return immViewBrowse
}

// immersiveSetSection switches the canvas to a nav-pill section, resetting
// drill-down state.
func (m *Model) immersiveSetSection(s immSection) {
	im := &m.immersive
	im.section = s
	im.back = nil
	im.cursor, im.scroll = 0, 0
	im.view = immRootView(s)
	if im.view == immViewBrowse {
		im.ctxID, im.ctxName, im.ctxSub = "", "", ""
		im.tracks = nil
		im.tracksLoading = false
	}
}

// immersiveHome returns the canvas to the section's root view.
func (m *Model) immersiveHome() {
	im := &m.immersive
	im.back = nil
	im.cursor, im.scroll = 0, 0
	im.view = immRootView(im.section)
	if im.view == immViewBrowse {
		im.ctxID, im.ctxName, im.ctxSub = "", "", ""
		im.tracks = nil
		im.tracksLoading = false
	}
}

// — navigation —

// openImmersiveItem opens a canvas item into its track view; tracks play
// directly instead of opening.
func (m *Model) openImmersiveItem(item immItem) tea.Cmd {
	prov := m.immersive.prov
	if prov == nil {
		return nil
	}
	m.pushImmersiveBack()
	providerName := prov.Name()
	gen := nextRequest(&m.requests.immersiveContent)
	m.immersive.tracks = nil
	m.immersive.tracksLoading = true
	m.immersive.cursor, m.immersive.scroll = 0, 0
	m.immersive.trackSort = immSortTrackOrder
	m.immersive.ctxID, m.immersive.ctxName, m.immersive.ctxSub = item.id, item.title, item.sub
	m.immersive.ctxKind = item.kind

	switch item.kind {
	case immKindPlaylist:
		m.immersive.view = immViewPlaylist
		return fetchImmersiveContentCmd(prov, item.kind, item.id, item.title, item.sub, gen)
	case immKindAlbum:
		m.immersive.view = immViewAlbum
		if l, ok := prov.(provider.AlbumTrackLoader); ok {
			return fetchImmersiveAlbumCmd(l, providerName, item.kind, item.id, item.title, item.sub, gen)
		}
	case immKindArtist:
		m.immersive.view = immViewArtist
		if l, ok := prov.(provider.ArtistDetailLoader); ok {
			return fetchImmersiveArtistCmd(l, providerName, item.id, nextRequest(&m.requests.immersiveArtist))
		}
	case immKindShow:
		m.immersive.view = immViewShow
		if l, ok := prov.(provider.AlbumTrackLoader); ok {
			return fetchImmersiveAlbumCmd(l, providerName, item.kind, item.id, item.title, item.sub, gen)
		}
		return fetchImmersiveContentCmd(prov, item.kind, item.id, item.title, item.sub, gen)
	}
	m.immersive.tracksLoading = false
	return nil
}

// pushImmersiveBack snapshots the current canvas context onto the back
// stack. Settings is transient and never pushed.
func (m *Model) pushImmersiveBack() {
	if m.immersive.view == immViewSettings {
		return
	}
	m.immersive.back = append(m.immersive.back, immNavSnap{
		view:      m.immersive.view,
		ctxID:     m.immersive.ctxID,
		ctxName:   m.immersive.ctxName,
		ctxSub:    m.immersive.ctxSub,
		ctxKind:   m.immersive.ctxKind,
		tracks:    m.immersive.tracks,
		trackSort: m.immersive.trackSort,
		cursor:    m.immersive.cursor,
		scroll:    m.immersive.scroll,
	})
}

// immersiveGoBack restores the last canvas context, or lands on the
// section root.
func (m *Model) immersiveGoBack() {
	n := len(m.immersive.back)
	if n == 0 {
		m.immersiveHome()
		return
	}
	snap := m.immersive.back[n-1]
	m.immersive.back = m.immersive.back[:n-1]
	m.immersive.view = snap.view
	m.immersive.ctxID, m.immersive.ctxName, m.immersive.ctxSub = snap.ctxID, snap.ctxName, snap.ctxSub
	m.immersive.ctxKind = snap.ctxKind
	m.immersive.tracks = snap.tracks
	m.immersive.tracksLoading = false
	m.immersive.trackSort = snap.trackSort
	m.immersive.cursor = snap.cursor
	m.immersive.scroll = snap.scroll
}

// sortedTracks returns the open context's tracks in the active list order.
func (m Model) sortedTracks() []playlist.Track {
	tracks := m.immersive.tracks
	if m.immersive.trackSort == immSortTrackOrder {
		return tracks
	}
	out := make([]playlist.Track, len(tracks))
	copy(out, tracks)
	switch m.immersive.trackSort {
	case immSortTrackTitle:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(trackViewName(out[i])) < strings.ToLower(trackViewName(out[j]))
		})
	case immSortTrackAlbum:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Album) < strings.ToLower(out[j].Album)
		})
	case immSortTrackDuration:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].DurationSecs < out[j].DurationSecs
		})
	}
	return out
}

// playImmersiveContext loads the open context's tracks into the queue and
// plays from index startIdx (in sorted order). Context is remembered so the
// queue panel can name what is playing next.
func (m *Model) playImmersiveContext(startIdx int) tea.Cmd {
	tracks := m.sortedTracks()
	if len(tracks) == 0 {
		return nil
	}
	m.replacePlayerPlaylist(tracks)
	if startIdx < 0 || startIdx >= m.playlist.Len() {
		startIdx = 0
	}
	m.plCursor = startIdx
	m.playlist.SetIndex(startIdx)
	if m.immersive.view == immViewPlaylist {
		m.activeProviderPlaylistID = m.immersive.ctxID
		m.loadedPlaylist = ""
	}
	cmd := m.playCurrentTrack()
	m.notifyPlayback()
	return cmd
}

// playImmersiveTrack plays the track at index in the open context.
func (m *Model) playImmersiveTrack(index int) tea.Cmd {
	tracks := m.sortedTracks()
	if index < 0 || index >= len(tracks) {
		return nil
	}
	m.replacePlayerPlaylist(tracks)
	m.plCursor = index
	m.playlist.SetIndex(index)
	if m.immersive.view == immViewPlaylist {
		m.activeProviderPlaylistID = m.immersive.ctxID
	}
	cmd := m.playCurrentTrack()
	m.notifyPlayback()
	return cmd
}

func wrapIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

// tickImmersive advances the loading spinner while fetches are in flight.
func (m *Model) tickImmersive() {
	if m.immersive.active && m.immLoadingActive() {
		m.immersive.spin++
	}
}
