//go:build !windows

package windows

import "image"

// Overlay is an inert stub off Windows: the pill overlay is a Windows-only v1 feature, and this
// keeps the tree cross-compiling for the future Linux/macOS port.
type Overlay struct{}

// StartOverlay returns an inert overlay off Windows.
func StartOverlay(onDisplayChange func()) (*Overlay, error) { return &Overlay{}, nil }

// StartPanel returns an inert panel off Windows.
func StartPanel(opts PanelOptions) (*Overlay, error) { return &Overlay{}, nil }

// SetHitRect is a no-op off Windows.
func (o *Overlay) SetHitRect(r Rect) {}

// Focus is a no-op off Windows.
func (o *Overlay) Focus() {}

// Move is a no-op off Windows.
func (o *Overlay) Move(x, y int) {}

// Handle is 0 off Windows.
func (o *Overlay) Handle() uintptr { return 0 }

// CursorPos is (0,0) off Windows.
func CursorPos() (int, int) { return 0, 0 }

// Present is a no-op off Windows.
func (o *Overlay) Present(img *image.RGBA, x, y int) {}

// Hide is a no-op off Windows.
func (o *Overlay) Hide() {}

// Close is a no-op off Windows.
func (o *Overlay) Close() error { return nil }

// LastError is always nil off Windows.
func (o *Overlay) LastError() error { return nil }

// WorkArea returns an empty rectangle and a default DPI off Windows.
func WorkArea(hwnd uintptr) (Rect, int) { return Rect{}, 96 }

// EnablePerMonitorDPI is a no-op off Windows.
func EnablePerMonitorDPI() error { return nil }
