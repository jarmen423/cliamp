package model

// immersive_browse.go brings a provider.CategoryBrowser's Browse page
// (Spotify's Made for you, New releases, genres and moods) into the
// immersive canvas. The Playlists section lists a Browse row first; it opens
// the category list, and a category opens as a track view whose playlists,
// albums and artists ride as search placeholders, so they open like search
// results do.

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// immBrowseID is the Browse row's id when the provider names no entry.
const immBrowseID = "browse"

// immersiveCategoriesMsg carries a CategoryBrowser's category list.
type immersiveCategoriesMsg struct {
	cats         []provider.CategoryInfo
	providerName string
	gen          uint64
	err          error
}

func fetchImmersiveCategoriesCmd(b provider.CategoryBrowser, providerName string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cats, err := b.BrowseCategories(ctx)
		return immersiveCategoriesMsg{cats: cats, providerName: providerName, gen: gen, err: err}
	}
}

// fetchImmersiveCategoryCmd loads one category's page as an
// immersiveContentMsg, so it lands like any opened collection.
func fetchImmersiveCategoryCmd(b provider.CategoryBrowser, providerName string, cat provider.CategoryInfo, gen uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		res, err := b.CategoryContent(ctx, cat)
		return immersiveContentMsg{
			kind: immKindCategory, id: cat.ID, name: cat.Name, sub: cat.Group,
			tracks: categoryTracks(res), providerName: providerName, gen: gen, err: err,
		}
	}
}

// categoryTracks lists a category page richest kind first, the order the
// classic Browse page picks its opening tab in (preferredBrowseTab):
// playlists, albums, tracks, then artists.
func categoryTracks(res provider.SearchResults) []playlist.Track {
	out := make([]playlist.Track, 0, len(res.Playlists)+len(res.Albums)+len(res.Tracks)+len(res.Artists))
	for _, p := range res.Playlists {
		out = append(out, playlistPlaceholder(p))
	}
	for _, a := range res.Albums {
		out = append(out, albumPlaceholder(a))
	}
	for _, t := range res.Tracks {
		if !isSearchPlaceholder(t) {
			out = append(out, t)
		}
	}
	for _, a := range res.Artists {
		out = append(out, artistPlaceholder(a))
	}
	return out
}

// immBrowseItem is the Playlists section's Browse row, named after the
// provider's Browse entry; ok is false when the provider has no Browse page.
func (m Model) immBrowseItem() (immItem, bool) {
	if _, ok := m.immersive.prov.(provider.CategoryBrowser); !ok {
		return immItem{}, false
	}
	entry, _ := providerBrowseEntryForMode(m.immersive.prov, provider.BrowseCategories)
	return immItem{
		kind:  immKindBrowse,
		id:    firstNonEmpty(entry.ID, immBrowseID),
		title: firstNonEmpty(entry.Name, "Browse"),
		sub:   firstNonEmpty(entry.Section, "Discover"),
	}, true
}

// categoryItems lists the Browse categories, each labeled with its group.
func (m Model) categoryItems() []immItem {
	items := make([]immItem, 0, len(m.immersive.categories))
	for _, c := range m.immersive.categories {
		items = append(items, immItem{kind: immKindCategory, id: c.ID, title: c.Name, sub: c.Group})
	}
	return items
}

// immersiveCategory returns the fetched category for item, or one rebuilt
// from the item when the list is not in yet (a resumed page).
func (m Model) immersiveCategory(item immItem) provider.CategoryInfo {
	for _, c := range m.immersive.categories {
		if c.ID == item.id {
			return c
		}
	}
	return provider.CategoryInfo{ID: item.id, Name: item.title, Group: item.sub}
}

// showImmersiveCategories shows the category list, fetching it the first
// time. History is the caller's job.
func (m *Model) showImmersiveCategories() tea.Cmd {
	im := &m.immersive
	im.view = immViewCategories
	im.tracksLoading = false
	b, ok := im.prov.(provider.CategoryBrowser)
	if !ok || im.categories != nil || im.loadingCategories {
		return nil
	}
	im.loadingCategories = true
	return fetchImmersiveCategoriesCmd(b, im.prov.Name(), nextRequest(&m.requests.immersiveBrowse))
}
