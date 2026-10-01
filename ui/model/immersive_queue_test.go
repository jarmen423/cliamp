package model

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// immQueuePageModel plays A of A..E with D queued and opens the queue page.
func immQueuePageModel(t *testing.T) *Model {
	t.Helper()
	m := immersiveMouseModel(t)
	for _, p := range []string{"a", "b", "c", "d", "e"} {
		m.playlist.Add(playlist.Track{Title: strings.ToUpper(p), Artist: "X", Path: "/" + p})
	}
	m.playlist.SetIndex(0)
	m.playingTrack, _ = m.playlist.Track(0)
	m.playingTrackActive = true
	m.player.(*playbackFakeEngine).playing = true
	m.playlist.Queue(3)
	m.openImmersiveQueueView()
	m.View()
	return m
}

func immQueuePageRows(m *Model) []string {
	var out []string
	for _, it := range m.canvasItems() {
		if it.kind == immKindHeader {
			out = append(out, "# "+it.title)
		} else {
			out = append(out, it.title)
		}
	}
	return out
}

func TestQueueViewSections(t *testing.T) {
	m := immQueuePageModel(t)
	got := strings.Join(immQueuePageRows(m), ",")
	want := "# Now playing,A,# Next in queue,D,# Next up,B,C,D,E"
	if got != want {
		t.Fatalf("rows = %s\nwant   %s", got, want)
	}
	if m.immCanvasTitle() != "Queue" {
		t.Fatalf("title = %q, want Queue", m.immCanvasTitle())
	}
}

// Section labels are never selected: opening lands on the first row and
// moving steps over labels.
func TestQueueViewCursorSkipsHeaders(t *testing.T) {
	m := immQueuePageModel(t)
	for _, want := range []int{1, 3, 5} {
		if m.immersive.cursor != want {
			t.Fatalf("cursor = %d, want %d", m.immersive.cursor, want)
		}
		m.immersiveMove(1, 0)
	}
	m.immersive.cursor = 3
	m.immersiveMove(-1, 0)
	if m.immersive.cursor != 1 {
		t.Fatalf("moving up from the first queued row: cursor = %d, want 1", m.immersive.cursor)
	}
}

func TestQueueViewEnterJumpsAndDequeues(t *testing.T) {
	m := immQueuePageModel(t)
	m.immersive.cursor = 3 // D in Next in queue
	m.immersiveActivate()
	fake := m.player.(*playbackFakeEngine)
	if len(fake.playCalls) == 0 || fake.playCalls[len(fake.playCalls)-1] != "/d" {
		t.Fatalf("playCalls = %v, want /d last", fake.playCalls)
	}
	if m.playlist.QueueLen() != 0 {
		t.Fatalf("queue len = %d, want the played track dequeued", m.playlist.QueueLen())
	}
}

func TestQueueViewRemoveOnlyQueuedRows(t *testing.T) {
	m := immQueuePageModel(t)
	m.immersive.cursor = 5 // B in Next up
	m.handleImmersiveKey(keyMsg("x"))
	if m.playlist.QueueLen() != 1 {
		t.Fatal("x on a Next up row changed the queue")
	}
	m.immersive.cursor = 3
	m.handleImmersiveKey(keyMsg("x"))
	if m.playlist.QueueLen() != 0 || !m.playlistUndo.active {
		t.Fatalf("x on a queued row: queue len %d, undo %v; want removed with undo", m.playlist.QueueLen(), m.playlistUndo.active)
	}
}

func TestQueueViewShiftMovesQueuedRow(t *testing.T) {
	m := immQueuePageModel(t)
	m.playlist.Queue(4) // queue is D, E
	m.immersive.cursor = 3
	m.handleImmersiveKey(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	q := m.playlist.QueueEntries()
	if q[0].Track.Title != "E" || q[1].Track.Title != "D" || m.immersive.cursor != 4 {
		t.Fatalf("queue = %s,%s cursor %d; want E,D with the cursor following D", q[0].Track.Title, q[1].Track.Title, m.immersive.cursor)
	}
}

// Next up follows the play order, so shuffle reorders it; the page is a
// list in every canvas mode; Esc returns to where it was opened from.
func TestQueueViewShuffleListAndBack(t *testing.T) {
	m := immQueuePageModel(t)
	m.playlist.ToggleShuffle()
	var want []string
	for _, e := range m.playlist.Upcoming(immQueueViewUpcoming) {
		want = append(want, e.Track.Title)
	}
	rows := immQueuePageRows(m)
	if got := rows[len(rows)-len(want):]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Next up = %v, want play order %v", got, want)
	}
	m.immersive.mode = immCanvasGrid
	m.View()
	for _, it := range m.immMouse.items {
		if it.box.H != 1 {
			t.Fatalf("queue page item box %v, want one-line list rows in grid mode", it.box)
		}
	}
	m.handleImmersiveKey(keyMsg("esc"))
	if m.immersive.view != immViewBrowse {
		t.Fatalf("view after Esc = %d, want the browse view it was opened from", m.immersive.view)
	}
}

func TestQueuePanelHeaderClickOpensQueueView(t *testing.T) {
	m := immersiveMouseModel(t)
	g := m.immMouse.geom
	immClickAt(m, 2, g.bodyY+g.queueY+1, tea.MouseLeft)
	if m.immersive.view != immViewQueue {
		t.Fatalf("view = %d, want the queue page", m.immersive.view)
	}
}

// radioStub adds a Recommender and ArtistDetailLoader to immProvStub.
type radioStub struct {
	immProvStub
	seeds [][]playlist.Track
}

