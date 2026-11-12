package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
)

// fakeSystemVolume is a SystemVolume in memory. err fails every call.
type fakeSystemVolume struct {
	mu    sync.Mutex
	level float64
	muted bool
	err   error
	gets  int
}

func (f *fakeSystemVolume) Get() (float64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return f.level, f.muted, f.err
}

func (f *fakeSystemVolume) Set(level float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.level = level
	}
	return f.err
}

func (f *fakeSystemVolume) SetMuted(muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.muted = muted
	}
	return f.err
}

func newSystemVolumeModel(backend SystemVolume) Model {
	m := Model{player: &playbackFakeEngine{}, playlist: playlist.New()}
	if backend != nil {
		m.SetSystemVolume(backend)
	}
	return m
}

func TestV2SystemVolume(t *testing.T) {
	for _, tc := range []struct {
		name      string
		op        string
		params    ipc.Request
		level     float64 // the system volume before the job
		muted     bool
		err       error
		wantError string
		wantLevel float64
		wantMuted bool
	}{
		{name: "set level", op: "system.volume", params: ipc.Request{Value: 40}, level: 0.8, wantLevel: 0.40},
		{name: "set zero", op: "system.volume", params: ipc.Request{Value: 0}, level: 0.8, wantLevel: 0},
		{name: "above 100 clamps", op: "system.volume", params: ipc.Request{Value: 150}, level: 0.2, wantLevel: 1},
		{name: "below 0 clamps", op: "system.volume", params: ipc.Request{Value: -5}, level: 0.2, wantLevel: 0},
		{name: "set keeps mute", op: "system.volume", params: ipc.Request{Value: 55}, muted: true, wantLevel: 0.55, wantMuted: true},
		{name: "mute on", op: "system.mute", params: ipc.Request{Name: "on"}, level: 0.5, wantLevel: 0.5, wantMuted: true},
		{name: "mute off", op: "system.mute", params: ipc.Request{Name: "OFF"}, level: 0.5, muted: true, wantLevel: 0.5},
		{name: "mute toggle", op: "system.mute", params: ipc.Request{}, level: 0.5, wantLevel: 0.5, wantMuted: true},
		{name: "mute bad name", op: "system.mute", params: ipc.Request{Name: "loud"}, level: 0.5, wantError: ipc.V2ErrorCodeInvalidParams, wantLevel: 0.5},
		{name: "backend error", op: "system.volume", params: ipc.Request{Value: 10}, level: 0.5, err: errors.New("no sink"), wantError: ipc.V2ErrorCodeInternal + ": no sink", wantLevel: 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSystemVolume{level: tc.level, muted: tc.muted}
			m := newSystemVolumeModel(fake)
			fake.err = tc.err
			got := runV2(t, &m, tc.op, tc.params)
			if got.Error != tc.wantError {
				t.Fatalf("error = %q, want %q", got.Error, tc.wantError)
			}
			if fake.level != tc.wantLevel || fake.muted != tc.wantMuted {
				t.Fatalf("system = %v muted %v, want %v muted %v", fake.level, fake.muted, tc.wantLevel, tc.wantMuted)
			}
			snap := m.runtimeSnapshot()
			if tc.wantError != "" {
				if snap.SystemVolume != nil {
					t.Fatalf("snapshot system volume = %d after a failure, want none", *snap.SystemVolume)
				}
				return
			}
			want := toPercent(tc.wantLevel)
			if got.SystemVolume == nil || *got.SystemVolume != want || got.SystemMuted == nil || *got.SystemMuted != tc.wantMuted {
				t.Fatalf("response = %v %v, want %d %v", got.SystemVolume, got.SystemMuted, want, tc.wantMuted)
			}
			if snap.SystemVolume == nil || *snap.SystemVolume != want || snap.SystemMuted == nil || *snap.SystemMuted != tc.wantMuted {
				t.Fatalf("snapshot = %v %v, want %d %v", snap.SystemVolume, snap.SystemMuted, want, tc.wantMuted)
			}
		})
	}
}

