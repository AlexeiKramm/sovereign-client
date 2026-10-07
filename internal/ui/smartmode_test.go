package ui

import "testing"

func TestSmartModeTapTogglesOn(t *testing.T) {
	sm := &SmartMode{}
	act, arm := sm.Press()
	if act != ActionStart || !arm {
		t.Fatalf("press from idle: (%v, %v), want (Start, true)", act, arm)
	}
	// Released inside the tap window: it was a tap, so it stays on.
	act, cancel := sm.Release()
	if act != ActionNone || !cancel {
		t.Fatalf("quick release: (%v, %v), want (None, true)", act, cancel)
	}
	// Next tap while on stops it.
	act, arm = sm.Press()
	if act != ActionStop || arm {
		t.Fatalf("press while on: (%v, %v), want (Stop, false)", act, arm)
	}
	if act, _ := sm.Release(); act != ActionNone {
		t.Fatalf("release after stop: %v, want None", act)
	}
}

func TestSmartModeHoldStopsOnRelease(t *testing.T) {
	sm := &SmartMode{}
	if act, _ := sm.Press(); act != ActionStart {
		t.Fatalf("press: %v, want Start", act)
	}
	sm.TimerFired() // held past the window
	act, cancel := sm.Release()
	if act != ActionStop || cancel {
		t.Fatalf("hold release: (%v, %v), want (Stop, false)", act, cancel)
	}
	if act, _ := sm.Press(); act != ActionStart {
		t.Fatalf("press after hold ended: %v, want Start", act)
	}
}

func TestSmartModeTimerOnIdleIsNoop(t *testing.T) {
	sm := &SmartMode{}
	if act := sm.TimerFired(); act != ActionNone {
		t.Fatalf("stray timer tick = %v, want None", act)
	}
}

// TestSmartModeResetAfterExternalStop pins the fix for "I had to press the hotkey twice": when the
// phone ends a tapped-on dictation by itself (silence auto-stop, cancel, busy), the loop resets the
// machine, so the next press starts a new dictation rather than trying to stop a dead one.
func TestSmartModeResetAfterExternalStop(t *testing.T) {
	sm := &SmartMode{}
	if act, _ := sm.Press(); act != ActionStart {
		t.Fatalf("press: %v, want Start", act)
	}
	if act, _ := sm.Release(); act != ActionNone {
		t.Fatalf("tap release: %v, want None", act)
	}
	// Without a reset the machine is still "on" and the next press stops nothing.
	sm.Reset()
	act, arm := sm.Press()
	if act != ActionStart || !arm {
		t.Fatalf("press after external stop = (%v, %v), want (Start, true)", act, arm)
	}
}
