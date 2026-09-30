package model

// immersive_suggest.go is the search-as-you-type dropdown under the Search
// pill. While the pill's input is open, each edit schedules a search after a
// short pause in typing; the ranked results (see immersive_search.go) list
// in a box below the pill. Up/Down pick one, Enter or a click opens it, and
// Enter with nothing picked runs the full search as before.

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

const (
	immSuggestDelay   = 250 * time.Millisecond // pause in typing before a search
	immSuggestTimeout = 10 * time.Second
	immSuggestMax     = 8 // rows in the dropdown
	immSuggestPerType = 5 // results asked per type of a MultiSearcher
	immSuggestMinW    = 44
)

// immSuggestTickMsg fires after the typing pause; gen drops stale ticks.
type immSuggestTickMsg struct{ gen uint64 }

// immSuggestMsg carries dropdown results for the query they were asked for.
type immSuggestMsg struct {
	tracks       []playlist.Track
	providerName string
	gen          uint64
	err          error
}

// immersiveSuggestChanged schedules suggestions for the edited query and
// clears the pick, since the list is about to change.
func (m *Model) immersiveSuggestChanged() tea.Cmd {
	gen := nextRequest(&m.requests.immersiveSuggest)
	m.immersive.suggestCursor = -1
	if strings.TrimSpace(m.immersive.searchQuery) == "" || m.immersive.prov == nil {
		m.immersive.suggest, m.immersive.suggestLoading = nil, false
		return nil
	}
	return tea.Tick(immSuggestDelay, func(time.Time) tea.Msg { return immSuggestTickMsg{gen: gen} })
}

// fetchImmersiveSuggestCmd searches for the dropdown: the ranked multi-type
// search when the provider has one, otherwise its track search.
func fetchImmersiveSuggestCmd(prov playlist.Provider, query string, gen uint64) tea.Cmd {
	multi, isMulti := prov.(provider.MultiSearcher)
	s, isSearcher := prov.(provider.Searcher)
	if !isMulti && !isSearcher {
		return nil
	}
	name := prov.Name()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), immSuggestTimeout)
		defer cancel()
		var tracks []playlist.Track
		var err error
		if isMulti {
			var res provider.SearchResults
			res, err = multi.SearchAll(ctx, query, immSuggestPerType)
			tracks = rankSearchResults(query, res)
		} else {
			tracks, err = s.SearchTracks(ctx, query, immSuggestMax)
		}
		if len(tracks) > immSuggestMax {
			tracks = tracks[:immSuggestMax]
		}
		return immSuggestMsg{tracks: tracks, providerName: name, gen: gen, err: err}
	}
}

// handleImmersiveSuggestMsg applies the typing-pause tick and results.
func (m *Model) handleImmersiveSuggestMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case immSuggestTickMsg:
		if msg.gen != m.requests.immersiveSuggest || !m.immersive.searching || m.immersive.prov == nil {
			return nil, true
		}
		m.immersive.suggestLoading = true
		return fetchImmersiveSuggestCmd(m.immersive.prov, strings.TrimSpace(m.immersive.searchQuery), msg.gen), true
	case immSuggestMsg:
		if msg.gen != m.requests.immersiveSuggest || !m.immersive.searching ||
			m.immersive.prov == nil || m.immersive.prov.Name() != msg.providerName {
			return nil, true
		}
		m.immersive.suggestLoading = false
		if msg.err != nil {
			m.immersive.suggest = nil // the full search on Enter reports errors
			return nil, true
		}
		m.immersive.suggest = msg.tracks
		m.immersive.suggestCursor = min(m.immersive.suggestCursor, len(msg.tracks)-1)
		return nil, true
	}
	return nil, false
}

// closeImmersiveSuggest hides the dropdown and drops in-flight results.
func (m *Model) closeImmersiveSuggest() {
	nextRequest(&m.requests.immersiveSuggest)
	m.immersive.suggest, m.immersive.suggestLoading, m.immersive.suggestCursor = nil, false, -1
}

