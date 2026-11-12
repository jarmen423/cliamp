//go:build windows

package osvolume

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows backend drives the master volume of the default output device
// through the Core Audio IAudioEndpointVolume COM interface. It needs no
// external tool.

var (
	modole32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modole32.NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioEndpointVolume = windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

const (
	clsctxAll         = 0x17 // CLSCTX_INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER | REMOTE_SERVER
	eRender           = 0
	eConsole          = 0
	rpcEChangedMode   = 0x80010106
	sFalse            = 1
	errorNotFoundHRes = 0x80070490 // HRESULT_FROM_WIN32(ERROR_NOT_FOUND): no output device
)

// Vtable slots, counted from the start of IUnknown (QueryInterface 0,
// AddRef 1, Release 2).
const (
	vtblRelease = 2

	// IMMDeviceEnumerator
	vtblGetDefaultAudioEndpoint = 4

	// IMMDevice
	vtblActivate = 3

	// IAudioEndpointVolume
	vtblSetMasterVolumeLevelScalar = 7
	vtblGetMasterVolumeLevelScalar = 9
	vtblSetMute                    = 14
	vtblGetMute                    = 15
)

// floatArgsOK reports whether syscall.SyscallN can pass a float argument.
// On amd64 Go copies the first four arguments into XMM0 to XMM3 as the x64
// calling convention needs, and on 386 every argument goes on the stack. On
// arm64 the float registers are not set, so SetMasterVolumeLevelScalar would
// read garbage.
const floatArgsOK = runtime.GOARCH != "arm64"

// comObject is a COM interface pointer: a pointer to a pointer to its vtable.
type comObject struct {
	vtbl *[32]uintptr
}

func (o *comObject) call(slot int, args ...uintptr) error {
	all := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return hresult(hr)
}

func (o *comObject) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[vtblRelease], uintptr(unsafe.Pointer(o)))
	}
}

// hresult turns a failed HRESULT into an error. S_OK and S_FALSE succeed.
func hresult(hr uintptr) error {
	if int32(hr) >= 0 {
		return nil
	}
	if uint32(hr) == errorNotFoundHRes {
		return fmt.Errorf("osvolume: no default output device: %w", ErrUnsupported)
	}
	return fmt.Errorf("osvolume: HRESULT 0x%08X", uint32(hr))
}

// withEndpointVolume opens the IAudioEndpointVolume of the default output
// device, runs fn on it and releases everything. COM is set up per call on a
// locked OS thread, so callers may use any goroutine.
func withEndpointVolume(fn func(ev *comObject) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	var code uint32
	if errno, ok := err.(syscall.Errno); ok {
		code = uint32(errno)
	}
	switch {
	case err == nil, code == sFalse:
		// S_FALSE means COM was already up on this thread in the same
		// mode. It still counts as a reference to pair with CoUninitialize.
		defer windows.CoUninitialize()
	case code == rpcEChangedMode:
		// COM is already up on this thread in another apartment mode. It
		// works as is, and the reference is not ours to drop.
	default:
		return fmt.Errorf("osvolume: CoInitializeEx: %w", err)
	}

	var enum *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enum)),
	)
	if err := hresult(hr); err != nil {
		return fmt.Errorf("osvolume: create device enumerator: %w", err)
	}
	defer enum.release()

	var device *comObject
	if err := enum.call(vtblGetDefaultAudioEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&device))); err != nil {
		return fmt.Errorf("osvolume: default output device: %w", err)
	}
	defer device.release()

	var ev *comObject
	if err := device.call(vtblActivate, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&ev))); err != nil {
		return fmt.Errorf("osvolume: activate endpoint volume: %w", err)
	}
	defer ev.release()

	return fn(ev)
}

// available is true on every supported Windows version, since Core Audio is
// built in. get reports an error when the machine has no output device.
func available() bool { return floatArgsOK }

func get() (level float64, muted bool, err error) {
	err = withEndpointVolume(func(ev *comObject) error {
		var scalar float32
		if err := ev.call(vtblGetMasterVolumeLevelScalar, uintptr(unsafe.Pointer(&scalar))); err != nil {
			return fmt.Errorf("osvolume: read volume: %w", err)
		}
		var mute int32 // Win32 BOOL
		if err := ev.call(vtblGetMute, uintptr(unsafe.Pointer(&mute))); err != nil {
			return fmt.Errorf("osvolume: read mute: %w", err)
		}
		level, muted = float64(scalar), mute != 0
		return nil
	})
	return level, muted, err
}

func set(level float64) error {
	if !floatArgsOK {
		return ErrUnsupported
	}
	return withEndpointVolume(func(ev *comObject) error {
		bits := uintptr(math.Float32bits(float32(level)))
		if err := ev.call(vtblSetMasterVolumeLevelScalar, bits, 0); err != nil {
			return fmt.Errorf("osvolume: set volume: %w", err)
		}
		return nil
	})
}

func setMuted(muted bool) error {
	var b uintptr
	if muted {
		b = 1
	}
	return withEndpointVolume(func(ev *comObject) error {
		if err := ev.call(vtblSetMute, b, 0); err != nil {
			return fmt.Errorf("osvolume: set mute: %w", err)
		}
		return nil
	})
}
