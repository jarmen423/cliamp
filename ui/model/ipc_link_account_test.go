package model

import (
	"context"
	"testing"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// linkTestProvider adds share links, multi-type search, an account and
// artist following to the library test provider.
type linkTestProvider struct {
	libraryTestProvider
	switched bool
	followed []bool
}

func (p *linkTestProvider) SearchAll(context.Context, string, int) (provider.SearchResults, error) {
	return provider.SearchResults{
		Tracks:    []playlist.Track{{Path: "test:hit", Title: "Hit"}},
		Artists:   []provider.ArtistInfo{{ID: "artist", Name: "Artist", ImageURL: "https://img/artist"}},
		Albums:    []provider.AlbumInfo{{ID: "album", Name: "Album", ImageURL: "https://img/album"}},
		Playlists: []playlist.PlaylistInfo{{ID: "mix", Name: "Mix", TrackCount: 3, ImageURL: "https://img/mix"}},
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

func TestHeadlessLinkAccountRequests(t *testing.T) {
	fake := &linkTestProvider{libraryTestProvider: libraryTestProvider{commandsTestProvider{name: "Test"}}}
	providers := []provider.Entry{{Key: "test", Name: "Test", Provider: fake}}
	m := newHeadlessModel(t, &headlessEngine{}, providers)

	for _, tc := range []struct {
		op     string
		params ipc.Request
		ok     func(ipc.Response) bool
	}{
		{"provider.list", ipc.Request{}, func(r ipc.Response) bool {
			return len(r.Providers) == 1 && r.Providers[0].Links && r.Providers[0].Account
		}},
		{"provider.search", ipc.Request{Provider: "test", Query: "hit"}, func(r ipc.Response) bool {
			return len(r.Tracks) == 1 && len(r.Artists) == 1 && r.Artists[0].ImageURL == "https://img/artist" &&
				len(r.Albums) == 1 && len(r.Playlists) == 1 && r.Playlists[0].ImageURL == "https://img/mix"
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
	} {
		t.Run(tc.op, func(t *testing.T) {
			if response := runV2(t, &m, tc.op, tc.params); !response.OK || !tc.ok(response) {
				t.Fatalf("%s %+v = %+v", tc.op, tc.params, response)
			}
		})
	}
	if len(fake.followed) != 2 || !fake.followed[0] || fake.followed[1] {
		t.Errorf("followed = %v, want [true false]", fake.followed)
	}
}
