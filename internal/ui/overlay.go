package ui

import (
	"bytes"
	"image"
	"math"
	"time"

	"github.com/alexeikramm/sovereign-client/internal/core"
	"github.com/alexeikramm/sovereign-client/internal/core/iface"
	"github.com/alexeikramm/sovereign-client/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-client/internal/windows"
)

// pillView is the overlay's current content, guarded by app.pillMu. shown is the controller's own
// state, separate from the window's visibility: the window is created once and shown/hidden.
type pillView struct {
	status        string
	committed     string
	provisional   string
	insertedWords int
	err           bool
	shown         bool
	processing    bool // showing the three-dot "Finishing…" mark
	dotPhase      int  // active processing dot (0..2), advanced by the overlay's timer
}

// pillMargin is the logical-pixel gap between the card and the work-area edge.
const pillMargin = 24.0

// processingDotStep is how long each processing dot stays active, matching the phone's CleaningDots.
const processingDotStep = 420 * time.Millisecond

// Animation timings, chosen from docs/pill-animation-lab.html: a snappy fade-in that leads with the
// content and a gentler fade-out (Balanced), and a short in-place crossfade between discrete states.
// All three animate on a ~16 ms step.
const (
	fadeInDuration    = 120 * time.Millisecond
	fadeOutDuration   = 220 * time.Millisecond
	crossfadeDuration = 120 * time.Millisecond
	fadeStep          = 16 * time.Millisecond
)

// escVK is the Windows virtual-key code for Esc, the pill's dismiss key. The overlay is
// WS_EX_NOACTIVATE, so it can never receive the key itself; Esc is caught globally and swallowed
// while the pill is up, exactly like the dictation hotkey.
const escVK = 0x1B

// startPill shows the overlay for a dictation. It is called the instant a dictation is requested
// over a live link — before the phone acknowledges the session — so the pill appears without
// waiting a network round-trip; the OnStarted/OnState frames only refine its status afterwards. It
// may also be called again on acceptance, which is harmless. It runs on the caller's goroutine, so
// it only touches the pill mutex and posts a frame.
func (a *app) startPill() {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	a.pill = pillView{status: "Listening…", shown: true}
	a.pillMu.Unlock()
	a.watchEsc()
	a.ensurePillVisible()
}

// showConnectingPill shows a neutral, persistent status while the on-demand discovery ladder runs.
// Unlike showErrorPill it never auto-dismisses: it is replaced by startPill when the session
// starts, or by showErrorPill if discovery fails. Esc dismisses it.
func (a *app) showConnectingPill(status string) {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	a.pill = pillView{status: status, shown: true}
	a.pillMu.Unlock()
	a.watchEsc()
	a.ensurePillVisible()
}

// showInsertedPill displays the success feedback state ("Inserted X words") in-place after utterance commit
// and injection, preserving the card geometry and dimming the final text, then automatically dismisses according to settings.FeedbackDurationSec.
func (a *app) showInsertedPill(words int, delivery string) {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	w := a.escWatcher
	a.escWatcher = nil
	if delivery != "" {
		a.pill.committed = delivery
	}
	a.pill.provisional = ""
	a.pill.insertedWords = words
	a.pill.processing = false
	a.pill.dotPhase = 0
	a.pill.shown = true
	a.pillMu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	a.ensurePillVisible()

	a.mu.Lock()
	dur := a.settings.FeedbackDurationSec
	a.mu.Unlock()
	if dur <= 0 {
		a.hidePill()
		return
	}

	time.AfterFunc(time.Duration(dur)*time.Second, func() {
		a.pillMu.Lock()
		stale := a.pill.shown && a.pill.insertedWords == words
		a.pillMu.Unlock()
		if stale {
			a.hidePill()
		}
	})
}

// hideListeningPill takes the overlay down only if it is NOT currently showing feedback (inserted capsule or error).
func (a *app) hideListeningPill() {
	a.pillMu.Lock()
	if a.pill.insertedWords > 0 || a.pill.err {
		a.pillMu.Unlock()
		return
	}
	a.pillMu.Unlock()
	a.hidePill()
}

