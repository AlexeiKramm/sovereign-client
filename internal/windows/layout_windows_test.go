//go:build windows

package windows

import (
	"testing"
	"unsafe"

	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
)

// TestStructLayouts pins the Win32 struct sizes the code relies on. SendInput reads a contiguous
// array of INPUT at sizeof(INPUT) stride, so a wrong size silently corrupts injection; GetMessage
// writes sizeof(MSG) bytes into our buffer.
func TestStructLayouts(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"INPUT", unsafe.Sizeof(input{}), 40},
		{"KEYBDINPUT", unsafe.Sizeof(keyboardInput{}), 24},
		{"MSG", unsafe.Sizeof(msg{}), 48},
		{"KBDLLHOOKSTRUCT", unsafe.Sizeof(kbdllHookStruct{}), 24},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestRegisterMods(t *testing.T) {
	all := iface.Modifiers{Ctrl: true, Alt: true, Shift: true, Win: true}
	if got := registerMods(all); got != modAlt|modControl|modShift|modWin {
		t.Errorf("registerMods(all) = %#x, want %#x", got, modAlt|modControl|modShift|modWin)
	}
	if got := registerMods(iface.Modifiers{Ctrl: true}); got != modControl {
		t.Errorf("registerMods(ctrl) = %#x, want %#x", got, modControl)
	}
}
