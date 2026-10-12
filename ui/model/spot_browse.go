package model

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
	"github.com/bjarneo/cliamp/ui"
)

// The Browse page of a provider.CategoryBrowser lives inside the provider
// search overlay: its category list is one more screen in front of the
// multi-type result tabs, so a category opens with the same tabs, drill-downs,
// and artist screen as a search.

// spotCategoriesMsg carries a CategoryBrowser's category list.
type spotCategoriesMsg struct {
	cats         []provider.CategoryInfo
	err          error
	providerName string
	gen          uint64
}

// spotCategoryMsg carries one category's page.
type spotCategoryMsg struct {
	results      provider.SearchResults
	err          error
	providerName string
	gen          uint64
}

func fetchSpotCategoriesCmd(ctx context.Context, b provider.CategoryBrowser, providerName string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		cats, err := b.BrowseCategories(ctx)
		return spotCategoriesMsg{cats: cats, err: err, providerName: providerName, gen: gen}
	}
}

func fetchSpotCategoryCmd(ctx context.Context, b provider.CategoryBrowser, providerName string, cat provider.CategoryInfo, gen uint64) tea.Cmd {
	return func() tea.Msg {
		results, err := b.CategoryContent(ctx, cat)
		return spotCategoryMsg{results: results, err: err, providerName: providerName, gen: gen}
	}
}

// openCategoryBrowse opens the search overlay on prov's Browse page.
func (m *Model) openCategoryBrowse(prov playlist.Provider) tea.Cmd {
	if _, ok := prov.(provider.CategoryBrowser); !ok {
		return nil
	}
	m.resetSpotSearch(prov, false)
	return m.spotShowBrowse()
}

// spotShowBrowse switches the overlay to the category list, loading it on
// first use, and drops any search or category load still in flight. The list
// cursor survives trips into categories and searches.
func (m *Model) spotShowBrowse() tea.Cmd {
	b, ok := m.spotSearch.prov.(provider.CategoryBrowser)
	if !ok {
		return nil
	}
	m.cancelSpotRequest()
	gen := nextRequest(&m.requests.spotSearch)
	m.spotSearch.screen = spotSearchBrowse
	m.spotSearch.browsing = true
	m.spotSearch.category = provider.CategoryInfo{}
	m.spotSearch.drill = nil
	m.spotSearch.err = ""
	if m.spotSearch.categories != nil {
		m.spotSearch.loading = false
		return nil
	}
	m.spotSearch.loading = true
	return fetchSpotCategoriesCmd(m.newSpotRequestContext(30*time.Second), b, m.spotSearch.prov.Name(), gen)
}

func (m *Model) handleSpotCategoriesMsg(msg spotCategoriesMsg) {
	if !m.isCurrentSpotRequest(msg.gen, msg.providerName) || m.spotSearch.screen != spotSearchBrowse {
		return
	}
	m.cancelSpotRequest()
	m.spotSearch.loading = false
	if msg.err != nil {
		m.spotSearch.err = "Browse failed: " + msg.err.Error()
		return
	}
	m.spotSearch.categories = msg.cats
	if len(msg.cats) == 0 {
		m.spotSearch.err = "Nothing to browse"
	}
	m.applyHeightMode()
	m.clampActiveScrollState()
}

// handleSpotCategoryMsg shows a category's page. Every way off the category
// load (Esc, /, a new category) bumps the request generation, so a current
// message always belongs to the open category.
func (m *Model) handleSpotCategoryMsg(msg spotCategoryMsg) {
	if !m.isCurrentSpotRequest(msg.gen, msg.providerName) {
		return
	}
	m.cancelSpotRequest()
	m.spotSearch.loading = false
	if msg.err != nil {
		m.spotSearch.err = "Load failed: " + msg.err.Error()
		m.spotSearch.category = provider.CategoryInfo{}
		return
	}
	m.showSpotResultsAll(msg.results, preferredBrowseTab(msg.results), "Nothing found for "+m.spotSearch.category.Name)
}

// showSpotResultsAll puts multi-type results on the tabbed results screen,
// opening on tab; emptyMsg reports a result set with nothing in it.
func (m *Model) showSpotResultsAll(results provider.SearchResults, tab spotSearchTab, emptyMsg string) {
	s := &m.spotSearch
	s.multi = true
	s.resultsAll = results
	s.screen = spotSearchResults
	s.tab = tab
	s.drill = nil
	s.cursor = 0
	s.scroll = 0
	if s.resultsAllCount() == 0 {
		s.err = emptyMsg
	}
	m.applyHeightMode()
	m.clampActiveScrollState()
}

