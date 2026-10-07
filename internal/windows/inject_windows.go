//go:build windows

package windows

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
)

var (
	// ErrFocusChanged means the foreground window moved since dictation started, so injecting
	// would land in the wrong place; nothing is typed.
	ErrFocusChanged = errors.New("focus changed since dictation started")
	// ErrInjectRefused means SendInput refused the keystrokes — typically UIPI into an elevated
	// window. The text is not typed.
	ErrInjectRefused = errors.New("SendInput refused the keystrokes")
)

// ForegroundWindow returns the current foreground window handle (0 if none).
func ForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

// InjectText types text at the current focus, but only while target is still the foreground window
// (target 0 skips the check). It uses KEYEVENTF_UNICODE, so no keyboard layout is involved and a
// non-BMP rune is sent as its surrogate pair.
func InjectText(target uintptr, text string) error {
	if target != 0 && ForegroundWindow() != target {
		return ErrFocusChanged
	}
	units := iface.UTF16(text)
	if len(units) == 0 {
		return nil
	}
	inputs := make([]input, 0, len(units)*2)
	for _, u := range units {
		inputs = append(inputs, unicodeInput(u, 0))
		inputs = append(inputs, unicodeInput(u, keyeventfKeyUp))
	}
	return send(inputs)
}

// PasteText is the opt-in fallback for a refused injection: put text on the clipboard, press
// Ctrl+V, then restore the previous clipboard. Restoration is best-effort and text-only.
func PasteText(text string) error {
	prev, hadPrev := readClipboardText()
	if err := writeClipboardText(text); err != nil {
		return err
	}
	if err := sendCtrlV(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if hadPrev {
		_ = writeClipboardText(prev)
	}
	return nil
}

// CopyText leaves text on the clipboard without typing it, so the user can paste manually.
func CopyText(text string) error { return writeClipboardText(text) }

func unicodeInput(scan uint16, extraFlags uint32) input {
	return input{
		inputType: inputKeyboard,
		u:         inputUnion{ki: keyboardInput{wScan: scan, dwFlags: keyeventfUnicode | extraFlags}},
	}
}

func vkInput(vk uint16, up bool) input {
	ki := keyboardInput{wVk: vk}
	if up {
		ki.dwFlags = keyeventfKeyUp
	}
	return input{inputType: inputKeyboard, u: inputUnion{ki: ki}}
}

func sendCtrlV() error {
	inputs := []input{vkInput(vkControl, false), vkInput('V', false), vkInput('V', true), vkInput(vkControl, true)}
	return send(inputs)
}

func send(inputs []input) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, _ := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(input{}))
	if int(n) != len(inputs) {
		return ErrInjectRefused
	}
	return nil
}

func writeClipboardText(s string) error {
	units := append(iface.UTF16(s), 0) // NUL-terminated
	h, _, e := procGlobalAlloc.Call(gmemMoveable, uintptr(len(units)*2))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %w", e)
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return errors.New("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(uptrToPtr(p)), len(units)), units)
	procGlobalUnlock.Call(h)

	if !openClipboard() {
		procGlobalFree.Call(h)
		return errors.New("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if r, _, e := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %w", e)
	}
	return nil // the clipboard now owns h
}

func readClipboardText() (string, bool) {
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText); r == 0 {
		return "", false
	}
	if !openClipboard() {
		return "", false
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)

	base := (*uint16)(uptrToPtr(p))
	n := 0
	for n < 1<<20 {
		if *(*uint16)(unsafe.Add(unsafe.Pointer(base), n*2)) == 0 {
			break
		}
		n++
	}
	return string(utf16.Decode(unsafe.Slice(base, n))), true
}

func openClipboard() bool {
	for i := 0; i < 3; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(30 * time.Millisecond)
	}
	return false
}