// Without a backend the operations fail and the snapshot has no system
// volume.
func TestV2SystemVolumeUnavailable(t *testing.T) {
	m := newSystemVolumeModel(nil)
	for _, op := range []string{"system.volume", "system.mute"} {
		if got := runV2(t, &m, op, ipc.Request{Value: 50}); got.Error != ipc.V2ErrorCodeUnavailable {
			t.Fatalf("%s error = %q, want %s", op, got.Error, ipc.V2ErrorCodeUnavailable)
		}
	}
	if snap := m.runtimeSnapshot(); snap.SystemVolume != nil || snap.SystemMuted != nil {
		t.Fatalf("snapshot = %v %v, want no system volume", snap.SystemVolume, snap.SystemMuted)
	}
	if cmd := m.pollSystemVolume(); cmd != nil {
		t.Fatal("poll without a backend returned a command")
	}
}

// Init reads the system volume once, and the read schedules the next poll.
func TestSystemVolumeInitRead(t *testing.T) {
	fake := &fakeSystemVolume{level: 0.42, muted: true}
	m := newSystemVolumeModel(fake)
	var read *systemVolumeMsg
	for _, msg := range runCmd(m.Init()) {
		if v, ok := msg.(systemVolumeMsg); ok {
			read = &v
		}
	}
	if read == nil || !read.poll {
		t.Fatalf("Init read = %+v, want a poll read", read)
	}
	if cmd := m.handleSystemVolume(*read); cmd == nil {
		t.Fatal("a poll read did not schedule the next poll")
	}
	if snap := m.runtimeSnapshot(); snap.SystemVolume == nil || *snap.SystemVolume != 42 || !*snap.SystemMuted {
		t.Fatalf("snapshot = %v %v, want 42 muted", snap.SystemVolume, snap.SystemMuted)
	}
}

// The poll reads only while an IPC client subscribes.
func TestSystemVolumePollNeedsSubscriber(t *testing.T) {
	prev := teaTick
	t.Cleanup(func() { teaTick = prev })
	teaTick = func(_ time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Now()) }
	}
	fake := &fakeSystemVolume{level: 0.3}
	m := newSystemVolumeModel(fake)
	broker := ipc.NewBroker()
	m.SetIPCBroker(broker)

	if msgs := runCmd(m.pollSystemVolume()); len(msgs) != 1 || msgs[0] != (systemVolumePollMsg{}) {
		t.Fatalf("poll with no subscriber = %v, want the next poll", msgs)
	}
	if fake.gets != 0 {
		t.Fatalf("gets = %d with no subscriber, want 0", fake.gets)
	}

	sub, err := broker.Subscribe([]string{ipcRuntimeEventState})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	msgs := runCmd(m.pollSystemVolume())
	if len(msgs) != 1 || fake.gets != 1 {
		t.Fatalf("poll with a subscriber = %v, gets %d, want one read", msgs, fake.gets)
	}
	if read, ok := msgs[0].(systemVolumeMsg); !ok || !read.poll || read.percent != 30 {
		t.Fatalf("poll read = %+v, want 30%%", msgs[0])
	}
}

// A read that started before a change does not overwrite the change.
func TestSystemVolumeDropsStaleRead(t *testing.T) {
	fake := &fakeSystemVolume{level: 0.9}
	m := newSystemVolumeModel(fake)
	stale := runCmd(readSystemVolumeCmd(fake, m.sysVolume.seq, true))[0].(systemVolumeMsg)
	runV2(t, &m, "system.volume", ipc.Request{Value: 20})
	m.handleSystemVolume(stale)
	if snap := m.runtimeSnapshot(); snap.SystemVolume == nil || *snap.SystemVolume != 20 {
		t.Fatalf("snapshot = %v, want 20 after a stale read", snap.SystemVolume)
	}
}

// A system volume change publishes a runtime event.
func TestSystemVolumeChangePublishes(t *testing.T) {
	m := newSystemVolumeModel(&fakeSystemVolume{})
	m.SetIPCBroker(ipc.NewBroker())
	m.publishIPCRuntimeState()
	before := m.ipcRuntime.revision
	m.handleSystemVolume(systemVolumeMsg{percent: 65})
	m.publishIPCRuntimeState()
	if m.ipcRuntime.revision == before {
		t.Fatal("a system volume change did not publish")
	}
}
