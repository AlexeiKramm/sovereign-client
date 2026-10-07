package ui

import (
	"fmt"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
	"github.com/alexeikramm/sovereign-desktop/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-desktop/internal/windows"
)

// panelView is the control window's content. Unlike the pill it persists between dictations, so it
// is updated even while hidden and only the repaint is skipped.
type panelView struct {
	shown       bool
	status      string
	committed   string
	provisional string
	err         bool
}

// escVKPanel is Esc, which closes the panel. The panel has keyboard focus, so it receives the key
// directly — no global hook is involved.
const escVKPanel = 0x1B

// openPanel shows the control window, creating it on first use.
func (a *app) openPanel() {
	if a.ensurePanel() == nil {
		return
	}
	a.panelMu.Lock()
	a.panelContent.shown = true
	ov := a.panelWin
	a.panelMu.Unlock()
	a.repaintPanel()
	if ov != nil {
		ov.Focus()
	}
}

// closePanel hides the control window; the tray keeps running.
func (a *app) closePanel() {
	a.panelMu.Lock()
	a.panelContent.shown = false
	ov := a.panelWin
	a.panelMu.Unlock()
	if ov != nil {
		ov.Hide()
	}
}

// togglePanel opens the panel if hidden, closes it if shown.
func (a *app) togglePanel() {
	a.panelMu.Lock()
	shown := a.panelContent.shown
	a.panelMu.Unlock()
	if shown {
		a.closePanel()
	} else {
		a.openPanel()
	}
}

// closePanelWindow tears the panel down for good, on quit.
func (a *app) closePanelWindow() {
	a.closePanel()
	a.panelMu.Lock()
	ov := a.panelWin
	a.panelWin = nil
	a.panelMu.Unlock()
	if ov != nil {
		_ = ov.Close()
	}
}

// ensurePanel creates the panel window on first use. A failure is logged and tolerated.
func (a *app) ensurePanel() *winplat.Overlay {
	a.panelOnce.Do(func() {
		o, err := winplat.StartPanel(winplat.PanelOptions{
			OnMouse:         a.onPanelMouse,
			OnKey:           a.onPanelKey,
			OnClose:         a.closePanel,
			OnDisplayChange: func() { go a.repaintPanel() },
		})
		if err != nil {
			a.logf("control panel unavailable: %v", err)
			return
		}
		a.panelMu.Lock()
		a.panelWin = o
		a.panelMu.Unlock()
		a.logf("control panel ready")
	})
	a.panelMu.Lock()
	defer a.panelMu.Unlock()
	return a.panelWin
}

// setPanelStatus sets the connection line ("Ready — Phone").
func (a *app) setPanelStatus(status string) {
	a.updatePanel(func(v *panelView) { v.status = status })
}

// updatePanel applies fn to the view. The view is always kept current (so opening the panel
// mid-dictation shows the live text), but it is only repainted while shown.
func (a *app) updatePanel(fn func(*panelView)) {
	a.panelMu.Lock()
	fn(&a.panelContent)
	shown := a.panelContent.shown
	a.panelMu.Unlock()
	if shown {
		a.repaintPanel()
	}
}

// refreshPanel repaints the panel if it is open, e.g. after the latency/session counters change.
func (a *app) refreshPanel() {
	a.panelMu.Lock()
	shown := a.panelContent.shown
	a.panelMu.Unlock()
	if shown {
		a.repaintPanel()
	}
}

// repaintPanel renders the panel and hands the bitmap to the window, positioning it on first show.
func (a *app) repaintPanel() {
	a.panelMu.Lock()
	view := a.panelContent
	ov := a.panelWin
	pos := a.panelPos
	placed := a.panelPlaced
	a.panelMu.Unlock()
	if ov == nil || !view.shown {
		return
	}

	work, dpi := winplat.WorkArea(ov.Handle())
	scale := float64(dpi) / 96.0
	p, err := render.RenderPanel(render.PanelModel{
		Title:       "Sovereign",
		Status:      view.status,
		Committed:   view.committed,
		Provisional: view.provisional,
		Info:        a.panelInfo(),
		Error:       view.err,
		Dark:        true,
		Scale:       scale,
	})
	if err != nil {
		a.logf("panel render failed: %v", err)
		return
	}
	cw, ch := p.Frame.Size()
	if !placed {
		x, y := anchorPos(work, cw, ch, PosBottomRight, dpi)
		pos = [2]int{x, y}
		a.panelMu.Lock()
		a.panelPos = pos
		a.panelPlaced = true
		a.panelMu.Unlock()
	}

	a.panelMu.Lock()
	a.panelLayout = p
	a.panelMu.Unlock()

	ov.SetHitRect(winplat.Rect{X: p.Frame.Pad, Y: p.Frame.Pad, W: cw, H: ch})
	ov.Present(p.Frame.Img, pos[0]-p.Frame.Pad, pos[1]-p.Frame.Pad)
}

