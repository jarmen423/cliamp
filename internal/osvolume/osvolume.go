// Package osvolume reads and sets the master output volume and mute of the
// operating system: PipeWire, PulseAudio or ALSA on Linux, AppleScript on
// macOS and Core Audio on Windows.
//
// Each call can take tens of milliseconds, since most backends run a command.
// The calls are safe from any goroutine. Never make them on the Bubbletea
// event loop.
package osvolume

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupported is returned when the system has no volume control that
// osvolume can drive.
var ErrUnsupported = errors.New("osvolume: unsupported")

// Available reports whether the system volume can be read and set.
func Available() bool { return available() }

// Get returns the output level, from 0 to 1, and the mute state.
func Get() (level float64, muted bool, err error) { return get() }

// Set sets the output level. level is clamped to [0, 1].
func Set(level float64) error { return set(clamp(level)) }

// SetMuted mutes or unmutes the output.
func SetMuted(muted bool) error { return setMuted(muted) }

func clamp(level float64) float64 {
	if math.IsNaN(level) || level < 0 {
		return 0
	}
	return min(level, 1)
}

// commandTimeout bounds each volume command, so a hung audio server cannot
// hold a caller for long.
const commandTimeout = 2 * time.Second

// run runs a volume command and returns its standard output.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", fmt.Errorf("osvolume: %s: %w", name, err)
	}
	return string(out), nil
}

// wholePercent returns level as a whole percent.
func wholePercent(level float64) int { return int(math.Round(level * 100)) }

// percent formats level as a whole percent for pactl and amixer.
func percent(level float64) string { return strconv.Itoa(wholePercent(level)) + "%" }

// parseWpctl reads `wpctl get-volume` output, such as "Volume: 0.40" or
// "Volume: 0.40 [MUTED]". A level above 1 is reported as 1.
func parseWpctl(out string) (level float64, muted bool, err error) {
	fields := strings.Fields(out)
	if len(fields) < 2 || fields[0] != "Volume:" {
		return 0, false, fmt.Errorf("osvolume: unexpected wpctl output %q", strings.TrimSpace(out))
	}
	level, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, false, fmt.Errorf("osvolume: wpctl volume %q: %w", fields[1], err)
	}
	return clamp(level), strings.Contains(out, "[MUTED]"), nil
}

var (
	percentRe = regexp.MustCompile(`(\d+)%`)
	amixerRe  = regexp.MustCompile(`\[(\d+)%\](?:.*\[(on|off)\])?`)
)

// parsePactlVolume reads `pactl get-sink-volume` output and returns the level
// of the first channel.
func parsePactlVolume(out string) (float64, error) {
	match := percentRe.FindStringSubmatch(out)
	if match == nil {
		return 0, fmt.Errorf("osvolume: unexpected pactl output %q", strings.TrimSpace(out))
	}
	pct, _ := strconv.Atoi(match[1])
	return clamp(float64(pct) / 100), nil
}

// parsePactlMute reads `pactl get-sink-mute` output: "Mute: yes" or "Mute: no".
func parsePactlMute(out string) (bool, error) {
	switch strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "Mute:")) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return false, fmt.Errorf("osvolume: unexpected pactl output %q", strings.TrimSpace(out))
}

// parseAmixer reads `amixer get Master` output and returns the level and the
// switch of the first channel, as in "Front Left: Playback 26214 [40%] [on]".
// A control without a switch is never muted.
func parseAmixer(out string) (level float64, muted bool, err error) {
	match := amixerRe.FindStringSubmatch(out)
	if match == nil {
		return 0, false, fmt.Errorf("osvolume: unexpected amixer output %q", strings.TrimSpace(out))
	}
	pct, _ := strconv.Atoi(match[1])
	return clamp(float64(pct) / 100), match[2] == "off", nil
}

// parseOSAScript reads the AppleScript record of `get volume settings`, such
// as "output volume:40, input volume:75, alert volume:100, output muted:false".
// An output device without a volume control reports "missing value".
func parseOSAScript(out string) (level float64, muted bool, err error) {
	var haveLevel, haveMuted bool
	for field := range strings.SplitSeq(strings.TrimSpace(out), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), ":")
		if !ok {
			continue
		}
		switch key {
		case "output volume":
			pct, perr := strconv.Atoi(value)
			if perr != nil {
				return 0, false, fmt.Errorf("osvolume: output volume %q: %w", value, ErrUnsupported)
			}
			level, haveLevel = clamp(float64(pct)/100), true
		case "output muted":
			muted, haveMuted = value == "true", true
		}
	}
	if !haveLevel || !haveMuted {
		return 0, false, fmt.Errorf("osvolume: unexpected osascript output %q", strings.TrimSpace(out))
	}
	return level, muted, nil
}