func (s *radioStub) RecommendTracks(_ context.Context, seed []playlist.Track, _ int) ([]playlist.Track, error) {
	s.seeds = append(s.seeds, seed)
	return []playlist.Track{{Title: "R1", Path: "/r1"}, {Title: "R2", Path: "/r2"}}, nil
}

func (s *radioStub) ArtistDetail(string) (provider.ArtistDetail, error) {
	return provider.ArtistDetail{Popular: []playlist.Track{{Title: "P1", Path: "/p1"}, {Title: "P2", Path: "/p2"}}}, nil
}

func runRadio(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("radio started nothing")
	}
	msg, ok := cmd().(collectionRadioMsg)
	if !ok {
		t.Fatal("radio did not produce a collectionRadioMsg")
	}
	m.handleCollectionRadio(msg)
}

func playlistPaths(p *playlist.Playlist) string {
	var out []string
	for _, t := range p.Tracks() {
		out = append(out, t.Path)
	}
	return strings.Join(out, ",")
}

func TestImmersiveRadioForOpenArtist(t *testing.T) {
	m := immersiveMouseModel(t)
	stub := &radioStub{}
	m.immersive.prov = stub
	m.immersive.view, m.immersive.ctxKind, m.immersive.ctxID, m.immersive.ctxName = immViewArtist, immKindArtist, "ar1", "Lizzo"
	runRadio(t, m, m.handleImmersiveKey(keyMsg("R")))
	if got := playlistPaths(m.playlist); got != "/p1,/r1,/p2,/r2" {
		t.Fatalf("radio queue = %s, want the artist's tracks mixed with recommendations", got)
	}
	if len(stub.seeds) != 1 || len(stub.seeds[0]) != 2 {
		t.Fatalf("seeds = %v, want the artist's two popular tracks", stub.seeds)
	}
	if fake := m.player.(*playbackFakeEngine); len(fake.playCalls) == 0 || fake.playCalls[len(fake.playCalls)-1] != "/p1" {
		t.Fatalf("playCalls = %v, want the radio to start on /p1", fake.playCalls)
	}
	if !strings.Contains(m.status.text, "Artist radio") {
		t.Fatalf("status = %q, want it to name the artist radio", m.status.text)
	}
}

func TestImmersiveRadioForFocusedPlaylist(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.prov = &radioStub{}
	m.immersive.cursor = 0 // pl1 in the Playlists browse
	runRadio(t, m, m.immersiveRadio())
	if got := playlistPaths(m.playlist); got != "/a,/r1,/b,/r2" {
		t.Fatalf("radio queue = %s, want the playlist's tracks mixed with recommendations", got)
	}
}

func TestImmersiveRadioNeedsRecommender(t *testing.T) {
	m := immersiveMouseModel(t)
	if cmd := m.immersiveRadio(); cmd != nil {
		t.Fatal("radio started on a provider without recommendations")
	}
	if !strings.Contains(m.status.text, "recommendations") {
		t.Fatalf("status = %q, want the reason shown", m.status.text)
	}
}

// A radio started before a newer one must not replace the queue.
func TestCollectionRadioStaleResultDropped(t *testing.T) {
	m := immersiveMouseModel(t)
	m.immersive.prov = &radioStub{}
	m.immersive.view, m.immersive.ctxKind, m.immersive.ctxID = immViewArtist, immKindArtist, "ar1"
	stale := m.immersiveRadio()
	m.immersiveRadio()
	runRadio(t, m, stale)
	if m.playlist.Len() != 0 {
		t.Fatalf("stale radio replaced the playlist with %s", playlistPaths(m.playlist))
	}
}

func TestSampleSeedsSpreadsAcrossList(t *testing.T) {
	var tracks []playlist.Track
	for i := range 100 {
		tracks = append(tracks, playlist.Track{Path: "/" + string(rune('A'+i%26)) + strings.Repeat("x", i/26), Title: "t"})
	}
	tracks = append([]playlist.Track{{Title: "album", ProviderMeta: map[string]string{playlist.MetaKind: playlist.MetaKindAlbum}}}, tracks...)
	got := sampleSeeds(tracks, 10)
	if len(got) != 10 || got[0].Path != tracks[1].Path || got[9].Path != tracks[91].Path {
		t.Fatalf("seeds %d: first %q last %q, want 10 spread from the first playable to near the end", len(got), got[0].Path, got[len(got)-1].Path)
	}
	if short := sampleSeeds(tracks[:4], 10); len(short) != 3 {
		t.Fatalf("short list: %d seeds, want every playable track (3)", len(short))
	}
}

func TestMixRadio(t *testing.T) {
	tr := func(paths ...string) []playlist.Track {
		out := make([]playlist.Track, len(paths))
		for i, p := range paths {
			out[i] = playlist.Track{Path: p}
		}
		return out
	}
	for _, tt := range []struct {
		name        string
		seeds, recs []playlist.Track
		want        string
	}{
		{"interleaves", tr("s1", "s2"), tr("r1", "r2", "r3", "r4"), "s1,r1,r2,s2,r3,r4"},
		{"drops repeats", tr("s1", "s2"), tr("s2", "r1"), "s1,s2,r1"},
		{"no recommendations", tr("s1", "s2"), nil, "s1,s2"},
		{"more seeds than recs", tr("s1", "s2", "s3"), tr("r1"), "s1,r1,s2,s3"},
	} {
		var got []string
		for _, t := range mixRadio(tt.seeds, tt.recs) {
			got = append(got, t.Path)
		}
		if strings.Join(got, ",") != tt.want {
			t.Errorf("%s: %v, want %s", tt.name, got, tt.want)
		}
	}
}