// panelInfo builds the footer lines: the last utterance's ASR/cleanup timings, the link RTT and the
// session count. Empty values are omitted so the footer never reserves space it does not use.
func (a *app) panelInfo() []string {
	a.mu.Lock()
	asr, cleanup := a.lastAsrMs, a.lastCleanupMs
	latency := a.lastLatency
	sessions := a.sessionCount
	a.mu.Unlock()

	var info []string
	if asr > 0 || cleanup > 0 {
		info = append(info, fmt.Sprintf("Last ASR %d ms · Cleanup %d ms", asr, cleanup))
	}
	if latency != "" {
		info = append(info, "Link "+latency)
	}
	info = append(info, fmt.Sprintf("Sessions %d", sessions))
	return info
}

// onPanelMouse handles clicks and drags. It runs on the window thread, so it only touches the panel
// mutex and posts work back to the window.
func (a *app) onPanelMouse(ev winplat.MouseEvent) {
	a.panelMu.Lock()
	layout := a.panelLayout
	ov := a.panelWin
	a.panelMu.Unlock()

	switch ev.Kind {
	case winplat.MouseDown:
		if c, ok := layout.ControlAt(ev.X, ev.Y); ok {
			if c.Kind == render.ControlClose {
				a.closePanel()
			}
			return
		}
		if layout.InHeader(ev.X, ev.Y) {
			a.panelMu.Lock()
			a.panelDragging = true
			a.panelDragX, a.panelDragY = ev.X, ev.Y
			a.panelMu.Unlock()
		}
	case winplat.MouseMove:
		a.panelMu.Lock()
		dragging := a.panelDragging
		dx, dy := a.panelDragX, a.panelDragY
		a.panelMu.Unlock()
		if !dragging || ov == nil {
			return
		}
		cx, cy := winplat.CursorPos()
		nx, ny := cx-dx, cy-dy
		a.panelMu.Lock()
		a.panelPos = [2]int{nx, ny}
		a.panelMu.Unlock()
		ov.Move(nx, ny)
	case winplat.MouseUp:
		a.panelMu.Lock()
		a.panelDragging = false
		a.panelMu.Unlock()
	}
}

// onPanelKey closes the panel on Esc. It runs on the window thread; returning true swallows the key.
func (a *app) onPanelKey(vk uint32) bool {
	if vk == escVKPanel {
		a.closePanel()
		return true
	}
	return false
}

// setLiveCommitted/provisional/error update both the pill and the panel, so the two surfaces always
// agree about the text being dictated.
func (a *app) setLiveCommitted(text string) {
	a.updatePill(func(v *pillView) { v.committed = text })
	a.updatePanel(func(v *panelView) { v.committed = text })
}

func (a *app) setLiveProvisional(text string) {
	a.updatePill(func(v *pillView) { v.provisional = text })
	a.updatePanel(func(v *panelView) { v.provisional = text })
}

func (a *app) setLiveError(err bool) {
	a.updatePill(func(v *pillView) { v.err = err })
	a.updatePanel(func(v *panelView) { v.err = err })
}

// recordCommitted keeps the last utterance's timings for the panel footer.
func (a *app) recordCommitted(perf core.CommitPerf) {
	a.mu.Lock()
	a.lastAsrMs, a.lastCleanupMs = perf.AsrMs, perf.CleanupMs
	a.mu.Unlock()
	a.refreshPanel()
}

// recordLatency keeps the last link RTT line for the panel footer.
func (a *app) recordLatency(line string) {
	a.mu.Lock()
	a.lastLatency = line
	a.mu.Unlock()
	a.refreshPanel()
}

// countSession bumps the session counter for the panel footer.
func (a *app) countSession() {
	a.mu.Lock()
	a.sessionCount++
	a.mu.Unlock()
	a.refreshPanel()
}
