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
