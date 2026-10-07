//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SingleInstanceMutexName is the named mutex that enforces one tray instance per user session.
const SingleInstanceMutexName = `Local\SovereignDesktop.SingleInstance`

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

// AcquireSingleInstance creates or opens the named mutex. It reports alreadyRunning when another
// process in this session holds the same name, so the caller can refuse to start a second tray
// (which would otherwise show duplicate icons and overlays). release closes the handle; it is a
// no-op when alreadyRunning is true, because the caller must not disturb the live instance.
func AcquireSingleInstance(name string) (alreadyRunning bool, release func(), err error) {
	release = func() {}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, release, err
	}
	// Call the syscall directly: LazyProc.Call captures GetLastError atomically, which is how
	// ERROR_ALREADY_EXISTS is distinguished from a fresh handle.
	h, _, e := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return false, release, fmt.Errorf("create mutex %q: %w", name, e)
	}
	if errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(windows.Handle(h))
		return true, release, nil
	}
	return false, func() { _ = windows.CloseHandle(windows.Handle(h)) }, nil
}
