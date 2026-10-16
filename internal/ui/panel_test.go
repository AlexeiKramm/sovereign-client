package ui

import (
	"path/filepath"
	"testing"

	"github.com/alexeikramm/sovereign-client/internal/core"
	"github.com/alexeikramm/sovereign-client/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-client/internal/windows"
)

// TestPanelInfoEmpty pins that the footer no longer shows session/timing stats (the footer
// only displays the version number).
func TestPanelInfoEmpty(t *testing.T) {
	a := &app{}
	a.sessionCount = 2
	a.lastAsrMs, a.lastCleanupMs = 1283, 547
	a.lastLatency = "rtt=12ms"
	got := a.panelInfo()
	if len(got) != 0 {
		t.Fatalf("panelInfo = %q, want empty (footer only displays version number)", got)
	}
}

// TestLiveTextTracksPanelWhileHidden checks the panel view is kept current even when the window is
// closed, so opening it mid-dictation shows the live text rather than a blank box.
func TestLiveTextTracksPanelWhileHidden(t *testing.T) {
	a := &app{}
	a.setLiveCommitted("settled")
	a.setLiveProvisional("live tail")
	if a.panelContent.committed != "settled" || a.panelContent.provisional != "live tail" {
		t.Fatalf("panel view = %+v, want the live text", a.panelContent)
	}
	if a.panelContent.shown {
		t.Fatal("panel should be hidden by default")
	}
}

// TestPanelRowsReflectSettings checks the settings rows are built from the live configuration:
// the selected chip follows the setting, and the connection button swaps with pairing.
func TestPanelRowsReflectSettings(t *testing.T) {
	a := &app{}
	a.settings = DefaultSettings()
	a.paired = false

	rows := a.panelRows()
	mode := rowByText(rows, "Dictation mode")
	if mode == nil {
		t.Fatal("no Dictation mode row")
	}
	if !optionOn(mode, "mode:"+ModeAuto) || optionOn(mode, "mode:"+ModeHold) {
		t.Fatalf("mode chips = %+v", mode.Options)
	}
	if _, ok := rowByID(rows, "pair"); !ok {
		t.Fatal("expected a pair button when unpaired")
	}
	if _, ok := rowByID(rows, "unpair"); ok {
		t.Fatal("unexpected unpair button when unpaired")
	}

	a.paired = true
	a.creds.ServerName = "Phone"
	rows = a.panelRows()
	if _, ok := rowByID(rows, "unpair"); !ok {
		t.Fatal("expected an unpair button when paired")
	}
	if _, ok := rowByID(rows, "pair"); ok {
		t.Fatal("unexpected pair button when paired")
	}
}

// TestPanelRowsHotkeyCapture checks the hotkey row prompts while a capture is in flight.
func TestPanelRowsHotkeyCapture(t *testing.T) {
	a := &app{}
	a.settings = DefaultSettings()
	a.hotkeyCapture = true
	r, ok := rowByID(a.panelRows(), "hotkey:set")
	if !ok {
		t.Fatal("no hotkey row")
	}
	if r.Detail != "Press a key…" {
		t.Fatalf("hotkey detail = %q, want the capture prompt", r.Detail)
	}
}

// TestPanelActionUpdatesSettings drives the chip/button dispatch the mouse handler uses.
func TestPanelActionUpdatesSettings(t *testing.T) {
	a := &app{}
	a.opts.SettingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.settings = DefaultSettings()

	a.panelAction(render.Control{Kind: render.ControlChip, ID: "mode:" + ModeHold})
	if a.settings.Mode != ModeHold {
		t.Fatalf("mode = %q, want %q", a.settings.Mode, ModeHold)
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "insertion:" + InsertType})
	if a.settings.Insertion != InsertType {
		t.Fatalf("insertion = %q, want %q", a.settings.Insertion, InsertType)
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "separator:" + SepNewline})
	if a.settings.Separator != SepNewline {
		t.Fatalf("separator = %q, want %q", a.settings.Separator, SepNewline)
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "position:" + PosTopLeft})
	if a.settings.OverlayPosition != PosTopLeft {
		t.Fatalf("position = %q, want %q", a.settings.OverlayPosition, PosTopLeft)
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "theme:" + ThemeDark})
	if a.settings.ThemeMode != ThemeDark {
		t.Fatalf("theme = %q, want %q", a.settings.ThemeMode, ThemeDark)
	}
	if !a.isDarkTheme() {
		t.Fatal("isDarkTheme should be true when ThemeDark")
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "theme:" + ThemeLight})
	if a.settings.ThemeMode != ThemeLight {
		t.Fatalf("theme = %q, want %q", a.settings.ThemeMode, ThemeLight)
	}
	a.panelAction(render.Control{Kind: render.ControlButton, ID: "dropdown:accent"})
	if !a.accentDropdownOpen {
		t.Fatal("dropdown:accent did not open dropdown")
	}
	a.panelAction(render.Control{Kind: render.ControlButton, ID: "preset:amber"})
	if a.settings.ThemePreset != "amber" {
		t.Fatalf("preset = %q, want %q", a.settings.ThemePreset, "amber")
	}
	if a.accentDropdownOpen {
		t.Fatal("preset selection did not close dropdown")
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "feedback:3"})
	if a.settings.FeedbackDurationSec != 3 {
		t.Fatalf("feedback = %d, want 3", a.settings.FeedbackDurationSec)
	}
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "feedback:off"})
	if a.settings.FeedbackDurationSec != -1 {
		t.Fatalf("feedback = %d, want -1", a.settings.FeedbackDurationSec)
	}
	a.panelAction(render.Control{Kind: render.ControlButton, ID: "tab:settings"})
	if a.panelActiveTab != render.TabSettings {
		t.Fatalf("active tab = %d, want TabSettings", a.panelActiveTab)
	}
	a.panelAction(render.Control{Kind: render.ControlButton, ID: "tab:dictations"})
	if a.panelActiveTab != render.TabDictations {
		t.Fatalf("active tab = %d, want TabDictations", a.panelActiveTab)
	}
	// A close control must not be mistaken for a chip (its Kind is the zero value).
	a.panelContent.shown = true
	a.panelAction(render.Control{Kind: render.ControlClose})
	if a.panelContent.shown {
		t.Fatal("close control did not close the panel")
	}
}

