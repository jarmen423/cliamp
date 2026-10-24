package spotify

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/dealer"
	librespotPlayer "github.com/devgianlu/go-librespot/player"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
	"google.golang.org/protobuf/proto"

	"github.com/bjarneo/cliamp/internal/playback"
	"github.com/bjarneo/cliamp/playlist"
)

func mustPayload(t *testing.T, body string) dealer.RequestPayload {
	t.Helper()
	var p dealer.RequestPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

func newTestReceiver(send func(any)) *connectReceiver {
	return newConnectReceiver(&Session{}, ConnectConfig{Name: "test-cliamp", Send: send})
}

func TestConnectConfigDeviceName(t *testing.T) {
	tests := []struct {
		name string
		cfg  ConnectConfig
		want string
	}{
		{"empty falls back to default", ConnectConfig{}, connectDefaultName},
		{"blank falls back to default", ConnectConfig{Name: "  "}, connectDefaultName},
		{"custom name", ConnectConfig{Name: "Kitchen"}, "Kitchen"},
	}
	for _, tt := range tests {
		if got := tt.cfg.deviceName(); got != tt.want {
			t.Errorf("deviceName() = %q, want %q", got, tt.want)
		}
	}
}

func TestRepeatMode(t *testing.T) {
	tests := []struct {
		name string
		opts *connectpb.ContextPlayerOptions
		want playlist.RepeatMode
	}{
		{"nil", nil, playlist.RepeatOff},
		{"off", &connectpb.ContextPlayerOptions{}, playlist.RepeatOff},
		{"context", &connectpb.ContextPlayerOptions{RepeatingContext: true}, playlist.RepeatAll},
		{"track", &connectpb.ContextPlayerOptions{RepeatingTrack: true}, playlist.RepeatOne},
		{"track beats context", &connectpb.ContextPlayerOptions{RepeatingContext: true, RepeatingTrack: true}, playlist.RepeatOne},
	}
	for _, tt := range tests {
		if got := repeatMode(tt.opts); got != tt.want {
			t.Errorf("%s: repeatMode() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestProvidedToPlaylist(t *testing.T) {
	pt := &connectpb.ProvidedTrack{
		Uri: "spotify:track:abc",
		Metadata: map[string]string{
			"title":       "Song",
			"artist_name": "Artist",
			"album_title": "Album",
			"duration_ms": "241000",
		},
	}
	got := providedToPlaylist(pt)
	if got.Path != "spotify:track:abc" || got.Title != "Song" || got.Artist != "Artist" ||
		got.Album != "Album" || got.DurationSecs != 241 {
		t.Fatalf("providedToPlaylist() = %+v", got)
	}
}

func TestProvidedToPlaylistUnplayable(t *testing.T) {
	got := providedToPlaylist(&connectpb.ProvidedTrack{Uri: "spotify:track:x", Removed: []string{"x"}})
	if !got.Unplayable {
		t.Fatal("removed track should map to Unplayable")
	}
}

func TestEnrichTracksFillsMissingMetadata(t *testing.T) {
	r := newTestReceiver(func(any) {})
	asked := stubItemMetadata(t, map[string]playlist.Track{
		"spotify:track:a":   {Title: "A", Artist: "Artist A", Album: "Album A", DurationSecs: 200, ProviderMeta: map[string]string{playlist.MetaAlbumID: "alb"}},
		"spotify:episode:e": {Title: "Ep", Artist: "Show", Album: "Show"},
	}, nil)
	trs := []playlist.Track{
		{Path: "spotify:track:a"},
		{Path: "spotify:track:named", Title: "Kept"},
		{Path: "spotify:episode:e"},
		{Path: "spotify:track:gone"},
		{Path: "/music/local.mp3"},
	}

	r.enrichTracks(trs)

	if want := []string{"spotify:track:a", "spotify:episode:e", "spotify:track:gone"}; len(*asked) != 1 || !slices.Equal((*asked)[0], want) {
		t.Fatalf("looked up %v, want %v", *asked, want)
	}
	if got := trs[0]; got.Title != "A" || got.Artist != "Artist A" || got.Album != "Album A" ||
		got.DurationSecs != 200 || got.AlbumID() != "alb" {
		t.Fatalf("track = %+v", got)
	}
	if trs[1].Title != "Kept" || trs[2].Title != "Ep" || trs[2].Artist != "Show" || trs[3].Title != "" {
		t.Fatalf("tracks = %+v", trs)
	}
}

func TestEnrichTracksLookupFailureKeepsTracks(t *testing.T) {
	r := newTestReceiver(func(any) {})
	stubItemMetadata(t, nil, errors.New("offline"))
	trs := []playlist.Track{{Path: "spotify:track:a"}}

	r.enrichTracks(trs)

	if trs[0].Path != "spotify:track:a" || trs[0].Title != "" {
		t.Fatalf("track = %+v", trs[0])
	}
}

func TestSplitSpotifyURI(t *testing.T) {
	tests := []struct {
		uri     string
		wantTyp string
		wantID  string
		wantOK  bool
	}{
		{"spotify:track:abc123", "track", "abc123", true},
		{"spotify:episode:xyz", "episode", "xyz", true},
		{"/local/file.mp3", "", "", false},
		{"spotify:noid", "", "", false},
		{"spotify:track:", "", "", false},
	}
	for _, tt := range tests {
		typ, id, ok := splitSpotifyURI(tt.uri)
		if typ != tt.wantTyp || id != tt.wantID || ok != tt.wantOK {
			t.Errorf("splitSpotifyURI(%q) = %q,%q,%v want %q,%q,%v", tt.uri, typ, id, ok, tt.wantTyp, tt.wantID, tt.wantOK)
		}
	}
}

func TestContextMetadataMerges(t *testing.T) {
	got := contextMetadata(
		map[string]string{"context_description": "My List", "shared": "ctx"},
		map[string]string{"shared": "list", "image_url": "img"},
	)
	if got["context_description"] != "My List" || got["image_url"] != "img" || got["shared"] != "ctx" {
		t.Fatalf("contextMetadata = %v", got)
	}
}

// TestHandleCommandDispatch table-drives remote commands into playback msgs.
func TestHandleCommandDispatch(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    func(any) bool
	}{
		{"pause", `{"command":{"endpoint":"pause"}}`, func(m any) bool { _, ok := m.(playback.PauseMsg); return ok }},
		{"resume", `{"command":{"endpoint":"resume"}}`, func(m any) bool { _, ok := m.(playback.PlayMsg); return ok }},
		{"skip_next", `{"command":{"endpoint":"skip_next"}}`, func(m any) bool { _, ok := m.(playback.NextMsg); return ok }},
		{"skip_prev", `{"command":{"endpoint":"skip_prev"}}`, func(m any) bool { _, ok := m.(playback.PrevMsg); return ok }},
		{"seek absolute", `{"command":{"endpoint":"seek_to","value":45000}}`, func(m any) bool { v, ok := m.(playback.SetPositionMsg); return ok && v.Position == 45*time.Second }},
		{"seek beginning", `{"command":{"endpoint":"seek_to","position":12000,"relative":"beginning"}}`, func(m any) bool { v, ok := m.(playback.SetPositionMsg); return ok && v.Position == 12*time.Second }},
		{"seek relative", `{"command":{"endpoint":"seek_to","position":-3000,"relative":"current"}}`, func(m any) bool { v, ok := m.(playback.SeekMsg); return ok && v.Offset == -3*time.Second }},
		{"shuffle on", `{"command":{"endpoint":"set_shuffling_context","value":true}}`, func(m any) bool { v, ok := m.(playback.SetShuffleMsg); return ok && v.On }},
		{"shuffle off", `{"command":{"endpoint":"set_shuffling_context","value":false}}`, func(m any) bool { v, ok := m.(playback.SetShuffleMsg); return ok && !v.On }},
		{"repeat context", `{"command":{"endpoint":"set_repeating_context","value":true}}`, func(m any) bool { v, ok := m.(playback.SetRepeatMsg); return ok && v.Mode == playlist.RepeatAll }},
		{"repeat track", `{"command":{"endpoint":"set_repeating_track","value":true}}`, func(m any) bool { v, ok := m.(playback.SetRepeatMsg); return ok && v.Mode == playlist.RepeatOne }},
		{"queue add", `{"command":{"endpoint":"add_to_queue","track":{"uri":"spotify:track:q1"}}}`, func(m any) bool { v, ok := m.(playback.EnqueueMsg); return ok && v.Track.Path == "spotify:track:q1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []any
			r := newTestReceiver(func(m any) { sent = append(sent, m) })
			if !r.handleCommand(nil, mustPayload(t, tt.payload)) {
				t.Fatal("handleCommand returned false")
			}
			if len(sent) == 0 {
				t.Fatal("no message sent")
			}
			found := false
			for _, m := range sent {
				if tt.want(m) {
					found = true
				}
			}
			if !found {
				t.Fatalf("sent %v; expected message not found", sent)
			}
		})
	}
}

func TestHandleCommandUnknown(t *testing.T) {
	r := newTestReceiver(func(any) {})
	if r.handleCommand(nil, mustPayload(t, `{"command":{"endpoint":"bogus_cmd"}}`)) {
		t.Fatal("unknown command should return false")
	}
}

// TestOptionsToggleSequence: repeat track then context-off yields RepeatOne;
// clearing both yields RepeatOff — matching the app's toggle sequence.
func TestOptionsToggleSequence(t *testing.T) {
	var sent []any
	r := newTestReceiver(func(m any) { sent = append(sent, m) })

	must := func(body string) {
		t.Helper()
		if !r.handleCommand(nil, mustPayload(t, body)) {
			t.Fatalf("handleCommand failed: %s", body)
		}
	}
	must(`{"command":{"endpoint":"set_repeating_track","value":true}}`)
	must(`{"command":{"endpoint":"set_repeating_context","value":true}}`)
	if got := repeatMode(r.opts); got != playlist.RepeatOne {
		t.Fatalf("repeatMode = %v, want RepeatOne", got)
	}
	must(`{"command":{"endpoint":"set_repeating_track","value":false}}`)
	if got := repeatMode(r.opts); got != playlist.RepeatAll {
		t.Fatalf("repeatMode = %v, want RepeatAll", got)
	}
	must(`{"command":{"endpoint":"set_repeating_context","value":false}}`)
	if got := repeatMode(r.opts); got != playlist.RepeatOff {
		t.Fatalf("repeatMode = %v, want RepeatOff", got)
	}
}

// TestPlayerStateRules checks the invariants Spotify requires: paused ⇒ speed 0,
// playing ⇒ position advances between snapshot and report.
func TestPlayerStateRules(t *testing.T) {
	r := newTestReceiver(func(any) {})

	r.notifyUpdate(playback.State{
		Status:   playback.StatusPaused,
		Position: 30 * time.Second,
		Track:    playback.Track{URL: "spotify:track:abc", Duration: 3 * time.Minute},
	})
	st := r.playerState()
	if !st.IsPaused || st.PlaybackSpeed != 0 {
		t.Fatalf("paused state: IsPaused=%v speed=%v, want paused with speed 0", st.IsPaused, st.PlaybackSpeed)
	}
	if st.PositionAsOfTimestamp != 30_000 {
		t.Fatalf("paused position = %d, want 30000", st.PositionAsOfTimestamp)
	}
	if st.Track == nil || st.Track.Uri != "spotify:track:abc" {
		t.Fatalf("track = %+v, want spotify uri", st.Track)
	}

	r.notifyUpdate(playback.State{
		Status:   playback.StatusPlaying,
		Position: 30 * time.Second,
		Track:    playback.Track{URL: "spotify:track:abc", Duration: 3 * time.Minute},
	})
	time.Sleep(5 * time.Millisecond)
	st = r.playerState()
	if st.IsPaused || st.PlaybackSpeed != 1 {
		t.Fatalf("playing state: IsPaused=%v speed=%v", st.IsPaused, st.PlaybackSpeed)
	}
	if st.PositionAsOfTimestamp < 30_000 {
		t.Fatalf("playing position should advance, got %d", st.PositionAsOfTimestamp)
	}
}

// TestPlayerStateTransferClaim: between taking over playback and the TUI
// playing the track, the reported state is the claimed track, playing and
// buffering — never the stale "stopped" snapshot, which leaves the sending
// app playing too. The claim ends once the TUI plays the claimed track.
func TestPlayerStateTransferClaim(t *testing.T) {
	r := newTestReceiver(func(any) {})
	r.notifyUpdate(playback.State{Status: playback.StatusStopped})
	r.claimPlayback(nil, "spotify:track:new", 42*time.Second, false)

	st := r.playerState()
	if !st.IsPlaying || st.IsPaused || !st.IsBuffering || st.PlaybackSpeed != 0 {
		t.Fatalf("claim: playing=%v paused=%v buffering=%v speed=%v, want playing, buffering, speed 0",
			st.IsPlaying, st.IsPaused, st.IsBuffering, st.PlaybackSpeed)
	}
	if st.Track.GetUri() != "spotify:track:new" || st.PositionAsOfTimestamp != 42_000 {
		t.Fatalf("claim: track=%q pos=%d, want spotify:track:new at 42000", st.Track.GetUri(), st.PositionAsOfTimestamp)
	}

	// A snapshot still on the old track keeps the claim.
	r.notifyUpdate(playback.State{Status: playback.StatusPlaying, Track: playback.Track{URL: "spotify:track:old"}})
	if st = r.playerState(); st.Track.GetUri() != "spotify:track:new" {
		t.Fatalf("old snapshot replaced the claim: track=%q", st.Track.GetUri())
	}

	r.notifyUpdate(playback.State{Status: playback.StatusPlaying, Position: 43 * time.Second, Track: playback.Track{URL: "spotify:track:new"}})
	st = r.playerState()
	if st.IsBuffering || st.PlaybackSpeed != 1 || r.claim != nil {
		t.Fatalf("claim should end once the TUI plays the track: buffering=%v speed=%v claim=%v", st.IsBuffering, st.PlaybackSpeed, r.claim)
	}
}

func TestPlayerStateTransferClaimExpires(t *testing.T) {
	r := newTestReceiver(func(any) {})
	r.claimPlayback(nil, "spotify:track:new", 0, true)
	r.claim.until = time.Now().Add(-time.Second)
	if st := r.playerState(); st.IsBuffering || st.Track != nil {
		t.Fatalf("expired claim still reported: buffering=%v track=%v", st.IsBuffering, st.Track)
	}
}

// TestPlayerStateNonSpotifyTrack: local files keep the device registered but
// report no track (cliamp can't resolve arbitrary paths into Spotify URIs).
func TestPlayerStateNonSpotifyTrack(t *testing.T) {
	r := newTestReceiver(func(any) {})
	r.notifyUpdate(playback.State{
		Status: playback.StatusPlaying,
		Track:  playback.Track{URL: "/home/u/song.mp3", Title: "Local"},
	})
	st := r.playerState()
	if st.Track != nil {
		t.Fatalf("non-spotify track should not be reported, got %+v", st.Track)
	}
	if !st.IsPlaying {
		t.Fatal("device should still report playing")
	}
}

func TestDeviceInfo(t *testing.T) {
	r := newTestReceiver(func(any) {})
	r.notifyUpdate(playback.State{VolumeDB: -30}) // mute → 0 volume steps
	di := r.deviceInfo()
	if di.Name != "test-cliamp" {
		t.Fatalf("Name = %q", di.Name)
	}
	if di.DeviceType != devicespb.DeviceType_COMPUTER {
		t.Fatalf("DeviceType = %v", di.DeviceType)
	}
	if !di.Capabilities.SupportsTransferCommand || !di.Capabilities.IsControllable {
		t.Fatal("missing required Connect capabilities")
	}
	if di.Volume != 0 {
		t.Fatalf("Volume = %d, want 0 for -30dB", di.Volume)
	}
}

func TestSleepTimerEcho(t *testing.T) {
	var sent []any
	r := newTestReceiver(func(m any) { sent = append(sent, m) })
	r.handleCommand(nil, mustPayload(t, `{"command":{"endpoint":"set_sleep_timer","timer_type":{"type":"duration","duration_s":60}}}`))
	if r.sleepState == nil {
		t.Fatal("sleep timer state should be echoed")
	}
	ts := r.sleepState.GetTimestamp()
	if ts == nil || ts.Timestamp <= time.Now().UnixMilli() {
		t.Fatalf("sleep timer timestamp = %+v", ts)
	}
	r.handleCommand(nil, mustPayload(t, `{"command":{"endpoint":"set_sleep_timer","timer_type":{"type":"clear"}}}`))
	if _, ok := r.sleepState.TimerType.(*connectpb.SleepTimer_None_); !ok {
		t.Fatalf("cleared sleep timer = %v", r.sleepState.TimerType)
	}
}

// TestPutDelayRespectsCooldown: a 429 backoff must win over the regular
// ~200ms min-PUT spacing, even when a playback update arrives mid-cooldown
// with a fresh lastPut (which used to reset the timer and wipe the backoff).
func TestPutDelayRespectsCooldown(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name              string
		lastPut, cooldown time.Time
		wantMin, wantMax  time.Duration
	}{
		{"idle: PUT now", now.Add(-time.Second), time.Time{}, 0, 0},
		{"spacing only", now, time.Time{}, 150 * time.Millisecond, connectStateMinPut},
		{"cooldown beats fresh lastPut", now, now.Add(30 * time.Second), 29 * time.Second, 30 * time.Second},
		{"cooldown beats stale lastPut", now.Add(-time.Hour), now.Add(5 * time.Second), 4 * time.Second, 5 * time.Second},
		{"expired cooldown falls back to spacing", now, now.Add(-time.Second), 150 * time.Millisecond, connectStateMinPut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := putDelay(tt.lastPut, tt.cooldown, now); got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("putDelay() = %v, want in [%v, %v]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestNotifyUpdateOnlyDirtiesOnChange: the TUI notifies every tick, so
// position drift alone must not mark dirty — otherwise the receiver PUTs
// ~1/sec and risks being throttled. Only meaningful change dirties.
func TestNotifyUpdateOnlyDirtiesOnChange(t *testing.T) {
	r := newTestReceiver(func(any) {})
	drained := func() bool {
		select {
		case <-r.dirty:
			return true
		default:
			return false
		}
	}

	st := playback.State{
		Status:   playback.StatusPlaying,
		Track:    playback.Track{Title: "Song", URL: "spotify:track:abc"},
		VolumeDB: -6,
		Seekable: true,
	}
	r.notifyUpdate(st)
	if !drained() {
		t.Fatal("first notifyUpdate did not signal dirty")
	}
	// Identical state plus position drift: no dirty.
	st.Position += time.Second
	r.notifyUpdate(st)
	if drained() {
		t.Fatal("unchanged notifyUpdate signaled dirty")
	}
	// Status flip dirties again (snapshot stays fresh regardless).
	st.Status = playback.StatusPaused
	r.notifyUpdate(st)
	if !drained() {
		t.Fatal("changed notifyUpdate did not signal dirty")
	}
	if got, _ := r.snapshot(); got.Status != playback.StatusPaused {
		t.Fatalf("snapshot status = %q, want Paused", got.Status)
	}
}

// TestSchedulePutDefersToCooldown proves end-to-end that a dirty signal
// during backoff doesn't trigger an early PUT: lastPut is stale (so without
// the cooldown this would flush immediately), yet no PUT happens.
func TestSchedulePutDefersToCooldown(t *testing.T) {
	r := newTestReceiver(func(any) {})
	r.spotConnID = "conn-id"
	r.cooldownUntil = time.Now().Add(30 * time.Second)
	r.schedulePut(nil) // nil session: would panic if it flushed
	if !r.lastPut.IsZero() {
		t.Fatalf("schedulePut flushed during cooldown, lastPut = %v", r.lastPut)
	}
}

func TestVolumeMessage(t *testing.T) {
	var sent []any
	r := newTestReceiver(func(m any) { sent = append(sent, m) })
	cmd := &connectpb.SetVolumeCommand{Volume: librespotPlayer.MaxStateVolume}
	payload, err := proto.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r.handleMessage(nil, dealer.Message{Uri: "hm://connect-state/v1/connect/volume", Payload: payload})
	if len(sent) != 1 {
		t.Fatalf("sent %v", sent)
	}
	vm, ok := sent[0].(playback.SetVolumeMsg)
	if !ok {
		t.Fatalf("sent %T, want SetVolumeMsg", sent[0])
	}
	if vm.VolumeDB < 5.9 || vm.VolumeDB > 6.1 {
		t.Fatalf("VolumeDB = %v, want ~6", vm.VolumeDB)
	}
}

func TestWithAutoplayFollowsALoneSong(t *testing.T) {
	const seed = "spotify:track:seed"
	contextURI, recent := stubStation(t, []string{
		seed, "spotify:track:a", "spotify:episode:e", "spotify:track:a", "spotify:track:b",
	}, nil)
	r := newTestReceiver(func(any) {})
	r.ctxURI = seed

	got := r.withAutoplay([]playlist.Track{{Path: seed, Title: "Seed"}})

	var paths []string
	for _, tr := range got {
		paths = append(paths, tr.Path)
	}
	if want := []string{seed, "spotify:track:a", "spotify:track:b"}; !slices.Equal(paths, want) {
		t.Fatalf("tracks = %v, want %v (the song, then its station without repeats)", paths, want)
	}
	if got[0].Title != "Seed" {
		t.Errorf("song = %+v, want it kept as given", got[0])
	}
	if *contextURI != seed || !slices.Equal(*recent, []string{seed}) {
		t.Errorf("station asked for %q with recent %v, want the song", *contextURI, *recent)
	}
}

func TestWithAutoplayLeavesOtherContexts(t *testing.T) {
	for _, tt := range []struct {
		name, ctxURI string
		tracks       []playlist.Track
		stationErr   error
	}{
		{"album", "spotify:album:x", []playlist.Track{{Path: "spotify:track:a"}}, nil},
		{"playlist", "spotify:playlist:x", []playlist.Track{{Path: "spotify:track:a"}, {Path: "spotify:track:b"}}, nil},
		{"no station", "spotify:track:a", []playlist.Track{{Path: "spotify:track:a"}}, errors.New("no station")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stubStation(t, []string{"spotify:track:z"}, tt.stationErr)
			r := newTestReceiver(func(any) {})
			r.ctxURI = tt.ctxURI
			if got := r.withAutoplay(tt.tracks); len(got) != len(tt.tracks) {
				t.Errorf("got %d tracks, want the %d given", len(got), len(tt.tracks))
			}
		})
	}
}