// preferredBrowseTab opens a category on its richest kind of content:
// playlists for genres and moods, albums for new releases, tracks otherwise.
func preferredBrowseTab(r provider.SearchResults) spotSearchTab {
	switch {
	case len(r.Playlists) > 0:
		return spotTabPlaylists
	case len(r.Albums) > 0:
		return spotTabAlbums
	case len(r.Tracks) > 0:
		return spotTabTracks
	case len(r.Artists) > 0:
		return spotTabArtists
	}
	return spotTabTracks
}

// handleSpotBrowseKey handles keys on the category list.
func (m *Model) handleSpotBrowseKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &m.spotSearch
	count := len(s.categories)
	visible := m.spotSearchResultsVisible()
	switch msg.String() {
	case "ctrl+x":
		m.toggleExpandedView()
	case "up", "k", "ctrl+p":
		if s.catCursor > 0 {
			s.catCursor--
		} else if count > 0 {
			s.catCursor = count - 1
		}
	case "down", "j", "ctrl+n":
		if s.catCursor < count-1 {
			s.catCursor++
		} else {
			s.catCursor = 0
		}
	case "pgup", "ctrl+u":
		s.catCursor = max(0, s.catCursor-max(1, visible))
	case "pgdown", "ctrl+d":
		s.catCursor = min(max(0, count-1), s.catCursor+max(1, visible))
	case "home", "g":
		s.catCursor = 0
	case "end", "G":
		s.catCursor = max(0, count-1)
	case "enter", "l", "right":
		if count > 0 && !s.loading {
			return m.spotOpenCategory(s.categories[s.catCursor])
		}
	case "/":
		// Drop a category load in flight so it cannot land on the input.
		m.cancelSpotRequest()
		nextRequest(&m.requests.spotSearch)
		s.loading = false
		s.category = provider.CategoryInfo{}
		s.screen = spotSearchInput
		s.err = ""
	case "esc", "backspace":
		m.closeSpotSearch()
		return nil
	}
	m.spotBrowseMaybeAdjustScroll(m.spotSearchResultsVisible())
	return nil
}

func (m *Model) spotOpenCategory(cat provider.CategoryInfo) tea.Cmd {
	b, ok := m.spotSearch.prov.(provider.CategoryBrowser)
	if !ok {
		return nil
	}
	m.spotSearch.category = cat
	m.spotSearch.loading = true
	m.spotSearch.err = ""
	return fetchSpotCategoryCmd(m.newSpotRequestContext(time.Minute), b, m.spotSearch.prov.Name(), cat, nextRequest(&m.requests.spotSearch))
}

// spotBrowseMaybeAdjustScroll keeps the category cursor visible, counting the
// group separator rows the list renders between categories.
func (m *Model) spotBrowseMaybeAdjustScroll(visible int) {
	s := &m.spotSearch
	clampScroll(&s.catCursor, &s.catScroll, len(s.categories), visible)
	for s.catScroll < s.catCursor && sectionRowsToCursor(s.categories, s.catScroll, s.catCursor, categoryGroup) > visible {
		s.catScroll++
	}
}

func categoryGroup(c provider.CategoryInfo) string { return c.Group }

func (m Model) renderSpotBrowseBody(budget int) string {
	s := m.spotSearch
	switch {
	case s.loading:
		what := "Browse"
		if s.category.ID != "" {
			what = s.category.Name
		}
		return bodyLines([]string{loadingLine("Loading " + what + "…")}, budget)
	case len(s.categories) == 0:
		return bodyMessage("Nothing to browse", budget)
	}
	lines := make([]string, 0, budget)
	for row := range sectionRows(s.categories, s.catScroll, categoryGroup) {
		if len(lines) >= budget {
			break
		}
		if row.Index < 0 {
			if budget > 1 {
				lines = append(lines, dimStyle.Render(labeledSeparator("", row.Section, m.layout.panelWidth)))
			}
			continue
		}
		label := truncate(s.categories[row.Index].Name, ui.PanelWidth-8)
		lines = append(lines, cursorLine(label, row.Index == s.catCursor))
	}
	return strings.Join(padLines(lines, budget, len(lines)), "\n")
}

// spotBrowseCrumb names the open Browse location for the overlay headers.
func (m Model) spotBrowseCrumb() string {
	crumb := "Browse: " + providerName(m.spotSearch.prov)
	if m.spotSearch.category.ID != "" {
		crumb += " / " + m.spotSearch.category.Name
	}
	return crumb
}
