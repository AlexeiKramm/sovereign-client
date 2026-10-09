package ui

import (
	"context"
	"testing"

	"github.com/alexeikramm/sovereign-client/internal/core"
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

// TestHotkeysLive pins that the hotkey stays armed across all states so pressing the hotkey
// delivers immediate visual feedback on the pill (such as phone busy, phone offline, etc.) rather
// than dead input.
func TestHotkeysLive(t *testing.T) {
	for _, s := range []TrayState{StateReady, StateDictating, StateBusy, StateOffline, StateConnecting, StateError} {
		if !hotkeysLive(s) {
			t.Errorf("hotkeysLive(%v) = false, want true (the hotkey must stay armed for user feedback)", s)
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

// TestResolveManualAddr pins that a manual -addr bypasses discovery entirely (no network), which is
// what keeps the connect-on-demand path cheap when the user pinned an address.
func TestResolveManualAddr(t *testing.T) {
	a := &app{opts: Options{Addr: "192.0.2.5:8765", Pin: "PIN"}}
	addr, pin, err := a.resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if addr != "192.0.2.5:8765" || pin != "PIN" {
		t.Fatalf("resolve = %q, %q, want the manual addr and pin", addr, pin)
	}
}

// TestConnectNoopWhenUnpaired pins that the on-demand connector does nothing without stored
// credentials, so launching an unpaired app never touches the network.
func TestConnectNoopWhenUnpaired(t *testing.T) {
	a := &app{ctx: context.Background()}
	a.connect()
	if a.connecting || a.connCancel != nil {
		t.Fatal("connect started while unpaired")
	}
}

// TestConnectDedupesInFlight pins that a second connect request while one is in flight is absorbed
// (the pending intent is served when the link comes up) rather than starting a parallel attempt.
func TestConnectDedupesInFlight(t *testing.T) {
	a := &app{ctx: context.Background(), paired: true, connecting: true}
	a.connect()
	if a.connCancel != nil {
		t.Fatal("connect started a second attempt while one was in flight")
	}
}

// TestEnvTruthy pins the affirmative values the SOVEREIGN_DEBUG override accepts.
func TestEnvTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "Yes", " on "} {
		if !envTruthy(v) {
			t.Errorf("envTruthy(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off", "2"} {
		if envTruthy(v) {
			t.Errorf("envTruthy(%q) = true, want false", v)
		}
	}
}

// TestDebugLogsFromSettingOrEnv pins that transcript logging is off by default and enabled by either
// the persisted setting or the SOVEREIGN_DEBUG override.
func TestDebugLogsFromSettingOrEnv(t *testing.T) {
	a := &app{}
	t.Setenv("SOVEREIGN_DEBUG", "")
	if a.debugLogs() {
		t.Fatal("debug logging is on by default")
	}
	a.settings.Debug = true
	if !a.debugLogs() {
		t.Fatal("Debug setting was not honored")
	}
	a.settings.Debug = false
	t.Setenv("SOVEREIGN_DEBUG", "1")
	if !a.debugLogs() {
		t.Fatal("SOVEREIGN_DEBUG override was not honored")
	}
}
