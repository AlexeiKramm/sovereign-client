package ui

import (
	"math"

	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
	"github.com/alexeikramm/sovereign-desktop/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-desktop/internal/windows"
)

// pillView is the overlay's current content, guarded by app.pillMu. shown is the controller's own
// state, separate from the window's visibility: the window is created once and shown/hidden.
type pillView struct {
	status      string
	committed   string
	provisional string
	err         bool
	shown       bool
}

// pillMargin is the logical-pixel gap between the card and the work-area edge.
const pillMargin = 24.0

// escVK is the Windows virtual-key code for Esc, the pill's dismiss key. The overlay is
// WS_EX_NOACTIVATE, so it can never receive the key itself; Esc is caught globally and swallowed
// while the pill is up, exactly like the dictation hotkey.
const escVK = 0x1B

// startPill shows the overlay for a dictation that has just been accepted. It is called from the
// client's reader goroutine, so it only touches the pill mutex and posts a frame.
func (a *app) startPill() {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	a.pill = pillView{status: "Listening…", shown: true}
	a.pillMu.Unlock()
	a.watchEsc()
	a.repaintPill()
}

// hidePill takes the overlay down and stops the Esc watcher. It is idempotent.
func (a *app) hidePill() {
	a.pillMu.Lock()
	a.pill.shown = false
	ov := a.overlay
	w := a.escWatcher
	a.escWatcher = nil
	a.pillMu.Unlock()
	if ov != nil {
		ov.Hide()
	}
	if w != nil {
		_ = w.Close()
	}
}

// closePill tears the overlay down for good, on quit.
func (a *app) closePill() {
	a.hidePill()
	a.pillMu.Lock()
	ov := a.overlay
	a.overlay = nil
	a.pillMu.Unlock()
	if ov != nil {
		_ = ov.Close()
	}
}

// ensureOverlay creates the layered window on first use. A failure is logged and tolerated: the
// tray still dictates, it just has no pill. It returns nil when the overlay is unavailable.
func (a *app) ensureOverlay() *winplat.Overlay {
	a.overlayOnce.Do(func() {
		o, err := winplat.StartOverlay(func() { go a.repaintPill() })
		if err != nil {
			a.logf("pill overlay unavailable: %v", err)
			return
		}
		a.pillMu.Lock()
		a.overlay = o
		a.pillMu.Unlock()
		a.logf("pill overlay ready")
	})
	a.pillMu.Lock()
	defer a.pillMu.Unlock()
	return a.overlay
}

// setPillStatus updates the header line ("Listening…", "Finishing…", …).
func (a *app) setPillStatus(status string) {
	a.updatePill(func(v *pillView) { v.status = status })
}

// setPillCommitted replaces the settled text (COMMIT_TEXT is a full snapshot, never an append).
func (a *app) setPillCommitted(text string) {
	a.updatePill(func(v *pillView) { v.committed = text })
}

// setPillProvisional replaces the live tail (PROVISIONAL_TEXT is the unit in flight, not cumulative).
func (a *app) setPillProvisional(text string) {
	a.updatePill(func(v *pillView) { v.provisional = text })
}

// setPillError switches the header to the error treatment.
func (a *app) setPillError(err bool) {
	a.updatePill(func(v *pillView) { v.err = err })
}

// updatePill applies fn to the view and repaints, but only while the pill is showing, so late
// frames from a session that already ended cannot resurrect it.
func (a *app) updatePill(fn func(*pillView)) {
	a.pillMu.Lock()
	if !a.pill.shown {
		a.pillMu.Unlock()
		return
	}
	fn(&a.pill)
	a.pillMu.Unlock()
	a.repaintPill()
}

// repaintPill renders the current view and hands the bitmap to the window. The work area and DPI
// are read per frame so a display change or a drag to another monitor re-anchors correctly.
func (a *app) repaintPill() {
	a.pillMu.Lock()
	view := a.pill
	ov := a.overlay
	a.pillMu.Unlock()
	if ov == nil || !view.shown {
		return
	}

	a.mu.Lock()
	target := a.target
	pos := a.settings.OverlayPosition
	a.mu.Unlock()

	work, dpi := winplat.WorkArea(target)
	scale := float64(dpi) / 96.0
	frame, err := render.Render(render.Model{
		Status:      view.status,
		Committed:   view.committed,
		Provisional: view.provisional,
		Error:       view.err,
		Dark:        true,
		Scale:       scale,
	})
	if err != nil {
		a.logf("pill render failed: %v", err)
		return
	}
	cw, ch := frame.Size()
	x, y := anchorPos(work, cw, ch, pos, dpi)
	ov.Present(frame.Img, x-frame.Pad, y-frame.Pad)
}

// anchorPos places the card's top-left corner for the configured overlay position.
func anchorPos(work winplat.Rect, cw, ch int, pos string, dpi int) (int, int) {
	m := int(math.Round(pillMargin * float64(dpi) / 96.0))
	switch pos {
	case PosBottomLeft:
		return work.X + m, work.Y + work.H - m - ch
	case PosTopRight:
		return work.X + work.W - m - cw, work.Y + m
	case PosTopLeft:
		return work.X + m, work.Y + m
	default: // PosBottomRight
		return work.X + work.W - m - cw, work.Y + work.H - m - ch
	}
}

// watchEsc installs the global Esc watcher while the pill is up. It reuses the hotkey watcher's
// low-level hook, in its single-key, swallowing mode: Esc dismisses the pill without also reaching
// the focused app.
func (a *app) watchEsc() {
	a.pillMu.Lock()
	if a.escWatcher != nil {
		a.pillMu.Unlock()
		return
	}
	a.pillMu.Unlock()

	w, err := winplat.StartKeyWatcher(escVK)
	if err != nil {
		a.logf("esc watcher unavailable: %v", err)
		return
	}
	a.pillMu.Lock()
	a.escWatcher = w
	a.pillMu.Unlock()

	go func() {
		for ev := range w.Events() {
			if ev == iface.Press {
				a.cancelDictation()
			}
		}
	}()
}

// cancelDictation discards the active session (no UTTERANCE_DONE, so nothing is injected); the
// phone answers SESSION_RELEASED, which hides the pill.
func (a *app) cancelDictation() {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return
	}
	if err := c.CancelDictation(a.ctx); err != nil {
		a.logf("cancel dictation: %v", err)
	}
}
