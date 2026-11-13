package model

import (
	"context"
	"slices"
	"testing"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// linkTestProvider adds share links, multi-type search, an account,
// playlist/artist following and multi-track playlists for sorting tests.
type linkTestProvider struct {
	libraryTestProvider
	switched          bool
	followed          []bool
	followedPlaylists []string
	unfollowed        []string
	playlists         []playlist.PlaylistInfo
	tracksByPlaylist  map[string][]playlist.Track
}

func (p *linkTestProvider) Playlists() ([]playlist.PlaylistInfo, error) {
	if p.playlists != nil {
		return p.playlists, nil
	}
	return p.libraryTestProvider.Playlists()
}

func (p *linkTestProvider) Tracks(id string) ([]playlist.Track, error) {
	if tracks, ok := p.tracksByPlaylist[id]; ok {
		return tracks, nil
	}
	return p.libraryTestProvider.Tracks(id)
}

func (p *linkTestProvider) SearchAll(context.Context, string, int) (provider.SearchResults, error) {
	return provider.SearchResults{
		Tracks:    []playlist.Track{{Path: "test:hit", Title: "Hit"}},
		Artists:   []provider.ArtistInfo{{ID: "artist", Name: "Artist", ImageURL: "https://img/artist"}},
		Albums:    []provider.AlbumInfo{{ID: "album", Name: "Album", ImageURL: "https://img/album"}},
		Playlists: []playlist.PlaylistInfo{{ID: "mix", Name: "Mix", TrackCount: 3, Owned: true, ImageURL: "https://img/mix"}},
	}, nil
}

func (p *linkTestProvider) ResolveLink(_ context.Context, text string) (provider.Link, error) {
	if text == "track link" {
		track := playlist.Track{Path: "test:hit", Title: "Hit"}
		return provider.Link{Kind: provider.LinkTrack, ID: "hit", Name: "Hit", Track: &track}, nil
	}
	return provider.Link{Kind: provider.LinkPlaylist, ID: "mix", Name: "Mix"}, nil
}

func (p *linkTestProvider) Account(context.Context) (provider.Account, error) {
	name := "Old User"
	if p.switched {
		name = "New User"
	}
	return provider.Account{ID: "user", Name: name, Detail: "premium"}, nil
}

func (p *linkTestProvider) SwitchAccount(ctx context.Context) (provider.Account, error) {
	p.switched = true
	return p.Account(ctx)
}

func (p *linkTestProvider) FollowArtist(context.Context, string) error {
	p.followed = append(p.followed, true)
	return nil
}

func (p *linkTestProvider) UnfollowArtist(context.Context, string) error {
	p.followed = append(p.followed, false)
	return nil
}

func (p *linkTestProvider) FollowPlaylistByID(_ context.Context, id string) error {
	p.followedPlaylists = append(p.followedPlaylists, id)
	return nil
}

func (p *linkTestProvider) UnfollowPlaylistByID(_ context.Context, id string) error {
	p.unfollowed = append(p.unfollowed, id)
	return nil
}

func TestHeadlessLinkAccountRequests(t *testing.T) {
	fake := &linkTestProvider{
		libraryTestProvider: libraryTestProvider{commandsTestProvider{name: "Test"}},
		playlists: []playlist.PlaylistInfo{
			{ID: "owned", Name: "Owned", Owned: true, Section: "Your playlists"},
			{ID: "shared", Name: "Shared", Owned: false, Section: "Followed playlists"},
		},
	}
	providers := []provider.Entry{{Key: "test", Name: "Test", Provider: fake}}
	m := newHeadlessModel(t, &headlessEngine{}, providers)

	for _, tc := range []struct {
		op     string
		params ipc.Request
		ok     func(ipc.Response) bool
	}{
		{"provider.list", ipc.Request{}, func(r ipc.Response) bool {
			return len(r.Providers) == 1 && r.Providers[0].Links && r.Providers[0].Account && r.Providers[0].FollowPlaylists
		}},
		{"provider.playlists", ipc.Request{Provider: "test"}, func(r ipc.Response) bool {
			return len(r.Playlists) == 2 && r.Playlists[0].Owned && !r.Playlists[1].Owned
		}},
		{"provider.search", ipc.Request{Provider: "test", Query: "hit"}, func(r ipc.Response) bool {
			return len(r.Tracks) == 1 && len(r.Artists) == 1 && r.Artists[0].ImageURL == "https://img/artist" &&
				len(r.Albums) == 1 && len(r.Playlists) == 1 && r.Playlists[0].ImageURL == "https://img/mix" && r.Playlists[0].Owned
		}},
		{"provider.open_link", ipc.Request{Provider: "test", Path: "playlist link"}, func(r ipc.Response) bool {
			return r.Link != nil && r.Link.Kind == provider.LinkPlaylist && r.Link.ID == "mix" && r.Link.Track == nil
		}},
		{"provider.open_link", ipc.Request{Provider: "test", Path: "track link"}, func(r ipc.Response) bool {
			return r.Link != nil && r.Link.Kind == provider.LinkTrack && r.Link.Track != nil && r.Link.Track.Path == "test:hit"
		}},
		{"provider.account", ipc.Request{Provider: "test"}, func(r ipc.Response) bool {
			return r.Account != nil && r.Account.Name == "Old User"
		}},
		{"provider.switch_account", ipc.Request{Provider: "test"}, func(r ipc.Response) bool {
			return r.Account != nil && r.Account.Name == "New User"
		}},
		{"provider.follow_artist", ipc.Request{Provider: "test", Artist: "artist", Follow: true}, func(r ipc.Response) bool {
			return r.OK
		}},
		{"provider.follow_artist", ipc.Request{Provider: "test", Artist: "artist"}, func(r ipc.Response) bool {
			return r.OK
		}},
		{"provider.follow_playlist", ipc.Request{Provider: "test", Playlist: "shared", Follow: true}, func(r ipc.Response) bool {
			return r.OK
		}},
		{"provider.follow_playlist", ipc.Request{Provider: "test", Playlist: "shared"}, func(r ipc.Response) bool {
			return r.OK
		}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			if response := runV2(t, &m, tc.op, tc.params); !response.OK || !tc.ok(response) {
				t.Fatalf("%s %+v = %+v", tc.op, tc.params, response)
			}
		})
	}
	if len(fake.followed) != 2 || !fake.followed[0] || fake.followed[1] {
		t.Errorf("followed artists = %v, want [true false]", fake.followed)
	}
	if !slices.Equal(fake.followedPlaylists, []string{"shared"}) {
		t.Errorf("followedPlaylists = %v, want [shared]", fake.followedPlaylists)
	}
	if !slices.Equal(fake.unfollowed, []string{"shared"}) {
		t.Errorf("unfollowed = %v, want [shared]", fake.unfollowed)
	}
	if resp := runV2(t, &m, "provider.follow_playlist", ipc.Request{Provider: "test", Follow: true}); resp.OK {
		t.Fatalf("missing playlist should fail, got %+v", resp)
	}
}

