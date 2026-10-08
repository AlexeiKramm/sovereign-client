//go:build windows

package windows

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Overlay is a hand-drawn Win32 layered window. In its default (pill) mode it is transparent,
// click-through, never activated and always on top; in interactive mode it is a focusable tool
// window for the control panel. No Go GUI toolkit can express the pill combination (see AGENTS.md),
// so it is built directly:
//
//   - WS_EX_LAYERED   the bitmap's alpha channel is honoured (UpdateLayeredWindow)
//   - WS_EX_TRANSPARENT  mouse input passes straight through to the window below (pill only)
//   - WS_EX_NOACTIVATE   showing it never steals focus from the target field (pill only)
//   - WS_EX_TOOLWINDOW   no taskbar button
//
// One dedicated OS thread owns the window and pumps its messages (a window's messages must be
// dispatched on the thread that created it); Present/Hide marshal a frame to that thread with
// PostMessage and return immediately, so the caller — the tray's client reader goroutine — never
// blocks on the UI.
type Overlay struct {
	hwnd  uintptr
	ready chan error
	done  chan struct{}

	// interactive switches the panel behaviour: focusable, clickable, not topmost, and mouse/key
	// callbacks (below).
	interactive     bool
	onDisplayChange func()
	onMouse         func(MouseEvent)
	onKey           func(vk uint32) bool
	onClose         func()

	mu      sync.Mutex
	pending *overlayFrame
	closed  bool
	lastErr error
	hitRect Rect // interactive: the clickable card; outside it the window is click-through

	// Window-thread-confined GDI state.
	wndProcCB uintptr
	memDC     uintptr
	dib       uintptr
	oldObj    uintptr
	dibBits   unsafe.Pointer
	dibW      int
	dibH      int
}

// overlayFrame is one marshalled Present/Hide request.
type overlayFrame struct {
	img  *image.RGBA
	x, y int
	hide bool
}

// StartOverlay creates the hidden pill window. onDisplayChange, if set, is called (on the window
// thread) when the display or DPI changes, so the controller can re-render and re-anchor.
func StartOverlay(onDisplayChange func()) (*Overlay, error) {
	return startLayered(&Overlay{
		ready:           make(chan error, 1),
		done:            make(chan struct{}),
		onDisplayChange: onDisplayChange,
	})
}

// StartPanel creates the hidden interactive control window.
func StartPanel(opts PanelOptions) (*Overlay, error) {
	return startLayered(&Overlay{
		ready:           make(chan error, 1),
		done:            make(chan struct{}),
		interactive:     true,
		onDisplayChange: opts.OnDisplayChange,
		onMouse:         opts.OnMouse,
		onKey:           opts.OnKey,
		onClose:         opts.OnClose,
	})
}

func startLayered(o *Overlay) (*Overlay, error) {
	go o.run()
	select {
	case err := <-o.ready:
		if err != nil {
			return nil, err
		}
	case <-o.done:
		return nil, errors.New("overlay thread exited before it was ready")
	case <-time.After(3 * time.Second):
		return nil, errors.New("overlay window did not become ready")
	}
	return o, nil
}

// SetHitRect sets the clickable region (in client coordinates). An interactive window returns
// HTTRANSPARENT for points outside it, so the transparent shadow margin does not block clicks on
// whatever is behind it.
func (o *Overlay) SetHitRect(r Rect) {
	o.mu.Lock()
	o.hitRect = r
	o.mu.Unlock()
}

// Focus brings an interactive window to the front and gives it keyboard focus, so Esc and (later)
// the hotkey-capture field work. It is a no-op for the pill.
func (o *Overlay) Focus() {
	o.mu.Lock()
	hwnd := o.hwnd
	interactive := o.interactive
	o.mu.Unlock()
	if hwnd == 0 || !interactive {
		return
	}
	procSetForegroundWindow.Call(hwnd)
}

// Move repositions an interactive window without changing its size, content or z-order.
func (o *Overlay) Move(x, y int) {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return
	}
	procSetWindowPos.Call(hwnd, 0, uintptr(int32(x)), uintptr(int32(y)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

// Present uploads img as the window content and places the window's top-left corner at screen
// position (x, y), then shows it without activating. img must be premultiplied RGBA (render.Frame
// is), which is exactly what UpdateLayeredWindow expects with AC_SRC_ALPHA.
func (o *Overlay) Present(img *image.RGBA, x, y int) {
	o.mu.Lock()
	o.pending = &overlayFrame{img: img, x: x, y: y}
	o.mu.Unlock()
	o.wake()
}

// Hide hides the window without destroying it.
func (o *Overlay) Hide() {
	o.mu.Lock()
	o.pending = &overlayFrame{hide: true}
	o.mu.Unlock()
	o.wake()
}

// Handle returns the window handle, for monitor/work-area queries.
func (o *Overlay) Handle() uintptr {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hwnd
}

// LastError returns the error from the most recent Present, or nil. Frames are applied
// asynchronously, so this is a diagnostic for the last frame the window thread processed.
func (o *Overlay) LastError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastErr
}

// Close destroys the window and waits for its thread to exit.
func (o *Overlay) Close() error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmOverlayQuit, 0, 0)
	}
	<-o.done
	return nil
}