// showErrorPill shows the pill in error state with a failure description (e.g. "Phone offline",
// "Phone busy", etc.), then automatically auto-dismisses after the feedback interval.
func (a *app) showErrorPill(msg string) {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	a.pill = pillView{status: msg, shown: true, err: true}
	a.pillMu.Unlock()
	a.ensurePillVisible()

	a.mu.Lock()
	dur := a.settings.FeedbackDurationSec
	if dur < 2 {
		dur = 2 // give at least 2 seconds for readable error feedback
	}
	a.mu.Unlock()

	time.AfterFunc(time.Duration(dur)*time.Second, func() {
		a.pillMu.Lock()
		stale := a.pill.shown && a.pill.err && a.pill.status == msg
		a.pillMu.Unlock()
		if stale {
			a.hidePill()
		}
	})
}

// showBusyPill delegates to showErrorPill.
func (a *app) showBusyPill(msg string) {
	a.showErrorPill(msg)
}

// hidePill starts the fade-out and stops the Esc watcher. It is idempotent.
func (a *app) hidePill() {
	a.pillMu.Lock()
	a.pill.shown = false
	a.pill.processing = false
	w := a.escWatcher
	a.escWatcher = nil
	a.pillMu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	a.fadeOutPill()
}

// closePill tears the overlay down for good, on quit. It hides immediately rather than fading, then
// destroys the window.
func (a *app) closePill() {
	a.pillMu.Lock()
	a.pill.shown = false
	a.pill.processing = false
	a.pillAlpha = 0
	a.fadeGen++
	a.xfadeGen++
	w := a.escWatcher
	a.escWatcher = nil
	ov := a.overlay
	a.overlay = nil
	a.pillMu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	if ov != nil {
		ov.Hide()
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

// setPillStatus updates the header line ("Listening…", "Finishing…", …) with a crossfade, because it
// is a discrete state change rather than streaming text.
func (a *app) setPillStatus(status string) {
	a.updatePillTransition(func(v *pillView) {
		if v.insertedWords == 0 && !v.err {
			v.status = status
		}
	})
}

// updatePill applies fn to the view and repaints instantly, but only while the pill is showing, so
// late frames from a session that already ended cannot resurrect it. Streaming text uses this.
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

// updatePillTransition is updatePill for a discrete state change, crossfading instead of snapping.
func (a *app) updatePillTransition(fn func(*pillView)) {
	a.pillMu.Lock()
	if !a.pill.shown {
		a.pillMu.Unlock()
		return
	}
	fn(&a.pill)
	a.pillMu.Unlock()
	a.crossfadePill()
}

// startProcessingPill switches the pill to its instant "Finishing…" phase and starts the three-dot
// cycle. It is called the moment a stop is requested — before the network round-trip — so the pill
// visibly responds to the keypress; the phone's later PROCESSING frame is then a no-op refinement.
// It is a no-op unless the pill is currently up. Idempotent.
func (a *app) startProcessingPill() {
	if a.ensureOverlay() == nil {
		return
	}
	a.pillMu.Lock()
	if !a.pill.shown {
		a.pillMu.Unlock()
		return
	}
	a.pill.processing = true
	a.pill.dotPhase = 0
	a.procGen++
	gen := a.procGen
	if a.pill.insertedWords == 0 && !a.pill.err {
		a.pill.status = "Finishing…"
	}
	a.pillMu.Unlock()
	a.crossfadePill()
	a.cyclePillDots(gen)
}

// stopProcessingPill leaves the processing phase. The dot-timer chain stops itself on its next tick
// (it re-reads the flag).
func (a *app) stopProcessingPill() {
	a.pillMu.Lock()
	a.pill.processing = false
	a.pill.dotPhase = 0
	a.pillMu.Unlock()
}

// cyclePillDots advances the active processing dot and repaints every processingDotStep, until the
// pill leaves the processing phase or a new episode begins. gen stops a stale chain from an earlier
// episode instead of letting it drive the current one.
func (a *app) cyclePillDots(gen int) {
	time.AfterFunc(processingDotStep, func() {
		a.pillMu.Lock()
		if !a.pill.processing || !a.pill.shown || a.procGen != gen {
			a.pillMu.Unlock()
			return
		}
		a.pill.dotPhase = (a.pill.dotPhase + 1) % 3
		a.pillMu.Unlock()
		a.repaintPill()
		a.cyclePillDots(gen)
	})
}

// ensurePillVisible fades the window in if it is not already fully opaque; when it is already up it
// crossfades to the (possibly) changed content. It is called whenever the pill becomes logically
// shown.
func (a *app) ensurePillVisible() {
	a.pillMu.Lock()
	a.pill.shown = true
	alpha := a.pillAlpha
	a.pillMu.Unlock()
	if alpha >= 255 {
		a.crossfadePill()
		return
	}
	a.fadePillTo(255, fadeInDuration)
}

// fadeOutPill fades the window to zero and then hides it.
func (a *app) fadeOutPill() {
	a.fadePillTo(0, fadeOutDuration)
}

// hideOverlayNow zeroes the alpha and hides the window immediately.
func (a *app) hideOverlayNow() {
	a.pillMu.Lock()
	a.pillAlpha = 0
	ov := a.overlay
	a.pillMu.Unlock()
	if ov != nil {
		ov.Hide()
	}
}

// fadePillTo animates the whole-frame alpha toward target over dur on a sequential ~16 ms ticker,
// and hides the window when it reaches zero. Sequential execution ensures earlier alpha steps can
// never execute after the final step (preventing the pill from getting stuck half-transparent).
// Every call bumps a generation, so a newer fade stops any in-flight fade.
func (a *app) fadePillTo(target int, dur time.Duration) {
	a.pillMu.Lock()
	start := a.pillAlpha
	a.fadeGen++
	a.xfadeGen++ // a fade cancels any in-flight state crossfade
	gen := a.fadeGen
	a.pillMu.Unlock()

	if dur <= 0 || start == target {
		a.pillMu.Lock()
		a.pillAlpha = target
		shown := a.pill.shown
		a.pillMu.Unlock()
		if target == 0 && !shown {
			a.hideOverlayNow()
		} else {
			a.repaintPill()
		}
		return
	}

	go func() {
		ticker := time.NewTicker(fadeStep)
		defer ticker.Stop()
		startTime := time.Now()
		for range ticker.C {
			elapsed := time.Since(startTime)
			frac := float64(elapsed) / float64(dur)
			if frac >= 1.0 {
				a.pillMu.Lock()
				if a.fadeGen != gen {
					a.pillMu.Unlock()
					return
				}
				shown := a.pill.shown
				a.pillAlpha = target
				a.pillMu.Unlock()
				if target == 0 && !shown {
					a.hideOverlayNow()
					return
				}
				a.repaintPill()
				return
			}
			val := start + int(math.Round(float64(target-start)*frac))
			a.pillMu.Lock()
			if a.fadeGen != gen {
				a.pillMu.Unlock()
				return
			}
			a.pillAlpha = val
			a.pillMu.Unlock()
			a.repaintPill()
		}
	}()
}

// repaintPill renders the current view and hands the bitmap to the window at the current alpha. The
// work area and DPI are read per frame so a display change or a drag to another monitor re-anchors
// correctly. It is the instant path: streaming text, the processing-dot tick and the fade steps.
func (a *app) repaintPill() {
	a.pillMu.Lock()
	view := a.pill
	ov := a.overlay
	alpha := a.pillAlpha
	a.pillMu.Unlock()
	if ov == nil || (!view.shown && alpha == 0) {
		return
	}
	frame, x, y, ok := a.renderPillFrame(view)
	if !ok {
		return
	}
	a.cachePillFrame(frame.Img)
	ov.PresentAlpha(frame.Img, x, y, uint8(alpha))
}

// crossfadePill renders the current view and, when it differs from the last presented frame, dissolves
// from the old to the new over crossfadeDuration on a sequential ~16 ms ticker. It is the discrete-state-change
// path (listening / warming / finishing / inserted / error), never the streaming-text or dot-tick path.
func (a *app) crossfadePill() {
	a.pillMu.Lock()
	view := a.pill
	ov := a.overlay
	alpha := a.pillAlpha
	from := a.lastPillFrame
	a.xfadeGen++
	gen := a.xfadeGen
	a.pillMu.Unlock()
	if ov == nil || !view.shown || alpha == 0 {
		return
	}
	frame, x, y, ok := a.renderPillFrame(view)
	if !ok {
		return
	}
	a.cachePillFrame(frame.Img)
	if from == nil || from.Bounds() != frame.Img.Bounds() || bytes.Equal(from.Pix, frame.Img.Pix) {
		ov.PresentAlpha(frame.Img, x, y, uint8(alpha))
		return
	}

	go func() {
		ticker := time.NewTicker(fadeStep)
		defer ticker.Stop()
		startTime := time.Now()
		for range ticker.C {
			elapsed := time.Since(startTime)
			frac := float64(elapsed) / float64(crossfadeDuration)
			if frac >= 1.0 {
				a.pillMu.Lock()
				if a.xfadeGen != gen || !a.pill.shown {
					a.pillMu.Unlock()
					return
				}
				al := a.pillAlpha
				a.pillMu.Unlock()
				if al == 0 {
					return
				}
				ov.PresentAlpha(frame.Img, x, y, uint8(al))
				return
			}
			blended := blendImages(from, frame.Img, frac)
			a.pillMu.Lock()
			if a.xfadeGen != gen || !a.pill.shown {
				a.pillMu.Unlock()
				return
			}
			al := a.pillAlpha
			a.pillMu.Unlock()
			if al == 0 {
				return
			}
			ov.PresentAlpha(blended, x, y, uint8(al))
		}
	}()
}

// renderPillFrame renders the view and returns the frame plus the window position (the card's top-left
// minus the shadow pad) for the current work area and DPI.
func (a *app) renderPillFrame(view pillView) (render.Frame, int, int, bool) {
	a.mu.Lock()
	target := a.target
	pos := a.settings.OverlayPosition
	a.mu.Unlock()

	work, dpi := winplat.WorkArea(target)
	scale := float64(dpi) / 96.0
	frame, err := render.Render(render.Model{
		Status:        view.status,
		Committed:     view.committed,
		Provisional:   view.provisional,
		InsertedWords: view.insertedWords,
		Error:         view.err,
		Processing:    view.processing,
		DotPhase:      view.dotPhase,
		Dark:          a.isDarkTheme(),
		Preset:        a.themePreset(),
		Scale:         scale,
	})
	if err != nil {
		a.logf("pill render failed: %v", err)
		return render.Frame{}, 0, 0, false
	}
	cw, ch := frame.Size()
	x, y := anchorPos(work, cw, ch, pos, dpi)
	return frame, x - frame.Pad, y - frame.Pad, true
}

// cachePillFrame remembers the last content frame so the next state change can dissolve from it.
func (a *app) cachePillFrame(img *image.RGBA) {
	a.pillMu.Lock()
	a.lastPillFrame = img
	a.pillMu.Unlock()
}

// blendImages linearly interpolates two same-sized premultiplied frames; t=0 is from, t=1 is to. The
// images are premultiplied, so interpolating the bytes directly is the correct cross-dissolve.
func blendImages(from, to *image.RGBA, t float64) *image.RGBA {
	out := image.NewRGBA(from.Bounds())
	for i := range out.Pix {
		f := float64(from.Pix[i])
		out.Pix[i] = uint8(f + (float64(to.Pix[i])-f)*t + 0.5)
	}
	return out
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
	a.logf("esc dismiss armed (%s)", w.Method())

	go func() {
		for ev := range w.Events() {
			if ev == iface.Press {
				a.logf("esc: dismissing dictation")
				a.cancelDictation()
			}
		}
	}()
}

// cancelDictation discards an active session (no UTTERANCE_DONE, so nothing is injected). The pill is
// local feedback, so it is dismissed locally (fading out) rather than waiting for the phone's
// SESSION_RELEASED — that network round-trip was the whole perceived latency of the Esc key. The
// cancelled session still ends on the wire below, and the later SESSION_RELEASED handling is
// idempotent. It also abandons a deferred dictation that is still waiting for the link, so Esc on the
// "Connecting…" pill really cancels.
func (a *app) cancelDictation() {
	a.mu.Lock()
	c := a.client
	ready := a.connState == core.StateReady
	pending := a.pending
	if pending {
		a.pending = false
		a.pendingTest = false
	}
	a.mu.Unlock()

	if pending && !ready {
		// Released before the link came up: abandon the deferred dictation and drop the pill.
		a.hidePill()
		a.signalSmartReset()
		return
	}

	// End it locally now, mirroring what OnReleased would do minus the round-trip, so Esc dismisses
	// the pill with no perceptible delay. The hide goes first so nothing (tray update included) sits
	// between the keypress and the pill leaving the screen; resetting the dictating flag and the
	// smart-mode machine then lets the very next hotkey press start a fresh dictation.
	a.hideListeningPill()
	a.setDictating(false)
	a.setState(StateReady, "")
	a.signalSmartReset()

	if c == nil {
		return
	}
	if err := c.CancelDictation(a.ctx); err != nil {
		a.logf("cancel dictation: %v", err)
	}
}
