package osvolume

import (
	"os/exec"
	"strconv"
	"sync"
)

// macOS drives the volume with AppleScript through osascript, so the build
// needs no cgo.

var hasOSAScript = sync.OnceValue(func() bool {
	_, err := exec.LookPath("osascript")
	return err == nil
})

func available() bool { return hasOSAScript() }

func get() (float64, bool, error) {
	out, err := run("osascript", "-e", "get volume settings")
	if err != nil {
		return 0, false, err
	}
	return parseOSAScript(out)
}

func set(level float64) error {
	_, err := run("osascript", "-e", "set volume output volume "+strconv.Itoa(wholePercent(level)))
	return err
}

func setMuted(muted bool) error {
	_, err := run("osascript", "-e", "set volume output muted "+strconv.FormatBool(muted))
	return err
}