func (o *Overlay) wake() {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmOverlayApply, 0, 0)
	}
}

func (o *Overlay) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(o.done)

	hInst, _, _ := procGetModuleHandleW.Call(0)
	// Each window gets its own class: the WndProc callback is bound to one Overlay, and a shared
	// class would dispatch the second window's messages to the first window's callback.
	class := fmt.Sprintf("%s-%d", overlayClassName, overlayClassSeq.Add(1))
	className, err := windows.UTF16PtrFromString(class)
	if err != nil {
		o.ready <- err
		return
	}
	o.wndProcCB = windows.NewCallback(o.wndProc)
	if o.wndProcCB == 0 {
		o.ready <- errors.New("NewCallback failed")
		return
	}

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   o.wndProcCB,
		hInstance:     hInst,
		hCursor:       loadArrowCursor(),
		lpszClassName: uintptr(unsafe.Pointer(className)),
	}
	if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		if !errors.Is(e, windows.ERROR_CLASS_ALREADY_EXISTS) {
			o.ready <- fmt.Errorf("RegisterClassEx: %w", e)
			return
		}
	}

	exStyle := uintptr(wsExLayered | wsExToolWindow)
	if !o.interactive {
		// The pill: transparent, never activated, always on top.
		exStyle |= wsExTransparent | wsExNoActivate | wsExTopmost
	}
	hwnd, _, e := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		0, // no title
		wsPopup,
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		o.ready <- fmt.Errorf("CreateWindowEx: %w", e)
		return
	}
	o.mu.Lock()
	o.hwnd = hwnd
	o.mu.Unlock()
	o.ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	// Teardown: the window is already destroyed (WM_DESTROY posted the quit). Release GDI objects.
	if o.oldObj != 0 && o.memDC != 0 {
		procSelectObject.Call(o.memDC, o.oldObj)
	}
	if o.dib != 0 {
		procDeleteObject.Call(o.dib)
	}
	if o.memDC != 0 {
		procDeleteDC.Call(o.memDC)
	}
}

func (o *Overlay) wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	switch uint32(message) {
	case wmOverlayApply:
		o.apply()
		return 0
	case wmDisplayChange, wmDpiChanged, wmSettingChange:
		if o.onDisplayChange != nil {
			o.onDisplayChange()
		}
		return 0
	case wmNCHitTest:
		if o.interactive {
			// WM_NCHITTEST's lParam is in SCREEN coordinates; the hit rect is client-relative, so
			// convert before comparing. Comparing screen coords directly made every point look
			// "outside", which returned HTTRANSPARENT everywhere and made the panel click-through.
			o.mu.Lock()
			hr := o.hitRect
			o.mu.Unlock()
			pt := point{X: int32(int16(lparam & 0xffff)), Y: int32(int16((lparam >> 16) & 0xffff))}
			procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			if int(pt.X) < hr.X || int(pt.Y) < hr.Y || int(pt.X) >= hr.X+hr.W || int(pt.Y) >= hr.Y+hr.H {
				return ^uintptr(0) // HTTRANSPARENT (-1)
			}
			return htClient
		}
		// The pill: belt and braces alongside WS_EX_TRANSPARENT.
		return ^uintptr(0) // HTTRANSPARENT (-1)
	case wmMouseActivate:
		// Return maNoActivate so Windows does not eat the mouse click upon activation from
		// another window. The panel activates itself on MouseDown so the first click registers.
		return maNoActivate
	case wmMouseWheel:
		if o.interactive && o.onMouse != nil {
			// WM_MOUSEWHEEL's lParam is in SCREEN coordinates (unlike the move/button messages); the
			// wheel delta is the signed high word of wParam. Convert before hit-testing.
			pt := point{X: int32(int16(lparam & 0xffff)), Y: int32(int16((lparam >> 16) & 0xffff))}
			procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			o.onMouse(MouseEvent{
				X:     int(pt.X),
				Y:     int(pt.Y),
				Kind:  MouseWheel,
				Delta: int(int16((wparam >> 16) & 0xffff)),
			})
			return 0
		}
	case wmMouseMove, wmLButtonDown, wmLButtonUp:
		if o.interactive && o.onMouse != nil {
			kind := mouseKind(uint32(message))
			// Capture the mouse for the duration of a press, so a drag keeps delivering moves even
			// when the cursor leaves the (small) window.
			switch kind {
			case MouseDown:
				procSetForegroundWindow.Call(hwnd)
				procSetCapture.Call(hwnd)
			case MouseUp:
				procReleaseCapture.Call()
			}
			o.onMouse(MouseEvent{
				X:    int(int16(lparam & 0xffff)),
				Y:    int(int16((lparam >> 16) & 0xffff)),
				Kind: kind,
			})
			return 0
		}
	case wmKeyDown, wmSysKeyDown:
		if o.interactive && o.onKey != nil && o.onKey(uint32(wparam)) {
			return 0
		}
	case wmClose:
		// The system close gesture (Alt+F4, the system menu). Let the controller decide; do NOT
		// destroy here, because Close() must be able to tear the window down deterministically.
		if o.interactive && o.onClose != nil {
			o.onClose()
			return 0
		}
	case wmOverlayQuit:
		// Close() asks for a real teardown: destroy the window so WM_DESTROY ends the message loop.
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, message, wparam, lparam)
	return r
}