// openImmersiveSuggestion closes the input and shows the dropdown's list as
// the search results, then opens (collections) or plays (songs) the picked
// row, so Back returns to the list and a song plays in its context.
func (m *Model) openImmersiveSuggestion(idx int) tea.Cmd {
	list := m.immersive.suggest
	if idx < 0 || idx >= len(list) {
		return nil
	}
	m.immersive.searching = false
	m.closeImmersiveSuggest()
	m.beginImmersiveSearchView()
	m.immersive.tracks = list
	m.immersive.cursor = idx
	m.clampCanvasScroll()
	return m.activateItem(trackItems(list)[idx])
}

// immSuggestGeom is the dropdown box in content coordinates, below the
// Search pill; ok is false when no dropdown shows.
func (m Model) immSuggestGeom() (immRect, bool) {
	im := m.immersive
	if !im.searching || (len(im.suggest) == 0 && !im.suggestLoading) {
		return immRect{}, false
	}
	g := m.immGeom()
	var pill immRect
	for _, p := range m.immNavGeom(g.w) {
		if p.section == immSecSearch {
			pill = p.box
		}
	}
	rows := max(1, len(im.suggest))
	w := min(g.w, max(pill.W, immSuggestMinW))
	x := clampInt(pill.X, 0, g.w-w)
	y := g.navY + immNavRows
	h := min(rows+2, g.seekY-y) // stop above the progress bar
	if h < 3 {
		return immRect{}, false
	}
	return immRect{X: x, Y: y, W: w, H: h}, true
}

// suggestLabel is a dropdown row: the name, then what kind of result it is.
func suggestLabel(t playlist.Track) (string, string) {
	switch t.ProviderMeta[playlist.MetaKind] {
	case immMetaKindArtist:
		return t.Title, "Artist"
	case immMetaKindPlaylist:
		return t.Title, firstNonEmpty(t.ProviderMeta[immMetaSub], "Playlist")
	case playlist.MetaKindAlbum:
		return t.Title, "Album · " + firstNonEmpty(t.Artist, "Various")
	}
	if t.Artist == "" {
		return firstNonEmpty(t.Title, trackViewName(t)), "Song"
	}
	return firstNonEmpty(t.Title, trackViewName(t)), "Song · " + t.Artist
}

// overlayImmSuggest draws the dropdown over the composed frame lines.
func (m Model) overlayImmSuggest(lines []string) []string {
	r, ok := m.immSuggestGeom()
	if !ok {
		return lines
	}
	inner := r.W - 2
	box := make([]string, 0, r.H)
	box = append(box, boxTop("Suggestions", r.W, true))
	for i := 0; i < r.H-2; i++ {
		var row string
		switch {
		case i < len(m.immersive.suggest):
			// The name on the left, the kind right-aligned; the kind gets at
			// most half the row so long artist lists never hide the name.
			title, kind := suggestLabel(m.immersive.suggest[i])
			kind = ansi.Truncate(kind, inner/2, "…")
			avail := max(1, inner-1-2-ansi.StringWidth(kind)-2) // lead space, "> ", gap
			label := playlistItemStyle.Render("  " + ansi.Truncate(title, avail, "…"))
			if i == m.immersive.suggestCursor {
				label = playlistSelectedStyle.Render("> " + ansi.Truncate(title, avail, "…"))
			}
			gap := max(1, inner-1-ansi.StringWidth(label)-ansi.StringWidth(kind))
			row = label + strings.Repeat(" ", gap) + dimStyle.Render(kind)
		case m.immersive.suggestLoading:
			row = dimStyle.Render(m.immSpin() + " Searching…")
		}
		box = append(box, boxSide(true)+fitCell(" "+row, inner)+boxSide(true))
	}
	box = append(box, boxBottom(r.W, true))
	for i, b := range box {
		y := r.Y + i
		if y < 0 || y >= len(lines) {
			continue
		}
		base := lines[y]
		lines[y] = ansi.Truncate(base, r.X, "") + b + ansi.TruncateLeft(base, r.X+r.W, "")
	}
	return lines
}

// immSuggestCovers reports whether the dropdown hides rect, so Sixel covers
// (drawn over the text) are held back while it is open.
func (m Model) immSuggestCovers(rect immRect) bool {
	r, ok := m.immSuggestGeom()
	return ok && rect.X < r.X+r.W && r.X < rect.X+rect.W && rect.Y < r.Y+r.H && r.Y < rect.Y+rect.H
}
