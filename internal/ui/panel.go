package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
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

// closePanel hides the control window and abandons any in-flight hotkey capture; the tray keeps
// running.
func (a *app) closePanel() {
	a.cancelHotkeyCapture()
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

// refreshPanel repaints the panel if it is open, e.g. after a setting changes.
func (a *app) refreshPanel() {
	a.panelMu.Lock()
	shown := a.panelContent.shown
	a.panelMu.Unlock()
	if shown {
		a.repaintPanel()
	}
}

// panelRows builds the settings rows from the current configuration. It is rebuilt on every repaint
// so a change takes effect immediately.
func (a *app) panelRows() []render.Row {
	a.mu.Lock()
	s := a.settings
	paired := a.paired
	server := a.creds.ServerName
	capturing := a.hotkeyCapture
	startup := a.startupOn
	a.mu.Unlock()

	hotkey := s.Hotkey
	if capturing {
		hotkey = "Press a key…"
	}

	rows := []render.Row{
		{Kind: render.RowChips, Text: "Dictation mode", Options: []render.Option{
			{ID: "mode:" + ModeAuto, Text: "Tap or hold", On: s.Mode == ModeAuto},
			{ID: "mode:" + ModeHold, Text: "Hold to talk", On: s.Mode == ModeHold},
			{ID: "mode:" + ModeToggle, Text: "Press to toggle", On: s.Mode == ModeToggle},
		}},
		{Kind: render.RowButton, ID: "hotkey:set", Text: "Set custom hotkey…", Detail: hotkey},
		{Kind: render.RowChips, Text: "Insertion", Options: []render.Option{
			{ID: "insertion:" + InsertPaste, Text: "Paste (fast)", On: s.Insertion == InsertPaste},
			{ID: "insertion:" + InsertType, Text: "Type", On: s.Insertion == InsertType},
		}},
		{Kind: render.RowChips, Text: "Overlay position", Options: []render.Option{
			{ID: "position:" + PosBottomRight, Text: "Bottom right", On: s.OverlayPosition == PosBottomRight},
			{ID: "position:" + PosBottomLeft, Text: "Bottom left", On: s.OverlayPosition == PosBottomLeft},
			{ID: "position:" + PosTopRight, Text: "Top right", On: s.OverlayPosition == PosTopRight},
			{ID: "position:" + PosTopLeft, Text: "Top left", On: s.OverlayPosition == PosTopLeft},
		}},
		{Kind: render.RowToggle, ID: "startup", Text: "Start with Windows", On: startup},
	}
	if paired {
		rows = append(rows, render.Row{Kind: render.RowButton, ID: "unpair", Text: "Unpair…", Detail: server})
	} else {
		rows = append(rows, render.Row{Kind: render.RowButton, ID: "pair", Text: "Pair with phone…"})
	}
	rows = append(rows, render.Row{Kind: render.RowButton, ID: "quit", Text: "Quit Sovereign"})
	return rows
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
		Rows:        a.panelRows(),
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

// panelInfo builds the footer lines: the phone's advertised availability, the last utterance's
// ASR/cleanup timings, the link RTT and the session count. Empty values are omitted so the footer
// never reserves space it does not use.
func (a *app) panelInfo() []string {
	a.mu.Lock()
	asr, cleanup := a.lastAsrMs, a.lastCleanupMs
	latency := a.lastLatency
	sessions := a.sessionCount
	phone := a.phoneState
	a.mu.Unlock()

	var info []string
	if t := availabilityText(phone); t != "" {
		info = append(info, t)
	}
	if asr > 0 || cleanup > 0 {
		info = append(info, fmt.Sprintf("Last ASR %d ms · Cleanup %d ms", asr, cleanup))
	}
	if latency != "" {
		info = append(info, "Link "+latency)
	}
	info = append(info, fmt.Sprintf("Sessions %d", sessions))
	return info
}

// availabilityText describes the phone's advertised pipeline availability (SERVER_STATE, v1.1). It
// is empty when the phone is idle, so the caller can omit the line. It is advisory only: the reply
// to a START_DICTATION stays authoritative (PROTOCOL.md §5).
func availabilityText(s core.ServerStatePayload) string {
	switch s.State {
	case core.ServerStateReserved:
		return "Phone busy — the phone's pill is dictating"
	case core.ServerStateBusy:
		return busyMessage(s.HolderName)
	default:
		return ""
	}
}

// busyMessage names the holder in a refusal, for both the panel footer (SERVER_STATE) and the pill
// (SESSION_BUSY). The phone names itself for a phone-local capture and sends the placeholder
// "another desktop" for a desktop session it cannot name.
func busyMessage(holder string) string {
	switch holder {
	case "":
		return "Phone busy"
	case "another desktop":
		return "Phone busy — another desktop is dictating"
	default:
		return "Phone busy — " + holder + " is dictating"
	}
}

// onPanelMouse handles clicks and drags. It runs on the window thread, so it never blocks: a control
// click is dispatched to a goroutine (some actions re-arm the hotkey, which starts a thread).
func (a *app) onPanelMouse(ev winplat.MouseEvent) {
	a.panelMu.Lock()
	layout := a.panelLayout
	ov := a.panelWin
	a.panelMu.Unlock()

	switch ev.Kind {
	case winplat.MouseDown:
		if c, ok := layout.ControlAt(ev.X, ev.Y); ok {
			go a.panelAction(c)
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

// panelAction performs a control's action by id.
func (a *app) panelAction(c render.Control) {
	switch {
	case c.Kind == render.ControlClose:
		a.closePanel()
	case strings.HasPrefix(c.ID, "mode:"):
		a.setMode(strings.TrimPrefix(c.ID, "mode:"))
	case strings.HasPrefix(c.ID, "insertion:"):
		a.setInsertion(strings.TrimPrefix(c.ID, "insertion:"))
	case strings.HasPrefix(c.ID, "position:"):
		a.setPosition(strings.TrimPrefix(c.ID, "position:"))
	case c.ID == "startup":
		a.toggleStartup()
	case c.ID == "hotkey:set":
		a.startPanelHotkeyCapture()
	case c.ID == "pair":
		go a.doPair()
	case c.ID == "unpair":
		go a.doUnpair()
	case c.ID == "quit":
		a.quit()
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

// startPanelHotkeyCapture arms the keyboard hook so the next chord the user presses becomes the
// dictation hotkey. The main watcher is disarmed first so the pressed chord does not also fire the
// current hotkey; it is re-armed when the capture ends.
func (a *app) startPanelHotkeyCapture() {
	a.mu.Lock()
	if a.hotkeyCapture {
		a.mu.Unlock()
		return
	}
	wasArmed := a.armed
	a.hotkeyCapture = true
	abort := make(chan struct{})
	a.hotkeyAbort = abort
	a.mu.Unlock()

	a.disarmHotkey()
	a.repaintPanel()

	w, err := winplat.StartHotkeyCapture()
	if err != nil {
		a.logf("hotkey capture failed: %v", err)
		a.finishHotkeyCapture(wasArmed)
		return
	}
	a.logf("capturing a new hotkey; press the combination")

	go func() {
		defer func() {
			_ = w.Close()
			a.finishHotkeyCapture(wasArmed)
		}()
		select {
		case c := <-w.Captured():
			if c.Mods == (iface.Modifiers{}) && !iface.IsFunctionKey(c.VK) {
				a.logf("hotkey capture: %s has no modifier; keeping the current hotkey", iface.KeyName(c.VK))
				return
			}
			chord := iface.NewChord(c.VK, c.Mods)
			a.applyHotkey(chord.String())
			a.logf("hotkey set to %s", chord)
		case <-abort:
			a.logf("hotkey capture cancelled")
		case <-time.After(15 * time.Second):
			a.logf("hotkey capture timed out; keeping the current hotkey")
		case <-a.ctx.Done():
		}
	}()
}

// finishHotkeyCapture clears the capture state and restores the watcher.
func (a *app) finishHotkeyCapture(wasArmed bool) {
	a.mu.Lock()
	a.hotkeyCapture = false
	a.hotkeyAbort = nil
	a.mu.Unlock()
	if wasArmed {
		a.armHotkey()
	}
	a.refreshPanel()
}

// cancelHotkeyCapture abandons an in-flight capture, e.g. when the panel is closed.
func (a *app) cancelHotkeyCapture() {
	a.mu.Lock()
	ch := a.hotkeyAbort
	a.hotkeyAbort = nil
	a.mu.Unlock()
	if ch != nil {
		close(ch)
	}
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
