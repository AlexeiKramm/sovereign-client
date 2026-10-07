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
