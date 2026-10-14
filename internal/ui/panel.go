package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alexeikramm/sovereign-client/internal/core"
	"github.com/alexeikramm/sovereign-client/internal/core/iface"
	"github.com/alexeikramm/sovereign-client/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-client/internal/windows"
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
	a.accentDropdownOpen = false
	a.panelUserMoved = false
	a.panelScroll = 0
	a.panelExpanded = nil
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

	a.panelMu.Lock()
	accentOpen := a.accentDropdownOpen
	a.panelMu.Unlock()

	currentPresetName := "Cyan"
	for _, p := range render.Presets {
		if p.ID == s.ThemePreset {
			currentPresetName = p.Name
			break
		}
	}

	presetOpts := make([]render.Option, len(render.Presets))
	for i, p := range render.Presets {
		presetOpts[i] = render.Option{
			ID:   "preset:" + p.ID,
			Text: p.Name,
			On:   s.ThemePreset == p.ID || (s.ThemePreset == "" && p.ID == DefaultThemePreset),
		}
	}

	rows := []render.Row{
		// 1. Voice Dictation
		{Kind: render.RowHeader, Text: "Voice Dictation", On: false},
		{Kind: render.RowChips, Text: "Dictation mode", Options: []render.Option{
			{ID: "mode:" + ModeAuto, Text: "Tap or hold", On: s.Mode == ModeAuto},
			{ID: "mode:" + ModeHold, Text: "Hold to talk", On: s.Mode == ModeHold},
			{ID: "mode:" + ModeToggle, Text: "Press to toggle", On: s.Mode == ModeToggle},
		}},
		{Kind: render.RowButton, ID: "hotkey:set", Text: "Set custom hotkey…", Detail: hotkey},

		// 2. Text Output
		{Kind: render.RowHeader, Text: "Text Output", On: true},
		{Kind: render.RowChips, Text: "Insertion", Options: []render.Option{
			{ID: "insertion:" + InsertPaste, Text: "Paste (fast)", On: s.Insertion == InsertPaste},
			{ID: "insertion:" + InsertType, Text: "Type", On: s.Insertion == InsertType},
		}},
		{Kind: render.RowChips, Text: "After each utterance", Options: []render.Option{
			{ID: "separator:" + SepNone, Text: "Nothing", On: s.Separator == SepNone},
			{ID: "separator:" + SepSpace, Text: "Space", On: s.Separator == SepSpace},
			{ID: "separator:" + SepNewline, Text: "New line", On: s.Separator == SepNewline},
		}},

		// 3. Floating Pill
		{Kind: render.RowHeader, Text: "Floating Pill", On: true},
		{Kind: render.RowChips, Text: "Overlay position", Options: []render.Option{
			{ID: "position:" + PosBottomRight, Text: "Bottom right", On: s.OverlayPosition == PosBottomRight},
			{ID: "position:" + PosBottomLeft, Text: "Bottom left", On: s.OverlayPosition == PosBottomLeft},
			{ID: "position:" + PosTopRight, Text: "Top right", On: s.OverlayPosition == PosTopRight},
			{ID: "position:" + PosTopLeft, Text: "Top left", On: s.OverlayPosition == PosTopLeft},
		}},
		{Kind: render.RowChips, Text: "Feedback pill duration", Options: []render.Option{
			{ID: "feedback:1", Text: "1s", On: s.FeedbackDurationSec == 1},
			{ID: "feedback:2", Text: "2s", On: s.FeedbackDurationSec == 2},
			{ID: "feedback:3", Text: "3s", On: s.FeedbackDurationSec == 3},
			{ID: "feedback:off", Text: "Off", On: s.FeedbackDurationSec == -1},
		}},

		// 4. Appearance
		{Kind: render.RowHeader, Text: "Appearance", On: true},
		{Kind: render.RowChips, Text: "Theme", Options: []render.Option{
			{ID: "theme:" + ThemeSystem, Text: "System", On: s.ThemeMode == ThemeSystem || s.ThemeMode == ""},
			{ID: "theme:" + ThemeDark, Text: "Dark", On: s.ThemeMode == ThemeDark},
			{ID: "theme:" + ThemeLight, Text: "Light", On: s.ThemeMode == ThemeLight},
		}},
		{
			Kind:    render.RowDropdown,
			ID:      "dropdown:accent",
			Text:    "Accent color",
			Detail:  currentPresetName,
			Options: presetOpts,
			On:      accentOpen,
		},

		// 5. System & Connection
		{Kind: render.RowHeader, Text: "System & Connection", On: true},
		{Kind: render.RowToggle, ID: "startup", Text: "Start with Windows", On: startup},
		{Kind: render.RowToggle, ID: "debug", Text: "Log transcripts (debug)", On: s.Debug},
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
	userMoved := a.panelUserMoved
	lastH := a.panelLastH
	tab := a.panelActiveTab
	copiedID := a.panelCopiedID
	pressedID := a.panelPressedID
	scroll := a.panelScroll
	expanded := make(map[string]bool, len(a.panelExpanded))
	for id, on := range a.panelExpanded {
		if on {
			expanded[id] = true
		}
	}
	a.panelMu.Unlock()
	if ov == nil || !view.shown {
		return
	}

	var histItems []render.PanelHistoryItem
	if a.history != nil {
		for _, it := range a.history.Items() {
			histItems = append(histItems, render.PanelHistoryItem{
				ID:        it.ID,
				TimeAgo:   formatTimeAgo(it.Time),
				CleanText: it.CleanText,
				RawText:   it.RawText,
				Words:     it.Words,
				AsrMs:     it.AsrMs,
				CleanupMs: it.CleanupMs,
				ViewMode:  it.ViewMode,
				Expanded:  expanded[it.ID],
			})
		}
	}

	work, dpi := winplat.WorkArea(ov.Handle())
	scale := float64(dpi) / 96.0
	dark := a.isDarkTheme()
	p, err := render.RenderPanel(render.PanelModel{
		Title:       "Sovereign",
		Status:      view.status,
		Committed:   view.committed,
		Provisional: view.provisional,
		ActiveTab:   tab,
		History:     histItems,
		CopiedID:    copiedID,
		Rows:        a.panelRows(),
		Info:        a.panelInfo(),
		Version:     "v0.1.0",
		Error:       view.err,
		Dark:        dark,
		Preset:      a.themePreset(),
		Scale:       scale,
		MaxHeight:   maxPanelHeight(work, dpi),
		Scroll:      scroll,
	})
	if err != nil {
		a.logf("panel render failed: %v", err)
		return
	}
	cw, ch := p.Frame.Size()
	pos = computePanelPos(work, cw, ch, dpi, userMoved, pos, lastH)

	a.panelMu.Lock()
	a.panelPos = pos
	a.panelPlaced = true
	a.panelLastH = ch
	a.panelLayout = p
	a.panelScroll = p.Scroll
	a.panelMaxScroll = p.MaxScroll
	a.panelScrollStep = p.ScrollStep
	a.panelScrollArea = p.ScrollArea
	a.panelMu.Unlock()

	ov.SetHitRect(winplat.Rect{X: p.Frame.Pad, Y: p.Frame.Pad, W: cw, H: ch})
	// Composite the current press highlight (if any) over the fresh base render, so a background
	// repaint while a control is held does not drop the highlight.
	ov.Present(render.WithPressHighlight(p.Frame.Img, p.Controls, pressedID, dark), pos[0]-p.Frame.Pad, pos[1]-p.Frame.Pad)
}

