package spotify

import (
	"context"
	"net/url"
	"os"
	"testing"

	"golang.org/x/oauth2"
)

func meHandler(t *testing.T, _ url.Values) string {
	return `{"id":"fred","display_name":"Fred Bloggs","product":"premium","images":[{"url":"https://img/fred"}]}`
}

func TestAccountCached(t *testing.T) {
	m := newMockAPI(t)
	m.handlers["/v1/me"] = meHandler
	p := newTestProvider()
	for range 2 {
		got, err := p.Account(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "fred" || got.Name != "Fred Bloggs" || got.Detail != "premium" || got.ImageURL != "https://img/fred" {
			t.Errorf("account = %+v, want Fred Bloggs premium", got)
		}
	}
	if n := m.calls("/v1/me"); n != 1 {
		t.Errorf("/v1/me called %d times, want 1 (cached)", n)
	}
}

func TestSwitchAccountSignsInFresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveCreds(&storedCreds{Username: "old-user", Data: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	m := newMockAPI(t)
	m.handlers["/v1/me"] = meHandler

	orig := signIn
	t.Cleanup(func() { signIn = orig })
	var gotExisting *Session
	signIn = func(_ context.Context, _ string, existing *Session) (*Session, error) {
		gotExisting = existing
		if _, err := os.Stat(mustCredsPath(t)); !os.IsNotExist(err) {
			t.Errorf("credentials still on disk at sign-in, stat err = %v", err)
		}
		return &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}, nil
	}

	p := newTestProvider() // holds a live session for the old account
	p.trackCache[savedTracksPlaylistID] = &playlistCache{}
	got, err := p.SwitchAccount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Fred Bloggs" {
		t.Errorf("account = %+v, want the new account", got)
	}
	if gotExisting != nil {
		t.Errorf("sign-in reused the old session, want a fresh one")
	}
	if _, ok := p.trackCache[savedTracksPlaylistID]; ok {
		t.Errorf("Liked Songs cache survived the switch")
	}
}

func mustCredsPath(t *testing.T) string {
	t.Helper()
	path, err := CredsPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStoredUsername(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := StoredUsername(); got != "" {
		t.Errorf("StoredUsername() = %q with no credentials, want empty", got)
	}
	if err := saveCreds(&storedCreds{Username: "fred"}); err != nil {
		t.Fatal(err)
	}
	if got := StoredUsername(); got != "fred" {
		t.Errorf("StoredUsername() = %q, want fred", got)
	}
}