// TestPanelHistoryActions verifies copy and view-mode cycling on history cards.
func TestPanelHistoryActions(t *testing.T) {
	a := &app{}
	a.history = NewHistoryStore("")
	it := a.history.Add("Hello world", "hello world raw", 200, 100)

	a.panelAction(render.Control{Kind: render.ControlButton, ID: "histmode:" + it.ID})
	items := a.history.Items()
	if items[0].ViewMode != ViewRaw {
		t.Fatalf("view mode = %q, want %q", items[0].ViewMode, ViewRaw)
	}

	a.panelAction(render.Control{Kind: render.ControlButton, ID: "histmode:" + it.ID})
	items = a.history.Items()
	if items[0].ViewMode != ViewDiff {
		t.Fatalf("view mode = %q, want %q", items[0].ViewMode, ViewDiff)
	}

	a.panelAction(render.Control{Kind: render.ControlButton, ID: "histmode:" + it.ID})
	items = a.history.Items()
	if items[0].ViewMode != ViewClean {
		t.Fatalf("view mode = %q, want %q", items[0].ViewMode, ViewClean)
	}

	a.panelAction(render.Control{Kind: render.ControlButton, ID: "copy:" + it.ID})
	if a.panelCopiedID != it.ID {
		t.Fatalf("copied ID = %q, want %q", a.panelCopiedID, it.ID)
	}
}

// TestToggleHistoryExpanded checks the whole-card click toggles a card between preview and full text.
func TestToggleHistoryExpanded(t *testing.T) {
	a := &app{}
	a.history = NewHistoryStore("")
	it := a.history.Add("Hello world", "hello world", 200, 100)

	a.toggleHistoryExpanded(it.ID)
	if !a.panelExpanded[it.ID] {
		t.Fatal("card was not expanded")
	}
	a.toggleHistoryExpanded(it.ID)
	if a.panelExpanded[it.ID] {
		t.Fatal("card was not collapsed")
	}
}

func rowByID(rows []render.Row, id string) (render.Row, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return render.Row{}, false
}

func rowByText(rows []render.Row, text string) *render.Row {
	for i := range rows {
		if rows[i].Text == text {
			return &rows[i]
		}
	}
	return nil
}

func optionOn(r *render.Row, id string) bool {
	for _, o := range r.Options {
		if o.ID == id {
			return o.On
		}
	}
	return false
}

