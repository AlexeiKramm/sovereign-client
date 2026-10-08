//go:build windows

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/alexeikramm/sovereign-client/internal/core/iface"
)

// Captured is a chord read during hotkey capture: the non-modifier key plus the modifiers held when
// it was pressed.
type Captured struct {
	VK   uint32
	Mods iface.Modifiers
}

// HotkeyWatcher watches a global hotkey. In hold mode it installs a WH_KEYBOARD_LL hook and reports
// both edges of the chord; in toggle mode it uses RegisterHotKey and reports Press only. A single
// dedicated OS thread owns the hook/registration and runs the message pump, and the callback only
// posts to a channel — no work happens on the hook thread (AGENTS.md: a slow hook is silently
// removed by Windows).
type HotkeyWatcher struct {
	chord    iface.Chord
	register bool // true: RegisterHotKey; false: the low-level hook
	capture  bool
	// hookFallback lets a registering watcher fall back to the consuming hook if RegisterHotKey is
	// refused (e.g. another app already owns the key). It is how the pill's Esc dismiss stays a
	// control even when the bare key cannot be registered.
	hookFallback bool
	detector     *iface.Detector
	// swallowVK, when non-zero, is a single virtual key whose events the hook consumes (returns 1)
	// instead of passing on to the focused app.
	swallowVK uint32
	id        uint32

	events   chan iface.Event
	captured chan Captured
	ready    chan error
	done     chan struct{}

	registered bool // set before ready; which mechanism actually got installed
	hook       uintptr
	hookCB     uintptr
	tid        uint32
}

// StartHotkey starts watching chord. toggle selects RegisterHotKey (true) or the low-level hook
// (false). It returns once the watcher is installed, or with the installation error.
func StartHotkey(chord iface.Chord, toggle bool) (*HotkeyWatcher, error) {
	return startWatcher(&HotkeyWatcher{
		chord:    chord,
		register: toggle,
		id:       hotkeyID,
		detector: iface.NewDetector(chord),
		events:   make(chan iface.Event, 8),
		ready:    make(chan error, 1),
		done:     make(chan struct{}),
	})
}

// StartKeyWatcher watches a single key with no modifiers and consumes it, so it can act as a global
// control (the pill's Esc dismiss) rather than also reaching the focused app. RegisterHotKey is
// tried first because the system consumes a registered hotkey outright; the consuming hook is the
// fallback when the key cannot be registered.
func StartKeyWatcher(vk uint32) (*HotkeyWatcher, error) {
	chord := iface.NewChord(vk, iface.Modifiers{})
	return startWatcher(&HotkeyWatcher{
		chord:        chord,
		register:     true,
		hookFallback: true,
		id:           keyHotkeyID,
		swallowVK:    vk,
		detector:     iface.NewDetector(chord),
		events:       make(chan iface.Event, 8),
		ready:        make(chan error, 1),
		done:         make(chan struct{}),
	})
}

func startWatcher(w *HotkeyWatcher) (*HotkeyWatcher, error) {
	go w.run()
	select {
	case err := <-w.ready:
		if err != nil {
			return nil, err
		}
	case <-time.After(3 * time.Second):
		return nil, errors.New("hotkey thread did not become ready")
	}
	return w, nil
}

// Method reports which mechanism was installed: "hotkey" (RegisterHotKey, which the system
// consumes) or "hook" (the low-level hook, which swallows the key itself).
func (w *HotkeyWatcher) Method() string {
	if w.registered {
		return "hotkey"
	}
	return "hook"
}

// Events returns the chord edge channel. It is closed when the watcher stops.
func (w *HotkeyWatcher) Events() <-chan iface.Event { return w.events }

// Captured returns the capture channel (nil unless the watcher was started for capture).
func (w *HotkeyWatcher) Captured() <-chan Captured { return w.captured }

// StartHotkeyCapture installs the hook and reports the next non-modifier key press together with the
// modifiers held at that instant, so the user can bind a hotkey by pressing it.
func StartHotkeyCapture() (*HotkeyWatcher, error) {
	w := &HotkeyWatcher{
		capture:  true,
		events:   make(chan iface.Event),
		captured: make(chan Captured, 1),
		ready:    make(chan error, 1),
		done:     make(chan struct{}),
	}
	go w.run()
	select {
	case err := <-w.ready:
		if err != nil {
			return nil, err
		}
	case <-time.After(3 * time.Second):
		return nil, errors.New("hotkey capture thread did not become ready")
	}
	return w, nil
}

