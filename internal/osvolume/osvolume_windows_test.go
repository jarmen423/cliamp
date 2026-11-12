//go:build windows

package osvolume

import (
	"errors"
	"math"
	"os"
	"testing"
)

// TestWindowsRoundTrip reads the master volume and mute state and writes the
// same values back, so it leaves the machine as it found it. A machine with
// no output device skips.
func TestWindowsRoundTrip(t *testing.T) {
	level, muted, err := Get()
	if errors.Is(err, ErrUnsupported) {
		t.Skipf("no output device: %v", err)
	}
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if level < 0 || level > 1 {
		t.Fatalf("Get level = %v, want 0..1", level)
	}
	if err := Set(level); err != nil {
		t.Fatalf("Set(%v): %v", level, err)
	}
	if err := SetMuted(muted); err != nil {
		t.Fatalf("SetMuted(%v): %v", muted, err)
	}
	got, gotMuted, err := Get()
	if err != nil {
		t.Fatalf("Get after write: %v", err)
	}
	if math.Abs(got-level) > 0.005 || gotMuted != muted {
		t.Fatalf("after write: level %v muted %v, want %v %v", got, gotMuted, level, muted)
	}
}

// TestWindowsSetChangesVolume moves the volume and mute state, checks that
// each change reads back, and restores the starting values. It turns the
// speakers down for a moment, so it runs only with CLIAMP_OSVOLUME_LIVE=1.
func TestWindowsSetChangesVolume(t *testing.T) {
	if os.Getenv("CLIAMP_OSVOLUME_LIVE") != "1" {
		t.Skip("set CLIAMP_OSVOLUME_LIVE=1 to change the system volume")
	}
	level, muted, err := Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	t.Cleanup(func() {
		if err := Set(level); err != nil {
			t.Errorf("restore level: %v", err)
		}
		if err := SetMuted(muted); err != nil {
			t.Errorf("restore mute: %v", err)
		}
	})

	// Every change goes quieter than the start, never louder.
	target := math.Round(level*50) / 100
	if err := Set(target); err != nil {
		t.Fatalf("Set(%v): %v", target, err)
	}
	if got, _, _ := Get(); math.Abs(got-target) > 0.005 {
		t.Fatalf("level after Set(%v) = %v", target, got)
	}
	if err := SetMuted(!muted); err != nil {
		t.Fatalf("SetMuted(%v): %v", !muted, err)
	}
	if _, got, _ := Get(); got != !muted {
		t.Fatalf("muted after SetMuted(%v) = %v", !muted, got)
	}
	// Set clamps out-of-range input.
	if err := Set(-0.5); err != nil {
		t.Fatalf("Set(-0.5): %v", err)
	}
	if got, _, _ := Get(); got > 0.005 {
		t.Fatalf("level after Set(-0.5) = %v, want 0", got)
	}
}
