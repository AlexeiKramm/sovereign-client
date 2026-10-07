package ui

import (
	"path/filepath"
	"testing"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
	"github.com/alexeikramm/sovereign-desktop/internal/ui/render"
)

// TestPanelInfoOmitsEmptyValues pins the footer's content: timings and latency only appear once
// they exist, and the session count is always shown.
func TestPanelInfoOmitsEmptyValues(t *testing.T) {
	a := &app{}
	a.sessionCount = 2
	got := a.panelInfo()
	if len(got) != 1 || got[0] != "Sessions 2" {
		t.Fatalf("panelInfo with only sessions = %q, want [Sessions 2]", got)
	}

	a.lastAsrMs, a.lastCleanupMs = 1283, 547
	a.lastLatency = "rtt=12ms"
	got = a.panelInfo()
	if len(got) != 3 {
		t.Fatalf("panelInfo = %q, want three lines", got)
	}
	if got[0] != "Last ASR 1283 ms · Cleanup 547 ms" || got[1] != "Link rtt=12ms" || got[2] != "Sessions 2" {
		t.Fatalf("panelInfo = %q", got)
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
	a.panelAction(render.Control{Kind: render.ControlChip, ID: "position:" + PosTopLeft})
	if a.settings.OverlayPosition != PosTopLeft {
		t.Fatalf("position = %q, want %q", a.settings.OverlayPosition, PosTopLeft)
	}
	// A close control must not be mistaken for a chip (its Kind is the zero value).
	a.panelContent.shown = true
	a.panelAction(render.Control{Kind: render.ControlClose})
	if a.panelContent.shown {
		t.Fatal("close control did not close the panel")
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

// TestPanelInfoShowsAvailability checks the availability line leads the footer while the phone is
// taken, disappears when it is idle, and is omitted for our own session (which the phone reports as
// BUSY "another desktop" and would otherwise read as someone else dictating).
func TestPanelInfoShowsAvailability(t *testing.T) {
	a := &app{}
	a.phoneState = core.ServerStatePayload{State: core.ServerStateReserved, HolderName: "Phone"}
	info := a.panelInfo()
	if len(info) == 0 || info[0] != "Phone busy — the phone's pill is dictating" {
		t.Fatalf("panelInfo = %q, want the availability line first", info)
	}

	a.phoneState = core.ServerStatePayload{}
	if info := a.panelInfo(); len(info) != 1 || info[0] != "Sessions 0" {
		t.Fatalf("panelInfo when idle = %q, want only the session count", info)
	}

	a.dictating = true
	a.phoneState = core.ServerStatePayload{State: core.ServerStateBusy, HolderName: "another desktop"}
	if info := a.panelInfo(); len(info) != 1 || info[0] != "Sessions 0" {
		t.Fatalf("panelInfo while dictating = %q, want the availability line omitted", info)
	}
}
