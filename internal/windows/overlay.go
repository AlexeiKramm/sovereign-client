package windows

// Rect is a screen rectangle in physical pixels. It is platform-neutral so the overlay controller
// can compute its anchor without importing Win32 types.
type Rect struct {
	X, Y, W, H int
}