// Close stops the watcher and waits for its thread to exit.
func (w *HotkeyWatcher) Close() error {
	select {
	case <-w.done:
		return nil
	default:
	}
	procPostThreadMessage.Call(uintptr(w.tid), wmQuit, 0, 0)
	<-w.done
	return nil
}

func (w *HotkeyWatcher) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)
	defer close(w.events)

	tid, _, _ := procGetCurrentThreadId.Call()
	w.tid = uint32(tid)

	w.hookCB = windows.NewCallback(w.hookProc)
	if w.hookCB == 0 {
		w.ready <- errors.New("NewCallback failed")
		return
	}

	if w.register {
		mods := uintptr(registerMods(w.chord.Mods)) | modNoRepeat
		if r, _, e := procRegisterHotKey.Call(0, uintptr(w.id), mods, uintptr(w.chord.VK)); r != 0 {
			w.registered = true
		} else if !w.hookFallback {
			w.ready <- fmt.Errorf("RegisterHotKey(%s): %w", w.chord, e)
			return
		}
	}
	if !w.registered {
		if err := w.setHook(); err != nil {
			w.ready <- err
			return
		}
		// Re-assert the hook periodically: Windows can drop a low-level hook across sleep/resume.
		procSetTimer.Call(0, hotkeyTimerID, hotkeyWatchdog, 0)
	}
	w.ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		switch m.message {
		case wmHotkey:
			if w.register {
				w.emit(iface.Press)
			}
		case wmTimer:
			if !w.registered {
				w.rearm()
			}
		}
	}

	// Teardown.
	if w.registered {
		procUnregisterHotKey.Call(0, uintptr(w.id))
	} else {
		procKillTimer.Call(0, hotkeyTimerID)
		if w.hook != 0 {
			procUnhookWindowsHookEx.Call(w.hook)
		}
	}
}

func (w *HotkeyWatcher) setHook() error {
	h, _, e := procSetWindowsHookEx.Call(whKeyboardLL, w.hookCB, 0, 0)
	if h == 0 {
		return fmt.Errorf("SetWindowsHookEx(WH_KEYBOARD_LL): %w", e)
	}
	w.hook = h
	return nil
}

func (w *HotkeyWatcher) rearm() {
	if w.hook != 0 {
		procUnhookWindowsHookEx.Call(w.hook)
		w.hook = 0
	}
	_ = w.setHook()
}

// hookProc is called on the hook thread for every key event. It must be trivial.
func (w *HotkeyWatcher) hookProc(code, wparam, lparam uintptr) uintptr {
	swallow := false
	if int32(code) >= 0 {
		switch uint32(wparam) {
		case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
			kb := (*kbdllHookStruct)(uptrToPtr(lparam))
			down := uint32(wparam) == wmKeyDown || uint32(wparam) == wmSysKeyDown
			if w.swallowVK != 0 && kb.vkCode == w.swallowVK {
				swallow = true
			}
			if w.capture {
				w.captureKey(kb.vkCode, down)
			} else if ev, ok := w.detector.Key(kb.vkCode, down, currentModifiers()); ok {
				w.emit(ev)
			}
		}
	}
	if swallow {
		return 1 // consumed: do not pass this key to the rest of the chain / the focused app
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

func (w *HotkeyWatcher) captureKey(vk uint32, down bool) {
	if !down || iface.IsModifierVK(vk) {
		return
	}
	select {
	case w.captured <- Captured{VK: vk, Mods: currentModifiers()}:
	default:
	}
}

// emit never blocks: dropping an edge is preferable to stalling the hook thread.
func (w *HotkeyWatcher) emit(ev iface.Event) {
	select {
	case w.events <- ev:
	default:
	}
}

func currentModifiers() iface.Modifiers {
	return iface.Modifiers{
		Ctrl:  keyDown(vkControl),
		Alt:   keyDown(vkMenu),
		Shift: keyDown(vkShift),
		Win:   keyDown(vkLWin) || keyDown(vkRWin),
	}
}

func keyDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return int16(r) < 0
}

func registerMods(m iface.Modifiers) uint32 {
	var v uint32
	if m.Alt {
		v |= modAlt
	}
	if m.Ctrl {
		v |= modControl
	}
	if m.Shift {
		v |= modShift
	}
	if m.Win {
		v |= modWin
	}
	return v
}