// maxPanelHeight caps the control window to the work area (minus the usual margin), so an expanded
// utterance scrolls inside the window instead of running off the screen.
func maxPanelHeight(work winplat.Rect, dpi int) float64 {
	if work.H <= 0 {
		return 0
	}
	m := pillMargin * float64(dpi) / 96.0
	return float64(work.H) - 2*m
}

// presentPanelPress updates only the press highlight: it composites the scrim over the last rendered
// frame and hands that to the window, without re-rendering the panel. A click therefore reads back
// instantly instead of after a full render.
func (a *app) presentPanelPress() {
	a.panelMu.Lock()
	ov := a.panelWin
	shown := a.panelContent.shown
	layout := a.panelLayout
	pos := a.panelPos
	pressed := a.panelPressedID
	a.panelMu.Unlock()
	if ov == nil || !shown || layout.Frame.Img == nil {
		return
	}
	img := render.WithPressHighlight(layout.Frame.Img, layout.Controls, pressed, a.isDarkTheme())
	ov.Present(img, pos[0]-layout.Frame.Pad, pos[1]-layout.Frame.Pad)
}

// computePanelPos calculates the window position so that when its size changes (such as expanding
// the accent color dropdown), it always expands away from the nearest screen edge and stays strictly
// within the work area.
func computePanelPos(work winplat.Rect, cw, ch int, dpi int, userMoved bool, lastPos [2]int, lastH int) [2]int {
	m := int(math.Round(pillMargin * float64(dpi) / 96.0))
	if !userMoved {
		// Anchored to bottom-right by default (above taskbar).
		// When height changes, anchorPos keeps the bottom edge fixed and expands UPWARD.
		x, y := anchorPos(work, cw, ch, PosBottomRight, dpi)
		return [2]int{x, y}
	}
	pos := lastPos
	if lastH > 0 && ch != lastH {
		deltaH := ch - lastH
		// If in lower half of screen, expand UPWARD away from bottom screen edge.
		if pos[1]+lastH/2 >= work.Y+work.H/2 {
			pos[1] -= deltaH
		}
		// If in upper half, keep top edge fixed and expand DOWNWARD away from top edge.
	}
	// Clamp strictly within work area bounds
	if pos[1]+ch > work.Y+work.H-m {
		pos[1] = work.Y + work.H - m - ch
	}
	if pos[1] < work.Y+m {
		pos[1] = work.Y + m
	}
	if pos[0]+cw > work.X+work.W-m {
		pos[0] = work.X + work.W - m - cw
	}
	if pos[0] < work.X+m {
		pos[0] = work.X + m
	}
	return pos
}

