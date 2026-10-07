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

// StartHotkey returns an inert watcher off Windows.
func StartHotkey(chord iface.Chord, toggle bool) (*HotkeyWatcher, error) {
	ch := make(chan iface.Event)
	close(ch)
	return &HotkeyWatcher{events: ch}, nil
}

// Events returns an already-closed channel.
func (w *HotkeyWatcher) Events() <-chan iface.Event { return w.events }

// Close is a no-op off Windows.
func (w *HotkeyWatcher) Close() error { return nil }

// ForegroundWindow is always 0 off Windows.
func ForegroundWindow() uintptr { return 0 }

// InjectText is unsupported off Windows.
func InjectText(target uintptr, text string) error { return ErrInjectRefused }

// PasteText is unsupported off Windows.
func PasteText(text string) error { return ErrInjectRefused }

// CopyText is unsupported off Windows.
func CopyText(text string) error { return ErrInjectRefused }
