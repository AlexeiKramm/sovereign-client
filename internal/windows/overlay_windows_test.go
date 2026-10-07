//go:build windows

package windows

import (
	"image"
	"os"
	"testing"
	"time"
	"unsafe"
)

var procIsWindowVisibleTest = user32.NewProc("IsWindowVisible")

// TestOverlayStructLayouts pins the Win32 struct sizes the layered window depends on. A wrong
// BITMAPINFOHEADER or BLENDFUNCTION size makes UpdateLayeredWindow fail or read garbage; a wrong
// WNDCLASSEXW/MONITORINFO size corrupts registration and monitor lookup.
func TestOverlayStructLayouts(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"BITMAPINFOHEADER", unsafe.Sizeof(bitmapInfoHeader{}), 40},
		{"BITMAPINFO", unsafe.Sizeof(bitmapInfo{}), 44},
		{"RGBQUAD", unsafe.Sizeof(rgbQuad{}), 4},
		{"BLENDFUNCTION", unsafe.Sizeof(blendFunction{}), 4},
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MONITORINFO", unsafe.Sizeof(monitorInfo{}), 40},
		{"RECT", unsafe.Sizeof(rect{}), 16},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestOverlayPresentAndHide drives the real layered window: create it, upload a bitmap, confirm the
// window is visible and UpdateLayeredWindow accepted the DIB, resize (which recreates the DIB), and
// hide. It needs an interactive desktop session, so it is opt-in: SOVEREIGN_TEST_OVERLAY=1.
func TestOverlayPresentAndHide(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_OVERLAY") == "" {
		t.Skip("set SOVEREIGN_TEST_OVERLAY=1 to run the layered-window test")
	}
	o, err := StartOverlay(nil)
	if err != nil {
		t.Fatalf("StartOverlay: %v", err)
	}
	defer o.Close()

	// A translucent frame exercises the premultiplied-alpha path, not just an opaque fill.
	img := image.NewRGBA(image.Rect(0, 0, 200, 60))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0x40, 0x20, 0x60, 0xC0
	}
	o.Present(img, 40, 40)
	time.Sleep(400 * time.Millisecond)
	if err := o.LastError(); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if r, _, _ := procIsWindowVisibleTest.Call(o.hwnd); r == 0 {
		t.Fatal("overlay window is not visible after Present")
	}

	// A different size forces the DIB to be recreated; a stale DIB would fail here.
	big := image.NewRGBA(image.Rect(0, 0, 320, 120))
	for i := 3; i < len(big.Pix); i += 4 {
		big.Pix[i] = 0xFF
	}
	o.Present(big, 40, 40)
	time.Sleep(300 * time.Millisecond)
	if err := o.LastError(); err != nil {
		t.Fatalf("Present(resize): %v", err)
	}

	o.Hide()
	time.Sleep(150 * time.Millisecond)
	if r, _, _ := procIsWindowVisibleTest.Call(o.hwnd); r != 0 {
		t.Fatal("overlay window is still visible after Hide")
	}
}

// TestPanelInteracts drives the interactive window: it is created focusable, presents a frame,
// reports it visible, and delivers posted mouse and key messages to the callbacks the controller
// depends on (click-to-close, drag, Esc). Opt-in like the overlay test: SOVEREIGN_TEST_OVERLAY=1.
//
// Because TestOverlayPresentAndHide runs first in the same process, this also guards the requirement
// that every layered window has its own window class: a shared class dispatches the second window's
// messages to the first window's WndProc callback, so the panel would never paint or show.
func TestPanelInteracts(t *testing.T) {
	if os.Getenv("SOVEREIGN_TEST_OVERLAY") == "" {
		t.Skip("set SOVEREIGN_TEST_OVERLAY=1 to run the layered-window test")
	}
	mouse := make(chan MouseEvent, 4)
	keys := make(chan uint32, 4)
	o, err := StartPanel(PanelOptions{
		OnMouse: func(ev MouseEvent) {
			select {
			case mouse <- ev:
			default:
			}
		},
		OnKey: func(vk uint32) bool {
			select {
			case keys <- vk:
			default:
			}
			return vk == 0x1B
		},
	})
	if err != nil {
		t.Fatalf("StartPanel: %v", err)
	}
	defer o.Close()

	img := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xFF
	}
	o.SetHitRect(Rect{X: 0, Y: 0, W: 200, H: 120})
	o.Present(img, 60, 60)
	time.Sleep(300 * time.Millisecond)
	if err := o.LastError(); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if r, _, _ := procIsWindowVisibleTest.Call(o.hwnd); r == 0 {
		t.Fatal("panel window is not visible after Present")
	}

	// Post a left-button-down at client (30, 20): the window must decode the packed lParam.
	lparam := uintptr(30) | uintptr(20)<<16
	procPostMessageW.Call(o.hwnd, wmLButtonDown, 0, lparam)
	select {
	case ev := <-mouse:
		if ev.Kind != MouseDown || ev.X != 30 || ev.Y != 20 {
			t.Fatalf("mouse event = %+v, want MouseDown at (30,20)", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no mouse event reached the callback")
	}

	procPostMessageW.Call(o.hwnd, wmKeyDown, 0x1B, 0)
	select {
	case vk := <-keys:
		if vk != 0x1B {
			t.Fatalf("key event = %#x, want Esc", vk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no key event reached the callback")
	}

	o.Hide()
	time.Sleep(150 * time.Millisecond)
	if r, _, _ := procIsWindowVisibleTest.Call(o.hwnd); r != 0 {
		t.Fatal("panel window is still visible after Hide")
	}
}