// panelInfo builds the footer lines: the phone's advertised availability, the last utterance's
// panelInfo returns nil: the panel footer only displays the version number now that
// each history entry shows its own ASR and cleanup timings.
func (a *app) panelInfo() []string {
	return nil
}

// availabilityText describes the phone's advertised pipeline availability (SERVER_STATE, v1.1). It
// is empty when the phone is idle, so the caller can omit the line. It is advisory only: the reply
// to a START_DICTATION stays authoritative (PROTOCOL.md §5).
func availabilityText(s core.ServerStatePayload) string {
	switch s.State {
	case core.ServerStateReserved, core.ServerStateBusy:
		if c := busyClause(s); c != "" {
			return "Phone busy — " + c
		}
		return "Phone busy"
	default:
		return ""
	}
}

// busyClause names who holds the pipeline for a SERVER_STATE frame: the phone itself for a
// phone-local capture, otherwise the holder.
func busyClause(s core.ServerStatePayload) string {
	if s.State == core.ServerStateReserved {
		return "the phone's pill is dictating"
	}
	return busyHolder(s.HolderName)
}

// busyHolder turns a holder name into the "who is dictating" clause. The phone names itself for a
// phone-local capture and sends the placeholder "another desktop" for a desktop it cannot name.
func busyHolder(holder string) string {
	switch holder {
	case "":
		return ""
	case "another desktop":
		return "another desktop is dictating"
	default:
		return holder + " is dictating"
	}
}

// busyMessage is the pill's one-line refusal, sharing the panel's wording.
func busyMessage(holder string) string {
	if c := busyHolder(holder); c != "" {
		return "Phone busy — " + c
	}
	return "Phone busy"
}

