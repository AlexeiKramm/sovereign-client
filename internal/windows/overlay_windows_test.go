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
