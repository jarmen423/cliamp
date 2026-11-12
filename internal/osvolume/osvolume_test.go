package osvolume

import (
	"math"
	"testing"
)

func TestClamp(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{-0.5, 0}, {0, 0}, {0.42, 0.42}, {1, 1}, {1.5, 1}, {math.NaN(), 0}, {math.Inf(1), 1},
	} {
		if got := clamp(tc.in); got != tc.want {
			t.Errorf("clamp(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{{0, "0%"}, {0.4, "40%"}, {0.425, "43%"}, {1, "100%"}} {
		if got := percent(tc.in); got != tc.want {
			t.Errorf("percent(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseWpctl(t *testing.T) {
	for _, tc := range []struct {
		name      string
		out       string
		level     float64
		muted     bool
		wantError bool
	}{
		{name: "plain", out: "Volume: 0.40\n", level: 0.40},
		{name: "muted", out: "Volume: 0.85 [MUTED]\n", level: 0.85, muted: true},
		{name: "above 100% caps", out: "Volume: 1.50\n", level: 1},
		{name: "zero", out: "Volume: 0.00", level: 0},
		{name: "empty", out: "", wantError: true},
		{name: "garbage", out: "Translate ID failed", wantError: true},
		{name: "bad number", out: "Volume: abc", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, muted, err := parseWpctl(tc.out)
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v, want error %v", err, tc.wantError)
			}
			if err == nil && (level != tc.level || muted != tc.muted) {
				t.Fatalf("got %v %v, want %v %v", level, muted, tc.level, tc.muted)
			}
		})
	}
}

func TestParsePactl(t *testing.T) {
	for _, tc := range []struct {
		name      string
		out       string
		level     float64
		wantError bool
	}{
		{name: "stereo", out: "Volume: front-left: 26214 /  40% / -23.88 dB,   front-right: 26214 /  40% / -23.88 dB\n        balance 0.00\n", level: 0.40},
		{name: "unbalanced uses first channel", out: "Volume: front-left: 32768 /  50% / -18.06 dB,   front-right: 19661 /  30% / -31.37 dB\n", level: 0.50},
		{name: "boosted caps", out: "Volume: mono: 98304 / 150% / 10.57 dB\n", level: 1},
		{name: "no percent", out: "Failure: No such entity\n", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, err := parsePactlVolume(tc.out)
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v, want error %v", err, tc.wantError)
			}
			if err == nil && level != tc.level {
				t.Fatalf("level = %v, want %v", level, tc.level)
			}
		})
	}
	for _, tc := range []struct {
		out       string
		muted     bool
		wantError bool
	}{{"Mute: yes\n", true, false}, {"Mute: no\n", false, false}, {"Mute: maybe", false, true}, {"", false, true}} {
		muted, err := parsePactlMute(tc.out)
		if (err != nil) != tc.wantError || (err == nil && muted != tc.muted) {
			t.Errorf("parsePactlMute(%q) = %v, %v; want %v, error %v", tc.out, muted, err, tc.muted, tc.wantError)
		}
	}
}

func TestParseAmixer(t *testing.T) {
	stereo := `Simple mixer control 'Master',0
  Capabilities: pvolume pswitch pswitch-joined
  Playback channels: Front Left - Front Right
  Limits: Playback 0 - 65536
  Mono:
  Front Left: Playback 26214 [40%] [on]
  Front Right: Playback 26214 [40%] [on]
`
	for _, tc := range []struct {
		name      string
		out       string
		level     float64
		muted     bool
		wantError bool
	}{
		{name: "stereo on", out: stereo, level: 0.40},
		{name: "mono off with dB", out: "  Mono: Playback 31 [48%] [-16.00dB] [off]\n", level: 0.48, muted: true},
		{name: "no switch", out: "  Mono: Playback 87 [87%]\n", level: 0.87},
		{name: "no control", out: "amixer: Unable to find simple control 'Master',0\n", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, muted, err := parseAmixer(tc.out)
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v, want error %v", err, tc.wantError)
			}
			if err == nil && (level != tc.level || muted != tc.muted) {
				t.Fatalf("got %v %v, want %v %v", level, muted, tc.level, tc.muted)
			}
		})
	}
}

func TestParseOSAScript(t *testing.T) {
	for _, tc := range []struct {
		name      string
		out       string
		level     float64
		muted     bool
		wantError bool
	}{
		{name: "unmuted", out: "output volume:40, input volume:75, alert volume:100, output muted:false\n", level: 0.40},
		{name: "muted", out: "output volume:0, input volume:50, alert volume:100, output muted:true", level: 0, muted: true},
		{name: "full", out: "output volume:100, input volume:missing value, alert volume:100, output muted:false", level: 1},
		{name: "no volume control", out: "output volume:missing value, input volume:75, alert volume:100, output muted:missing value", wantError: true},
		{name: "empty", out: "", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, muted, err := parseOSAScript(tc.out)
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v, want error %v", err, tc.wantError)
			}
			if err == nil && (level != tc.level || muted != tc.muted) {
				t.Fatalf("got %v %v, want %v %v", level, muted, tc.level, tc.muted)
			}
		})
	}
}