// onPanelMouse handles clicks and drags. It runs on the window thread, so it never blocks: a control
// click is dispatched to a goroutine (some actions re-arm the hotkey, which starts a thread). A
// press highlights the control immediately; it fires on release, and only if the pointer is still
// over the control the press began on, so a click has visible feedback and a drag-off cancels it.
func (a *app) onPanelMouse(ev winplat.MouseEvent) {
	a.panelMu.Lock()
	layout := a.panelLayout
	ov := a.panelWin
	a.panelMu.Unlock()

	switch ev.Kind {
	case winplat.MouseWheel:
		a.panelMu.Lock()
		step := a.panelScrollStep
		maxScroll := a.panelMaxScroll
		area := a.panelScrollArea
		cur := a.panelScroll
		a.panelMu.Unlock()
		if step <= 0 || ev.Delta == 0 {
			return
		}
		if ev.X < area.Min.X || ev.X >= area.Max.X || ev.Y < area.Min.Y || ev.Y >= area.Max.Y {
			return // wheel outside the scrollable list does nothing
		}
		n := cur - ev.Delta*step/120
		if n < 0 {
			n = 0
		}
		if n > maxScroll {
			n = maxScroll
		}
		if n == cur {
			return
		}
		a.panelMu.Lock()
		a.panelScroll = n
		a.panelMu.Unlock()
		a.refreshPanel()
	case winplat.MouseDown:
		if c, ok := layout.ControlAt(ev.X, ev.Y); ok {
			a.panelMu.Lock()
			a.panelPressing = true
			a.panelPressID = c.ID
			a.panelPressedID = c.ID
			a.panelMu.Unlock()
			a.presentPanelPress()
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
		pressing := a.panelPressing
		a.panelMu.Unlock()
		if pressing {
			// Follow the pointer in and out of the pressed control so the highlight matches where a
			// release would fire.
			hover := ""
			if c, ok := layout.ControlAt(ev.X, ev.Y); ok {
				hover = c.ID
			}
			a.panelMu.Lock()
			changed := a.panelPressedID != hover
			a.panelPressedID = hover
			a.panelMu.Unlock()
			if changed {
				a.presentPanelPress()
			}
		}
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
		a.panelUserMoved = true
		a.panelMu.Unlock()
		ov.Move(nx, ny)
	case winplat.MouseUp:
		a.panelMu.Lock()
		pressID := a.panelPressID
		pressing := a.panelPressing
		a.panelPressID = ""
		a.panelPressedID = ""
		a.panelPressing = false
		a.panelDragging = false
		a.panelMu.Unlock()
		if !pressing {
			return
		}
		a.presentPanelPress()
		if c, ok := layout.ControlAt(ev.X, ev.Y); ok && c.ID == pressID {
			go a.panelAction(c)
		}
	}
}

// panelAction performs a control's action by id.
func (a *app) panelAction(c render.Control) {
	if c.ID != "dropdown:accent" && !strings.HasPrefix(c.ID, "preset:") {
		a.panelMu.Lock()
		if a.accentDropdownOpen {
			a.accentDropdownOpen = false
			a.panelMu.Unlock()
			a.refreshPanel()
		} else {
			a.panelMu.Unlock()
		}
	}

	switch {
	case c.Kind == render.ControlClose:
		a.closePanel()
	case c.ID == "tab:dictations":
		a.panelMu.Lock()
		a.panelActiveTab = render.TabDictations
		a.panelScroll = 0
		a.panelMu.Unlock()
		a.refreshPanel()
	case c.ID == "tab:settings":
		a.panelMu.Lock()
		a.panelActiveTab = render.TabSettings
		a.panelScroll = 0
		a.panelMu.Unlock()
		a.refreshPanel()
	case strings.HasPrefix(c.ID, "expand:"):
		a.toggleHistoryExpanded(strings.TrimPrefix(c.ID, "expand:"))
	case strings.HasPrefix(c.ID, "copy:"):
		id := strings.TrimPrefix(c.ID, "copy:")
		a.copyHistoryItem(id)
	case strings.HasPrefix(c.ID, "histmode:"):
		id := strings.TrimPrefix(c.ID, "histmode:")
		a.cycleHistoryMode(id)
	case strings.HasPrefix(c.ID, "feedback:"):
		arg := strings.TrimPrefix(c.ID, "feedback:")
		if arg == "off" || arg == "0" {
			a.setFeedbackDuration(-1)
		} else {
			var sec int
			fmt.Sscanf(arg, "%d", &sec)
			a.setFeedbackDuration(sec)
		}
	case c.ID == "dropdown:accent":
		a.panelMu.Lock()
		a.accentDropdownOpen = !a.accentDropdownOpen
		a.panelMu.Unlock()
		a.refreshPanel()
	case strings.HasPrefix(c.ID, "mode:"):
		a.setMode(strings.TrimPrefix(c.ID, "mode:"))
	case strings.HasPrefix(c.ID, "insertion:"):
		a.setInsertion(strings.TrimPrefix(c.ID, "insertion:"))
	case strings.HasPrefix(c.ID, "separator:"):
		a.setSeparator(strings.TrimPrefix(c.ID, "separator:"))
	case strings.HasPrefix(c.ID, "position:"):
		a.setPosition(strings.TrimPrefix(c.ID, "position:"))
	case strings.HasPrefix(c.ID, "theme:"):
		a.setThemeMode(strings.TrimPrefix(c.ID, "theme:"))
	case strings.HasPrefix(c.ID, "preset:"):
		a.panelMu.Lock()
		a.accentDropdownOpen = false
		a.panelMu.Unlock()
		a.setThemePreset(strings.TrimPrefix(c.ID, "preset:"))
	case c.ID == "startup":
		a.toggleStartup()
	case c.ID == "debug":
		a.toggleDebug()
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
	a.updatePill(func(v *pillView) {
		if v.insertedWords == 0 {
			v.committed = text
		}
	})
	a.updatePanel(func(v *panelView) { v.committed = text })
}

func (a *app) setLiveProvisional(text string) {
	a.updatePill(func(v *pillView) {
		if v.insertedWords == 0 {
			v.provisional = text
		}
	})
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

// formatTimeAgo produces a concise relative time string for history cards.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "Just now"
	case d < 2*time.Minute:
		return "1 min ago"
	case d < time.Hour:
		return fmt.Sprintf("%d mins ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hrs ago", int(d.Hours()))
	default:
		return t.Format("Jan 02, 15:04")
	}
}

// toggleHistoryExpanded expands or collapses one history card so the whole utterance is readable.
// The card grows in place; if the list no longer fits the window the render clamps the scroll and
// the user can wheel through it.
func (a *app) toggleHistoryExpanded(id string) {
	a.panelMu.Lock()
	if a.panelExpanded == nil {
		a.panelExpanded = make(map[string]bool)
	}
	if a.panelExpanded[id] {
		delete(a.panelExpanded, id)
	} else {
		a.panelExpanded[id] = true
	}
	a.panelMu.Unlock()
	a.refreshPanel()
}

// copyHistoryItem copies the card text to the clipboard and shows feedback.
func (a *app) copyHistoryItem(id string) {
	if a.history == nil {
		return
	}
	var textToCopy string
	for _, it := range a.history.Items() {
		if it.ID == id {
			switch it.ViewMode {
			case ViewRaw:
				textToCopy = it.RawText
			case ViewDiff:
				textToCopy = "RAW:\n" + it.RawText + "\n\nCLEAN:\n" + it.CleanText
			default:
				textToCopy = it.CleanText
			}
			break
		}
	}
	if textToCopy != "" {
		_ = winplat.CopyText(textToCopy)
		a.panelMu.Lock()
		a.panelCopiedID = id
		a.panelMu.Unlock()
		a.refreshPanel()

		time.AfterFunc(1500*time.Millisecond, func() {
			a.panelMu.Lock()
			if a.panelCopiedID == id {
				a.panelCopiedID = ""
			}
			a.panelMu.Unlock()
			a.refreshPanel()
		})
	}
}

// cycleHistoryMode cycles between Clean, Raw, and Diff views for a history card.
func (a *app) cycleHistoryMode(id string) {
	if a.history == nil {
		return
	}
	for _, it := range a.history.Items() {
		if it.ID == id {
			next := ViewClean
			switch it.ViewMode {
			case ViewClean:
				next = ViewRaw
			case ViewRaw:
				next = ViewDiff
			case ViewDiff:
				next = ViewClean
			}
			a.history.SetViewMode(id, next)
			a.refreshPanel()
			return
		}
	}
}

// setFeedbackDuration updates the pill's auto-dismiss interval.
func (a *app) setFeedbackDuration(sec int) {
	a.mu.Lock()
	a.settings.FeedbackDurationSec = sec
	s := a.settings
	a.mu.Unlock()
	a.refreshPanel()
	a.persist(s)
}
