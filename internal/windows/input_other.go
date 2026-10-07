//go:build !windows

package windows

import (
	"errors"

	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
)

var (
	// ErrFocusChanged mirrors the Windows implementation off-platform.
	ErrFocusChanged = errors.New("focus changed since dictation started")
	// ErrInjectRefused is returned off Windows: injection is a Windows-only feature for v1.
	ErrInjectRefused = errors.New("text injection is not implemented on this platform")
)

// HotkeyWatcher is a stub off Windows; its event channel is closed immediately so a consumer's
// range terminates and no hotkeys are ever delivered.
type HotkeyWatcher struct{ events chan iface.Event }

// Captured mirrors the Windows capture result off-platform.
type Captured struct {
	VK   uint32
	Mods iface.Modifiers
}

// StartHotkey returns an inert watcher off Windows.
func StartHotkey(chord iface.Chord, toggle bool) (*HotkeyWatcher, error) {
	ch := make(chan iface.Event)
	close(ch)
	return &HotkeyWatcher{events: ch}, nil
}

// StartHotkeyCapture returns an inert capture watcher off Windows.
func StartHotkeyCapture() (*HotkeyWatcher, error) {
	ch := make(chan iface.Event)
	close(ch)
	return &HotkeyWatcher{events: ch}, nil
}

// StartKeyWatcher returns an inert single-key watcher off Windows.
func StartKeyWatcher(vk uint32) (*HotkeyWatcher, error) {
	ch := make(chan iface.Event)
	close(ch)
	return &HotkeyWatcher{events: ch}, nil
}

// Events returns an already-closed channel.
func (w *HotkeyWatcher) Events() <-chan iface.Event { return w.events }

// Captured returns a nil channel off Windows.
func (w *HotkeyWatcher) Captured() <-chan Captured { return nil }

// Close is a no-op off Windows.
func (w *HotkeyWatcher) Close() error { return nil }

// Method reports the mechanism off Windows.
func (w *HotkeyWatcher) Method() string { return "stub" }

// ForegroundWindow is always 0 off Windows.
func ForegroundWindow() uintptr { return 0 }

// InjectText is unsupported off Windows.
func InjectText(target uintptr, text string) error { return ErrInjectRefused }

// PasteText is unsupported off Windows.
func PasteText(text string) error { return ErrInjectRefused }

// CopyText is unsupported off Windows.
func CopyText(text string) error { return ErrInjectRefused }