func mouseKind(message uint32) MouseKind {
	switch message {
	case wmLButtonDown:
		return MouseDown
	case wmLButtonUp:
		return MouseUp
	default:
		return MouseMove
	}
}

func (o *Overlay) apply() {
	o.mu.Lock()
	fr := o.pending
	o.pending = nil
	o.mu.Unlock()
	if fr == nil {
		return
	}
	if fr.hide {
		procShowWindow.Call(o.hwnd, swHide)
		return
	}
	if err := o.upload(fr.img, fr.x, fr.y); err != nil {
		// A failed frame is not fatal: record it for diagnostics and skip it, so a transient GDI
		// hiccup cannot take down the pill.
		o.mu.Lock()
		o.lastErr = err
		o.mu.Unlock()
		return
	}
	o.mu.Lock()
	o.lastErr = nil
	o.mu.Unlock()
	if o.interactive {
		// The panel is a normal focusable window: show it activated, do not force it topmost (the
		// user can cover it, and Focus brings it forward when it is opened).
		procShowWindow.Call(o.hwnd, swShow)
		return
	}
	procShowWindow.Call(o.hwnd, swShowNoActivate)
	// Re-assert topmost without activating or moving; another always-on-top window can otherwise
	// cover the pill after it is re-shown.
	procSetWindowPos.Call(o.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// upload copies the premultiplied RGBA image into a top-down 32-bit DIB and hands it to
// UpdateLayeredWindow. The DIB is recreated only when the size changes.
func (o *Overlay) upload(img *image.RGBA, x, y int) error {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	if o.memDC == 0 {
		dc, _, e := procCreateCompatibleDC.Call(0)
		if dc == 0 {
			return fmt.Errorf("CreateCompatibleDC: %w", e)
		}
		o.memDC = dc
	}
	if o.dib == 0 || o.dibW != w || o.dibH != h {
		if o.dib != 0 {
			procSelectObject.Call(o.memDC, o.oldObj)
			procDeleteObject.Call(o.dib)
			o.dib = 0
			o.dibBits = nil
		}
		info := bitmapInfo{header: bitmapInfoHeader{
			biSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			biWidth:       int32(w),
			biHeight:      int32(-h), // negative: top-down rows, matching image.RGBA
			biPlanes:      1,
			biBitCount:    32,
			biCompression: biRGB,
		}}
		hbm, _, e := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&o.dibBits)), 0, 0)
		if hbm == 0 {
			return fmt.Errorf("CreateDIBSection: %w", e)
		}
		o.dib = hbm
		o.oldObj, _, _ = procSelectObject.Call(o.memDC, hbm)
		o.dibW, o.dibH = w, h
	}

	dst := unsafe.Slice((*byte)(o.dibBits), w*h*4)
	for row := 0; row < h; row++ {
		src := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+row) : img.PixOffset(b.Min.X, b.Min.Y+row)+w*4]
		out := dst[row*w*4 : (row+1)*w*4]
		for i := 0; i < w*4; i += 4 {
			out[i] = src[i+2]   // B
			out[i+1] = src[i+1] // G
			out[i+2] = src[i]   // R
			out[i+3] = src[i+3] // A
		}
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return errors.New("GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	ptDst := point{X: int32(x), Y: int32(y)}
	size := struct{ cx, cy int32 }{int32(w), int32(h)}
	ptSrc := point{}
	blend := blendFunction{blendOp: acSrcOver, sourceConstantAlpha: 255, alphaFormat: acSrcAlpha}
	r, _, e := procUpdateLayeredWindow.Call(
		o.hwnd,
		screenDC,
		uintptr(unsafe.Pointer(&ptDst)),
		uintptr(unsafe.Pointer(&size)),
		o.memDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ulwAlpha,
	)
	if r == 0 {
		return fmt.Errorf("UpdateLayeredWindow: %w", e)
	}
	return nil
}

