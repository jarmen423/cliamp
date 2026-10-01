package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/external/emby"
	"github.com/bjarneo/cliamp/external/jellyfin"
	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

func TestRestoredMediaServerSourcesUseCurrentTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := "emby-current"
		if strings.HasPrefix(r.URL.Path, "/jellyfin/") {
			token = "jellyfin-current"
		}
		if r.URL.Query().Get("api_key") != token || r.URL.Query().Get("ApiKey") != token {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	embyURL := server.URL + "/emby"
	jellyURL := server.URL + "/jellyfin"
	embyProv := emby.NewFromConfig(config.EmbyConfig{URL: embyURL, Token: "emby-current", UserID: "user"})
	jellyProv := jellyfin.NewFromConfig(config.JellyfinConfig{URL: jellyURL, Token: "jellyfin-current", UserID: "user"})
	for _, tt := range []struct {
		name  string
		base  string
		jelly *jellyfin.Provider
		emby  *emby.Provider
	}{
		{"Emby alone", embyURL, nil, embyProv},
		{"Jellyfin alone", jellyURL, jellyProv, nil},
		{"Emby with Jellyfin", embyURL, jellyProv, embyProv},
		{"Jellyfin with Emby", jellyURL, jellyProv, embyProv},
	} {
		t.Run(tt.name, func(t *testing.T) {
			saved := tt.base + "/Items/song/Download?api_key=revoked"
			state := resume.State{Path: saved, Context: []playlist.Track{{Path: saved}}}
			restore := sessionTrackRestorer(tt.jelly, embyURL)
			tracks, _, _, ok := restoreResumeContext(state, restore)
			if !ok {
				t.Fatal("saved context was not restored")
			}
			pl := playlist.New()
			requeueSession(pl, []playlist.Track{{Path: saved}}, restore)
			resolver := mediaServerSourceResolver(tt.jelly, tt.emby)
			for _, track := range append(tracks, pl.QueueTracks()...) {
				source, err := resolver(track.Path)
				if err != nil {
					t.Fatal(err)
				}
				response, err := http.Get(source.URL)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("restored source HTTP %d, want 200", response.StatusCode)
				}
			}
		})
	}
	for _, rawURL := range []string{
		"https://foreign.example/Items/song/Download?api_key=foreign",
		server.URL + "/foreign/Items/song/Download?api_key=foreign",
		server.URL + "/emby/radio?token=foreign",
	} {
		source, err := mediaServerSourceResolver(jellyProv, embyProv)(rawURL)
		if err != nil || source.URL != rawURL {
			t.Fatalf("foreign source changed: %q, err=%v", source.URL, err)
		}
	}
}
