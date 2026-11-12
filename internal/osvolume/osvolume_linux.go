package osvolume

import (
	"os/exec"
	"strconv"
	"sync"
)

// backend drives one Linux volume command.
type backend struct {
	name     string
	get      func() (float64, bool, error)
	set      func(level float64) error
	setMuted func(muted bool) error
}

const (
	wpctlSink = "@DEFAULT_AUDIO_SINK@"
	pactlSink = "@DEFAULT_SINK@"
)

// backends lists the commands in order of preference: PipeWire, then
// PulseAudio, then ALSA.
var backends = []backend{
	{
		name: "wpctl",
		get: func() (float64, bool, error) {
			out, err := run("wpctl", "get-volume", wpctlSink)
			if err != nil {
				return 0, false, err
			}
			return parseWpctl(out)
		},
		set: func(level float64) error {
			_, err := run("wpctl", "set-volume", "--limit", "1.0", wpctlSink, strconv.FormatFloat(level, 'f', 2, 64))
			return err
		},
		setMuted: func(muted bool) error {
			_, err := run("wpctl", "set-mute", wpctlSink, boolArg(muted))
			return err
		},
	},
	{
		name: "pactl",
		get: func() (float64, bool, error) {
			out, err := run("pactl", "get-sink-volume", pactlSink)
			if err != nil {
				return 0, false, err
			}
			level, err := parsePactlVolume(out)
			if err != nil {
				return 0, false, err
			}
			if out, err = run("pactl", "get-sink-mute", pactlSink); err != nil {
				return 0, false, err
			}
			muted, err := parsePactlMute(out)
			return level, muted, err
		},
		set: func(level float64) error {
			_, err := run("pactl", "set-sink-volume", pactlSink, percent(level))
			return err
		},
		setMuted: func(muted bool) error {
			_, err := run("pactl", "set-sink-mute", pactlSink, boolArg(muted))
			return err
		},
	},
	{
		// -M uses the mapped volume, which follows loudness the way the
		// desktop sliders do.
		name: "amixer",
		get: func() (float64, bool, error) {
			out, err := run("amixer", "-M", "get", "Master")
			if err != nil {
				return 0, false, err
			}
			return parseAmixer(out)
		},
		set: func(level float64) error {
			_, err := run("amixer", "-q", "-M", "set", "Master", percent(level))
			return err
		},
		setMuted: func(muted bool) error {
			arg := "unmute"
			if muted {
				arg = "mute"
			}
			_, err := run("amixer", "-q", "set", "Master", arg)
			return err
		},
	},
}

var (
	pickOnce sync.Once
	picked   *backend
)

// current returns the backend to use, or nil when none is installed. It
// picks once: the first installed command that reads the volume, or else the
// first installed command, in case the audio server is not up yet.
func current() *backend {
	pickOnce.Do(func() {
		var installed []*backend
		for i := range backends {
			if _, err := exec.LookPath(backends[i].name); err == nil {
				installed = append(installed, &backends[i])
			}
		}
		for _, b := range installed {
			if _, _, err := b.get(); err == nil {
				picked = b
				return
			}
		}
		if len(installed) > 0 {
			picked = installed[0]
		}
	})
	return picked
}

func boolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func available() bool { return current() != nil }

func get() (float64, bool, error) {
	b := current()
	if b == nil {
		return 0, false, ErrUnsupported
	}
	return b.get()
}

func set(level float64) error {
	b := current()
	if b == nil {
		return ErrUnsupported
	}
	return b.set(level)
}

func setMuted(muted bool) error {
	b := current()
	if b == nil {
		return ErrUnsupported
	}
	return b.setMuted(muted)
}
