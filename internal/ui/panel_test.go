package ui

import "testing"

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
