package ui

import (
	"testing"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
)

func TestMapConnState(t *testing.T) {
	cases := []struct {
		in   core.ConnState
		want TrayState
	}{
		{core.StateConnecting, StateConnecting},
		{core.StateReady, StateReady},
		{core.StateAuthFailed, StateError},
		{core.StateDisconnected, StateOffline},
	}
	for _, c := range cases {
		if got := mapConnState(c.in); got != c.want {
			t.Errorf("mapConnState(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStatusTitle(t *testing.T) {
	if got := statusTitle(StateReady, ""); got != "Ready" {
		t.Errorf("statusTitle with no detail = %q, want %q", got, "Ready")
	}
	if got := statusTitle(StateError, "re-pair required"); got != "Error — re-pair required" {
		t.Errorf("statusTitle with detail = %q", got)
	}
}

// TestHotkeysLiveWhilePhoneBusy pins the fix for a wedge found on the device: a refused dictation
// used to move the tray to StateError, which disarmed the hotkey, and nothing re-armed it until a
// reconnect — so the hotkey appeared dead after the phone freed. A taken phone is a refusal, not a
// transport failure, so the hotkey must stay armed.
func TestHotkeysLiveWhilePhoneBusy(t *testing.T) {
	for _, s := range []TrayState{StateReady, StateDictating, StateBusy} {
		if !hotkeysLive(s) {
			t.Errorf("hotkeysLive(%v) = false, want true (the hotkey must stay armed)", s)
		}
	}
	for _, s := range []TrayState{StateOffline, StateConnecting, StateError} {
		if hotkeysLive(s) {
			t.Errorf("hotkeysLive(%v) = true, want false (no link to dictate over)", s)
		}
	}
}

// TestBusyStateLabel checks the tray names the taken phone without calling it an error.
func TestBusyStateLabel(t *testing.T) {
	if got := stateLabel(StateBusy); got != "Phone busy" {
		t.Errorf("stateLabel(StateBusy) = %q, want %q", got, "Phone busy")
	}
	if got := statusTitle(StateBusy, busyClause(core.ServerStatePayload{State: core.ServerStateReserved})); got != "Phone busy — the phone's pill is dictating" {
		t.Errorf("busy tray title = %q", got)
	}
}
