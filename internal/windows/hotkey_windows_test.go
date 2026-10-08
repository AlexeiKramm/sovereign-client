//go:build windows

package windows

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/alexeikramm/sovereign-client/internal/core/iface"
)

// TestHotkeyRoundTripViaInjectedInput proves the whole hotkey path — WH_KEYBOARD_LL install,
// message pump, hook callback, chord detection and channel delivery — by synthesizing the chord
// with SendInput (low-level hooks see injected input).
//
// It is opt-in because it types real keystrokes at the machine: run with SOVEREIGN_TEST_INPUT=1.
// The chord avoids Alt (Alt+Space pops the window menu) so it disturbs a focused app as little as
// possible.
func TestHotkeyRoundTripViaInjectedInput(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_INPUT") == "" {
		t.Skip("set SOVEREIGN_TEST_INPUT=1 to run the input-injection test")
	}
	chord, err := iface.ParseHotkey("Ctrl+Shift+F9")
	if err != nil {
		t.Fatal(err)
	}
	w, err := StartHotkey(chord, false)
	if err != nil {
		t.Fatalf("StartHotkey: %v", err)
	}
	defer w.Close()

	time.Sleep(150 * time.Millisecond) // let the hook thread start pumping

	const f9 = 0x78
	if err := send([]input{
		vkInput(vkControl, false), vkInput(vkShift, false), vkInput(f9, false),
		vkInput(f9, true), vkInput(vkShift, true), vkInput(vkControl, true),
	}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	want := []iface.Event{iface.Press, iface.Release}
	for i, exp := range want {
		select {
		case got := <-w.Events():
			if got != exp {
				t.Fatalf("event %d = %v, want %v", i, got, exp)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for event %d (%v)", i, exp)
		}
	}
}

// TestInjectTextAccepted checks that SendInput accepts our INPUT array. SendInput returns the
// number of events it inserted and fails if cbSize does not match the OS struct, so this catches a
// wrong INPUT stride — the failure mode that would otherwise be a silent no-op. It types a short
// string at the current focus, so it is opt-in like the hotkey test.
func TestInjectTextAccepted(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_INPUT") == "" {
		t.Skip("set SOVEREIGN_TEST_INPUT=1 to run the input-injection test")
	}
	if err := InjectText(0, "sx"); err != nil {
		t.Fatalf("InjectText: %v", err)
	}
}

// TestKeyWatcherConsumesEscape proves the pill's Esc dismiss is actually consumed, not merely
// observed. StartKeyWatcher prefers RegisterHotKey because the system swallows a registered hotkey
// outright; a second registration of the same key must therefore be refused, which is what this
// asserts. Opt-in because it grabs Esc globally for the moment it runs.
func TestKeyWatcherConsumesEscape(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_INPUT") == "" {
		t.Skip("set SOVEREIGN_TEST_INPUT=1 to run the input-injection test")
	}
	const vkEsc = 0x1B
	w, err := StartKeyWatcher(vkEsc)
	if err != nil {
		t.Fatalf("StartKeyWatcher: %v", err)
	}
	defer w.Close()

	if got := w.Method(); got != "hotkey" {
		t.Fatalf("Esc watcher installed via %q, not RegisterHotKey; the key would only be swallowed by the hook", got)
	}
	// The same key+modifiers are now registered; a second registration must fail with
	// ERROR_HOTKEY_ALREADY_REGISTERED. Success would mean nothing holds the key, so it is not
	// consumed and would reach the focused app.
	r, _, e := procRegisterHotKey.Call(0, uintptr(keyHotkeyID+1), modNoRepeat, vkEsc)
	if r != 0 {
		procUnregisterHotKey.Call(0, uintptr(keyHotkeyID+1))
		t.Fatal("a second RegisterHotKey for Esc succeeded: the watcher's registration is not live")
	}
	if !errors.Is(e, windows.ERROR_HOTKEY_ALREADY_REGISTERED) {
		t.Fatalf("second RegisterHotKey failed with %v, want ERROR_HOTKEY_ALREADY_REGISTERED", e)
	}
}

// TestHotkeyCaptureViaInjectedInput proves the capture path: a synthesized chord is reported with the
// key and the modifiers held at that instant. Opt-in for the same reasons as the round-trip test.
func TestHotkeyCaptureViaInjectedInput(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_INPUT") == "" {
		t.Skip("set SOVEREIGN_TEST_INPUT=1 to run the input-injection test")
	}
	w, err := StartHotkeyCapture()
	if err != nil {
		t.Fatalf("StartHotkeyCapture: %v", err)
	}
	defer w.Close()

	time.Sleep(150 * time.Millisecond)

	const f9 = 0x78
	if err := send([]input{
		vkInput(vkControl, false), vkInput(vkShift, false), vkInput(f9, false),
		vkInput(f9, true), vkInput(vkShift, true), vkInput(vkControl, true),
	}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	select {
	case c := <-w.Captured():
		if c.VK != f9 {
			t.Fatalf("captured vk %#x, want %#x", c.VK, f9)
		}
		if !c.Mods.Ctrl || !c.Mods.Shift || c.Mods.Alt || c.Mods.Win {
			t.Fatalf("captured mods %+v, want Ctrl+Shift only", c.Mods)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a captured chord")
	}
}
