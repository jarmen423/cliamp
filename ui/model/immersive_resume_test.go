package model

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/playlist"
)

var restoreLists = []playlist.PlaylistInfo{
	{ID: "pl1", Name: "One"}, {ID: "pl2", Name: "Two"}, {ID: "pl3", Name: "Three"},
}

// relaunch starts a fresh immersive session on immProvStub with v pending,
// the way main wires a saved page, and returns the model and startup command.
func relaunch(t *testing.T, v *resume.View) (*Model, tea.Cmd) {
	t.Helper()
	m := immersiveModel(t)
	m.immersive = immersiveState{}
	m.provider = immProvStub{}
	m.SetImmersiveRestore(v)
	if !m.openImmersiveOnce {
		t.Fatal("a saved page must open immersive on start")
	}
	return m, m.startImmersive()
}

// deliver runs msg through Update and returns the follow-up command.
func deliver(m *Model, msg tea.Msg) tea.Cmd {
	updated, cmd := m.Update(msg)
	*m = updated.(Model)
	return cmd
}

func listsAnswer(m *Model, err error) tea.Cmd {
	return deliver(m, immersiveListsMsg{lists: restoreLists, providerName: "stub", gen: m.requests.immersiveLists, err: err})
}

// The page on screen is saved by name; the settings tab saves what is
// under it, and outside immersive nothing is saved.
func TestImmersiveResumeViewSavesPage(t *testing.T) {
	m := immersiveModel(t)
	m.immersive.view = immViewPlaylist
	m.immersive.ctxKind, m.immersive.ctxID, m.immersive.ctxName, m.immersive.ctxSub = immKindPlaylist, "pl2", "Two", "Playlist"
	m.immersive.cursor, m.immersive.scroll = 5, 2
	want := resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl2", Name: "Two", Sub: "Playlist", Cursor: 5, Scroll: 2}
	if got := m.immersiveResumeView(); got == nil || *got != want {
		t.Fatalf("view = %+v, want %+v", got, want)
	}
	m.immersive.settingsReturn, m.immersive.view = immViewPlaylist, immViewSettings
	if got := m.immersiveResumeView(); got == nil || got.View != "playlist" {
		t.Fatalf("settings tab saved %+v, want the playlist under it", got)
	}
	m.immersive.active = false
	if got := m.immersiveResumeView(); got != nil {
		t.Fatalf("classic layout saved %+v, want nil", got)
	}
}

func TestImmersiveRestoreReopensPage(t *testing.T) {
	for _, tt := range []struct {
		name       string
		view       resume.View
		wantView   immersiveView
		wantCtx    string
		wantCursor int
	}{
		{"playlist with cursor", resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl2", Name: "Two", Cursor: 1}, immViewPlaylist, "pl2", 1},
		{"cursor past the end clamps", resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl2", Cursor: 40}, immViewPlaylist, "pl2", 1},
		{"browse list", resume.View{Provider: "stub", Section: "playlists", View: "browse", Cursor: 2}, immViewBrowse, "", 2},
		{"search re-runs its query", resume.View{Provider: "stub", Section: "search", View: "search", Query: "kanye"}, immViewSearch, "", 0},
		{"other provider falls back to the root", resume.View{Provider: "Spotify", Section: "albums", View: "album", Kind: "album", ID: "al9"}, immViewBrowse, "", 0},
		{"unknown section falls back to the root", resume.View{Provider: "stub", Section: "moods", View: "browse", Cursor: 2}, immViewBrowse, "", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := relaunch(t, &tt.view)
			cmd := listsAnswer(m, nil)
			switch tt.wantView {
			case immViewPlaylist:
				deliver(m, runCmdUntil[immersiveContentMsg](t, cmd))
			case immViewSearch:
				deliver(m, runCmdUntil[immersiveSearchMsg](t, cmd))
			}
			im := m.immersive
			if im.view != tt.wantView || im.ctxID != tt.wantCtx || im.cursor != tt.wantCursor {
				t.Fatalf("view=%d ctx=%q cursor=%d, want view=%d ctx=%q cursor=%d", im.view, im.ctxID, im.cursor, tt.wantView, tt.wantCtx, tt.wantCursor)
			}
			if m.immRestore != nil || m.immRestorePos != nil {
				t.Fatal("restore left pending after its page loaded")
			}
		})
	}
}

// A provider still signing in keeps the saved page pending; it reopens after
// sign-in, when startImmersive runs again and the lists load.
func TestImmersiveRestoreWaitsForSignIn(t *testing.T) {
	m, _ := relaunch(t, &resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl3"})
	if cmd := listsAnswer(m, playlist.ErrNeedsAuth); cmd != nil || m.immRestore == nil {
		t.Fatalf("restore ran or was dropped before sign-in (pending=%v)", m.immRestore != nil)
	}
	m.startImmersive() // what a finished sign-in does
	deliver(m, runCmdUntil[immersiveContentMsg](t, listsAnswer(m, nil)))
	if m.immersive.view != immViewPlaylist || m.immersive.ctxID != "pl3" {
		t.Fatalf("after sign-in: view=%d ctx=%q, want playlist pl3", m.immersive.view, m.immersive.ctxID)
	}
}

// Any other lists error drops the restore; the page stays at the root.
func TestImmersiveRestoreDroppedOnListsError(t *testing.T) {
	m, _ := relaunch(t, &resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl3"})
	if cmd := listsAnswer(m, errors.New("offline")); cmd != nil || m.immRestore != nil {
		t.Fatal("restore survived a failed provider")
	}
}

// A saved collection that no longer loads falls back to the section root.
func TestImmersiveRestoreStaleCollection(t *testing.T) {
	m, _ := relaunch(t, &resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "gone", Cursor: 3})
	msg := runCmdUntil[immersiveContentMsg](t, listsAnswer(m, nil))
	msg.err = errors.New("404")
	deliver(m, msg)
	if m.immersive.view != immViewBrowse || m.immersive.ctxID != "" || m.immRestorePos != nil {
		t.Fatalf("view=%d ctx=%q pending=%v, want the section root", m.immersive.view, m.immersive.ctxID, m.immRestorePos != nil)
	}
}

// Moving before the restored list arrives cancels the saved cursor.
func TestImmersiveRestoreCursorYieldsToNavigation(t *testing.T) {
	m, _ := relaunch(t, &resume.View{Provider: "stub", Section: "playlists", View: "playlist", Kind: "playlist", ID: "pl2", Cursor: 1})
	msg := runCmdUntil[immersiveContentMsg](t, listsAnswer(m, nil))
	m.immersiveGoBack()
	if m.immRestorePos != nil {
		t.Fatal("Back kept the pending cursor")
	}
	deliver(m, msg) // late, and for a view the user left
	if m.immersive.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after the user moved on", m.immersive.cursor)
	}
}
