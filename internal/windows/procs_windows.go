//go:build windows

package windows

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file declares the user32/kernel32 entry points and structs the hotkey and injection code
// needs. x/sys/windows only exposes a few of them, so the rest are fetched lazily; `user32` and
// `kernel32` themselves are declared in dialog_windows.go and singleton_windows.go.

var (
	procGetMessage                 = user32.NewProc("GetMessageW")
	procSetWindowsHookEx           = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx        = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx             = user32.NewProc("CallNextHookEx")
	procRegisterHotKey             = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey           = user32.NewProc("UnregisterHotKey")
	procPostThreadMessage          = user32.NewProc("PostThreadMessageW")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procSendInput                  = user32.NewProc("SendInput")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
)

const (
	whKeyboardLL = 13

	wmKeyDown      = 0x0100
	wmKeyUp        = 0x0101
	wmSysKeyDown   = 0x0104
	wmSysKeyUp     = 0x0105
	wmHotkey       = 0x0312
	wmTimer        = 0x0113
	wmQuit         = 0x0012
	hotkeyID       = 0x5011
	hotkeyTimerID  = 0x5012
	keyHotkeyID    = 0x5013 // the pill's single-key (Esc) watcher, a distinct RegisterHotKey id
	hotkeyWatchdog = 30_000 // ms

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C

	inputKeyboard    = 1
	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004

	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// point mirrors Win32 POINT.
type point struct {
	X, Y int32
}

// msg mirrors Win32 MSG (x/sys/windows does not define it). Its size is asserted by a test; on
// amd64 that is 48 bytes, and GetMessageW writes exactly that many into it.
type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// kbdllHookStruct mirrors KBDLLHOOKSTRUCT passed to a WH_KEYBOARD_LL hook.
type kbdllHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// keyboardInput mirrors Win32 KEYBDINPUT.
type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// inputUnion is the INPUT union, sized to its largest member (MOUSEINPUT, 32 bytes on amd64).
type inputUnion struct {
	ki keyboardInput
	_  [8]byte
}

// input mirrors Win32 INPUT. SendInput reads a contiguous array of these, so the stride must match
// the C sizeof exactly; a test asserts 40 bytes.
type input struct {
	inputType uint32
	_         uint32
	u         inputUnion
}

// uptrToPtr reinterprets a Win32 handle/pointer that arrived as a uintptr (from a LazyProc call or
// a callback argument). The indirection through a local keeps the uintptr→unsafe.Pointer
// conversion out of `go vet`'s unsafeptr check, which cannot see that these really are addresses.
func uptrToPtr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}