func TestHeadlessPlaylistSortRequests(t *testing.T) {
	fake := &linkTestProvider{
		libraryTestProvider: libraryTestProvider{commandsTestProvider{name: "Test"}},
		tracksByPlaylist: map[string][]playlist.Track{
			"pl-dated": {
				{Path: "test:1", Title: "Zebra", Artist: "Beta", Album: "B-Side", TrackNumber: 2, DurationSecs: 240, AddedAt: "2026-01-01T00:00:00Z"},
				{Path: "test:2", Title: "alpha", Artist: "Gamma", Album: "A-Side", TrackNumber: 2, DurationSecs: 0, AddedAt: ""},
				{Path: "test:3", Title: "Middle", Artist: "alpha", Album: "A-Side", TrackNumber: 1, DurationSecs: 120, AddedAt: "2026-03-01T00:00:00Z"},
				{Path: "test:4", Title: "Early", Artist: "alpha", Album: "Z-Side", TrackNumber: 1, DurationSecs: 180, AddedAt: "2026-02-01T00:00:00Z"},
			},
			"pl-undated": {
				{Path: "test:a", Title: "Song B", Artist: "Artist B"},
				{Path: "test:b", Title: "Song A", Artist: "Artist A"},
			},
			"spotify:album:al1": {
				{Path: "test:al1", Title: "Track 1", Artist: "Artist"},
				{Path: "test:al2", Title: "Track 2", Artist: "Artist"},
			},
		},
	}
	providers := []provider.Entry{{Key: "test", Name: "Test", Provider: fake}}
	m := newHeadlessModel(t, &headlessEngine{}, providers)

	sortIDs := func(sorts []ipc.SortInfo) []string {
		ids := make([]string, len(sorts))
		for i, s := range sorts {
			ids[i] = s.ID
		}
		return ids
	}
	paths := func(tracks []ipc.TrackInfo) []string {
		out := make([]string, len(tracks))
		for i, tr := range tracks {
			out[i] = tr.Path
		}
		return out
	}

	for _, tc := range []struct {
		name      string
		playlist  string
		sort      string
		wantPaths []string
		wantSorts []string
	}{
		{
			name:      "default",
			playlist:  "pl-dated",
			wantPaths: []string{"test:1", "test:2", "test:3", "test:4"},
			wantSorts: []string{"default", "recent", "title", "artist", "album", "duration"},
		},
		{
			name:      "recent",
			playlist:  "pl-dated",
			sort:      "recent",
			wantPaths: []string{"test:3", "test:4", "test:1", "test:2"},
		},
		{
			name:      "title",
			playlist:  "pl-dated",
			sort:      "title",
			wantPaths: []string{"test:2", "test:4", "test:3", "test:1"},
		},
		{
			name:      "artist",
			playlist:  "pl-dated",
			sort:      "artist",
			wantPaths: []string{"test:3", "test:4", "test:1", "test:2"},
		},
		{
			name:      "album",
			playlist:  "pl-dated",
			sort:      "album",
			wantPaths: []string{"test:3", "test:2", "test:1", "test:4"},
		},
		{
			name:      "duration",
			playlist:  "pl-dated",
			sort:      "duration",
			wantPaths: []string{"test:3", "test:4", "test:1", "test:2"},
		},
		{
			name:      "undated omits recent",
			playlist:  "pl-undated",
			sort:      "title",
			wantPaths: []string{"test:b", "test:a"},
			wantSorts: []string{"default", "title", "artist", "album", "duration"},
		},
		{
			name:      "saved album omits sorts",
			playlist:  "spotify:album:al1",
			wantPaths: []string{"test:al1", "test:al2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := runV2(t, &m, "provider.tracks", ipc.Request{Provider: "test", Playlist: tc.playlist, Sort: tc.sort})
			if !resp.OK {
				t.Fatalf("provider.tracks failed: %+v", resp)
			}
			if got := paths(resp.Tracks); !slices.Equal(got, tc.wantPaths) {
				t.Errorf("tracks = %v, want %v", got, tc.wantPaths)
			}
			if tc.wantSorts != nil {
				if got := sortIDs(resp.Sorts); !slices.Equal(got, tc.wantSorts) {
					t.Errorf("sorts = %v, want %v", got, tc.wantSorts)
				}
			} else if tc.playlist == "spotify:album:al1" && len(resp.Sorts) != 0 {
				t.Errorf("saved album sorts = %v, want empty", resp.Sorts)
			}
		})
	}

	if resp := runV2(t, &m, "provider.tracks", ipc.Request{Provider: "test", Playlist: "pl-dated", Sort: "bogus"}); resp.OK {
		t.Fatalf("unknown sort should fail, got %+v", resp)
	}

	loadResp := runV2(t, &m, "provider.load", ipc.Request{Provider: "test", Playlist: "pl-dated", Sort: "recent"})
	if !loadResp.OK {
		t.Fatalf("provider.load failed: %+v", loadResp)
	}
	wantLoaded := []string{"test:3", "test:4", "test:1", "test:2"}
	if got := paths(loadResp.Tracks); !slices.Equal(got, wantLoaded) {
		t.Errorf("loaded tracks = %v, want %v", got, wantLoaded)
	}
	var queuePaths []string
	for _, tr := range m.playlist.Tracks() {
		queuePaths = append(queuePaths, tr.Path)
	}
	if !slices.Equal(queuePaths, wantLoaded) {
		t.Errorf("model playlist paths = %v, want %v", queuePaths, wantLoaded)
	}
}
