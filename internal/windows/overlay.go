package windows

// Rect is a screen rectangle in physical pixels. It is platform-neutral so the overlay controller
// can compute its anchor without importing Win32 types.
type Rect struct {
	X, Y, W, H int
}

// MouseKind is the kind of pointer event a panel receives.
type MouseKind int

const (
	// MouseMove is a pointer move over the panel.
	MouseMove MouseKind = iota
	// MouseDown is a left-button press.
	MouseDown
	// MouseUp is a left-button release.
	MouseUp
	// MouseWheel is a wheel notch; Delta is the wheel delta (positive = away from the user, i.e. up).
	MouseWheel
)

// MouseEvent is a pointer event in the panel's client coordinates (physical pixels), measured from
// the window's top-left corner — which includes the transparent shadow margin, so the controller
// hit-tests against the same rects it drew.
type MouseEvent struct {
	X, Y int
	Kind MouseKind
	// Delta is the wheel delta for MouseWheel (multiples of 120), zero otherwise.
	Delta int
}

// PanelOptions configures the interactive control window (Chunk 4d). Unlike the pill it is
// focusable and clickable: it takes keyboard input (Esc closes it) and pointer events for its rows
// and buttons.
type PanelOptions struct {
	// OnMouse receives pointer events; it runs on the window thread and must not block.
	OnMouse func(MouseEvent)
	// OnKey receives key-down virtual-key codes; return true to swallow the key.
	OnKey func(vk uint32) bool
	// OnClose is called when the user asks to close the window (the system close gesture). The
	// controller decides whether to hide or destroy it.
	OnClose func()
	// OnDisplayChange is called when the display or DPI changes.
	OnDisplayChange func()
}