// WorkArea returns the work area (the monitor minus the taskbar) of the monitor nearest hwnd, in
// physical pixels, plus that monitor's DPI. hwnd 0 falls back to the primary monitor.
func WorkArea(hwnd uintptr) (Rect, int) {
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	mi := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		// Fall back to the primary display's full size.
		w, _, _ := procGetSystemMetrics.Call(smCxScreen)
		h, _, _ := procGetSystemMetrics.Call(smCyScreen)
		return Rect{X: 0, Y: 0, W: int(w), H: int(h)}, systemDPI()
	}
	dpi := 96
	if hwnd != 0 {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			dpi = int(d)
		}
	} else {
		dpi = systemDPI()
	}
	return Rect{
		X: int(mi.rcWork.left),
		Y: int(mi.rcWork.top),
		W: int(mi.rcWork.right - mi.rcWork.left),
		H: int(mi.rcWork.bottom - mi.rcWork.top),
	}, dpi
}

// EnablePerMonitorDPI opts the process into Per-Monitor-V2 DPI awareness, so the pill is crisp on
// scaled displays and its physical size is predictable. It should be called once, early in main,
// before any window exists; a failure (e.g. awareness already set) is not fatal.
func EnablePerMonitorDPI() error {
	ctx := int32(-4) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	r, _, e := procSetProcessDpiAwarenessContext.Call(uintptr(ctx))
	if r == 0 {
		return fmt.Errorf("SetProcessDpiAwarenessContext: %w", e)
	}
	return nil
}

func systemDPI() int {
	if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
		return int(d)
	}
	return 96
}

// CursorPos returns the cursor position in physical screen pixels, used while dragging the panel.
func CursorPos() (int, int) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y)
}

func loadArrowCursor() uintptr {
	c, _, _ := procLoadCursorW.Call(0, idcArrow)
	return c
}

const overlayClassName = "SovereignDesktopOverlay"

// overlayClassSeq gives every layered window its own window class, because each class carries one
// WndProc callback bound to a single Overlay. Two windows sharing a class would dispatch the second
// window's messages to the first window's callback.
var overlayClassSeq atomic.Uint32

const (
	wsPopup         = 0x80000000
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExNoActivate  = 0x08000000
	wsExToolWindow  = 0x00000080
	wsExTopmost     = 0x00000008

	swHide           = 0
	swShow           = 5
	swShowNoActivate = 4

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// hwndTopmost is (HWND)-1.
	hwndTopmost = ^uintptr(0)

	ulwAlpha   = 0x00000002
	acSrcOver  = 0x00
	acSrcAlpha = 0x01

	biRGB = 0

	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmMouseWheel    = 0x020A
	wmMouseActivate = 0x0021
	wmDisplayChange = 0x007E
	wmNCHitTest     = 0x0084
	wmDpiChanged    = 0x02E0
	wmSettingChange = 0x001A

	wmApp          = 0x8000
	wmOverlayApply = wmApp + 1
	wmOverlayQuit  = wmApp + 2

	maActivate   = 1 // MA_ACTIVATE: activates window without discarding click
	maNoActivate = 3
	htClient     = 1

	monitorDefaultToNearest = 2

	smCxScreen = 0
	smCyScreen = 1

	idcArrow = 32512
)

// rect mirrors Win32 RECT.
type rect struct {
	left, top, right, bottom int32
}

// monitorInfo mirrors Win32 MONITORINFO.
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

// wndClassEx mirrors Win32 WNDCLASSEXW.
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
	hIconSm       uintptr
}

// bitmapInfoHeader mirrors Win32 BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// rgbQuad mirrors Win32 RGBQUAD.
type rgbQuad struct {
	blue, green, red, reserved byte
}

// bitmapInfo mirrors Win32 BITMAPINFO (header plus a one-entry colour table).
type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]rgbQuad
}

// blendFunction mirrors Win32 BLENDFUNCTION.
type blendFunction struct {
	blendOp             byte
	blendFlags          byte
	sourceConstantAlpha byte
	alphaFormat         byte
}

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProc                 = user32.NewProc("DefWindowProcW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procScreenToClient                = user32.NewProc("ScreenToClient")
	procSetCapture                    = user32.NewProc("SetCapture")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procGetDC                         = user32.NewProc("GetDC")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procUpdateLayeredWindow           = user32.NewProc("UpdateLayeredWindow")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

// IsSystemDarkTheme checks the Windows personalization settings in the registry to determine
// whether the system is currently using Dark Mode (AppsUseLightTheme == 0).
func IsSystemDarkTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true // default to dark if key does not exist or cannot be read
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return val == 0
}
