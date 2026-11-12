//go:build !linux && !darwin && !windows

package osvolume

func available() bool { return false }

func get() (float64, bool, error) { return 0, false, ErrUnsupported }

func set(float64) error { return ErrUnsupported }

func setMuted(bool) error { return ErrUnsupported }
