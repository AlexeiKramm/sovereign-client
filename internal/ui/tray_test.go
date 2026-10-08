package ui

import (
	"context"
	"testing"
	"time"

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

// TestRequestFullResolveSetsAndKicks pins that a hotkey asks the connect manager for the full
// discovery ladder and wakes a waiting loop, without touching the network.
func TestRequestFullResolveSetsAndKicks(t *testing.T) {
	a := &app{resolveKick: make(chan struct{}, 1)}
	a.requestFullResolve()
	a.mu.Lock()
	full := a.resolveFull
	a.mu.Unlock()
	if !full {
		t.Fatal("requestFullResolve did not set resolveFull")
	}
	select {
	case <-a.resolveKick:
	case <-time.After(time.Second):
		t.Fatal("requestFullResolve did not kick the connect loop")
	}
}

// TestWaitRetryWakesOnKick pins that an explicit resolve request interrupts the reconnect backoff.
func TestWaitRetryWakesOnKick(t *testing.T) {
	a := &app{resolveKick: make(chan struct{}, 1)}
	go func() {
		time.Sleep(10 * time.Millisecond)
		a.resolveKick <- struct{}{}
	}()
	if !a.waitRetry(context.Background()) {
		t.Fatal("waitRetry returned false after a kick")
	}
}

// TestWaitRetryStopsOnCancel pins that a cancelled manager stops waiting promptly.
func TestWaitRetryStopsOnCancel(t *testing.T) {
	a := &app{resolveKick: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.waitRetry(ctx) {
		t.Fatal("waitRetry returned true after cancellation")
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