// TestAvailabilityText pins the wording the control window shows for the phone's advertised
// availability (SERVER_STATE, v1.1), including the phone's own name for a phone-local capture and
// the "another desktop" placeholder the phone sends for a session it cannot name.
func TestAvailabilityText(t *testing.T) {
	cases := []struct {
		in   core.ServerStatePayload
		want string
	}{
		{core.ServerStatePayload{State: core.ServerStateIdle}, ""},
		{core.ServerStatePayload{State: core.ServerStateReserved, HolderName: "Phone"},
			"Phone busy — the phone's pill is dictating"},
		{core.ServerStatePayload{State: core.ServerStateBusy, HolderName: "another desktop"},
			"Phone busy — another desktop is dictating"},
		{core.ServerStatePayload{State: core.ServerStateBusy, HolderName: "Laptop"},
			"Phone busy — Laptop is dictating"},
	}
	for _, c := range cases {
		if got := availabilityText(c.in); got != c.want {
			t.Errorf("availabilityText(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBusyMessage pins the pill's refusal wording, shared with the panel.
func TestBusyMessage(t *testing.T) {
	if got := busyMessage(""); got != "Phone busy" {
		t.Errorf("busyMessage(\"\") = %q", got)
	}
	if got := busyMessage("another desktop"); got != "Phone busy — another desktop is dictating" {
		t.Errorf("busyMessage(another desktop) = %q", got)
	}
	if got := busyMessage("Phone"); got != "Phone busy — Phone is dictating" {
		t.Errorf("busyMessage(Phone) = %q", got)
	}
}

// TestBusyClause pins the tray's short form for the same states (the tray prefixes its own label).
func TestBusyClause(t *testing.T) {
	if got := busyClause(core.ServerStatePayload{State: core.ServerStateReserved, HolderName: "Phone"}); got != "the phone's pill is dictating" {
		t.Errorf("busyClause(RESERVED) = %q", got)
	}
	if got := busyClause(core.ServerStatePayload{State: core.ServerStateBusy, HolderName: "another desktop"}); got != "another desktop is dictating" {
		t.Errorf("busyClause(BUSY) = %q", got)
	}
}

// TestComputePanelPosAnchoredExpandsUpward verifies that when anchored to bottom-right,
// expanding height (e.g. opening dropdown) moves the top edge UP and keeps the bottom pinned above the taskbar.
func TestComputePanelPosAnchoredExpandsUpward(t *testing.T) {
	work := winplat.Rect{X: 0, Y: 0, W: 1920, H: 1040}
	dpi := 96
	cw := 380

	posClosed := computePanelPos(work, cw, 480, dpi, false, [2]int{0, 0}, 0)
	posOpen := computePanelPos(work, cw, 720, dpi, false, posClosed, 480)

	bottomClosed := posClosed[1] + 480
	bottomOpen := posOpen[1] + 720

	if bottomClosed != bottomOpen {
		t.Errorf("bottom changed on expand: closed bottom %d vs open bottom %d", bottomClosed, bottomOpen)
	}
	if posOpen[1] >= posClosed[1] {
		t.Errorf("window did not expand upward: closed y=%d, open y=%d", posClosed[1], posOpen[1])
	}
}

// TestComputePanelPosUserMovedExpandsAwayFromScreenEdge checks that when moved to the lower half
// of the screen, height expansion grows upward and stays within work area bounds.
func TestComputePanelPosUserMovedExpandsAwayFromScreenEdge(t *testing.T) {
	work := winplat.Rect{X: 0, Y: 0, W: 1920, H: 1040}
	dpi := 96
	cw := 380

	// User placed window at (500, 500) with height 480 (bottom at 980, in lower half)
	initialPos := [2]int{500, 500}
	expandedPos := computePanelPos(work, cw, 720, dpi, true, initialPos, 480)

	// Bottom should remain at 980 (500 + 480 - 720 = 260)
	if expandedPos[1]+720 != 980 {
		t.Errorf("bottom edge shifted: got y=%d + h=720 = %d, want 980", expandedPos[1], expandedPos[1]+720)
	}
	if expandedPos[1] != 260 {
		t.Errorf("expected y=260, got %d", expandedPos[1])
	}
}

// TestRecordCommittedAccumulatesPerf verifies that per-commit perf measurements are accumulated
// across multiple commits and that trailing zero-delta commits (such as punctuation finalization)
// do not clobber the utterance's total timings.
func TestRecordCommittedAccumulatesPerf(t *testing.T) {
	a := &app{}
	a.recordCommitted(core.CommitPerf{AsrMs: 1200, CleanupMs: 650})
	if a.lastAsrMs != 1200 || a.lastCleanupMs != 650 {
		t.Fatalf("first commit: got asr=%d cleanup=%d, want 1200, 650", a.lastAsrMs, a.lastCleanupMs)
	}

	// Second commit delta
	a.recordCommitted(core.CommitPerf{AsrMs: 300, CleanupMs: 150})
	if a.lastAsrMs != 1500 || a.lastCleanupMs != 800 {
		t.Fatalf("second commit: got asr=%d cleanup=%d, want 1500, 800", a.lastAsrMs, a.lastCleanupMs)
	}

	// Trailing zero commit (e.g. punctuation normalization)
	a.recordCommitted(core.CommitPerf{AsrMs: 0, CleanupMs: 0})
	if a.lastAsrMs != 1500 || a.lastCleanupMs != 800 {
		t.Fatalf("trailing zero commit: got asr=%d cleanup=%d, want 1500, 800", a.lastAsrMs, a.lastCleanupMs)
	}
}
