package ui

import (
	"path/filepath"
	"testing"

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
