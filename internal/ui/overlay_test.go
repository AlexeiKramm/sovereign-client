package ui

import (
	"testing"

	"github.com/alexeikramm/sovereign-client/internal/core"
	winplat "github.com/alexeikramm/sovereign-client/internal/windows"
)

// TestAnchorPos pins the four overlay anchors against a known work area, including the DPI-scaled
// margin, so the pill lands where the user chose on any monitor scale.
func TestAnchorPos(t *testing.T) {
	work := winplat.Rect{X: 100, Y: 50, W: 1000, H: 800}
	const cw, ch = 420, 120
	const dpi = 96
	m := int(pillMargin)
	cases := []struct {
		pos  string
		want [2]int
	}{
		{PosBottomRight, [2]int{100 + 1000 - m - cw, 50 + 800 - m - ch}},
		{PosBottomLeft, [2]int{100 + m, 50 + 800 - m - ch}},
		{PosTopRight, [2]int{100 + 1000 - m - cw, 50 + m}},
		{PosTopLeft, [2]int{100 + m, 50 + m}},
	}
	for _, c := range cases {
		x, y := anchorPos(work, cw, ch, c.pos, dpi)
		if x != c.want[0] || y != c.want[1] {
			t.Errorf("anchorPos(%s) = (%d,%d), want (%d,%d)", c.pos, x, y, c.want[0], c.want[1])
		}
	}
}

// TestAnchorPosScalesMargin checks that the gap scales with the monitor DPI while the work area is
// already in physical pixels.
func TestAnchorPosScalesMargin(t *testing.T) {
	work := winplat.Rect{X: 0, Y: 0, W: 1000, H: 800}
	x96, _ := anchorPos(work, 420, 120, PosTopLeft, 96)
	x192, _ := anchorPos(work, 420, 120, PosTopLeft, 192)
	if x192 <= x96 {
		t.Fatalf("margin did not grow with DPI: 96dpi x=%d, 192dpi x=%d", x96, x192)
	}
}

// TestCancelDictationHidesPillImmediately pins the removal of the Esc latency: the pill is local
// feedback, so cancelling must drop it at once rather than waiting for the phone's SESSION_RELEASED
// round-trip, and must clear the dictating/smart-mode state so the next press starts fresh.
func TestCancelDictationHidesPillImmediately(t *testing.T) {
	a := &app{logf: func(string, ...any) {}}
	a.connState = core.StateReady
	a.pill = pillView{status: "Listening…", shown: true}
	a.dictating = true
	a.cancelDictation()
	if a.pill.shown {
		t.Fatal("Esc must hide the pill immediately, not wait for SESSION_RELEASED")
	}
	if a.isDictating() {
		t.Fatal("Esc must clear the dictating flag immediately")
	}
}

// TestCancelDictationAbandonsPending checks Esc on the deferred "Connecting…" pill drops it and
// clears the pending intent so the link coming up later does not start a dictation the user bailed on.
func TestCancelDictationAbandonsPending(t *testing.T) {
	a := &app{logf: func(string, ...any) {}}
	a.pending = true
	a.pendingTest = true
	a.pill = pillView{status: "Connecting…", shown: true}
	a.cancelDictation()
	if a.pill.shown {
		t.Fatal("Esc must hide the connecting pill")
	}
	if a.pending || a.pendingTest {
		t.Fatalf("pending intent not cleared: pending=%v test=%v", a.pending, a.pendingTest)
	}
}

// TestHideListeningPillPreservesFeedbackCapsule tests that normal idle/stopped state transitions
// do not prematurely dismiss the "Inserted X words" feedback capsule.
func TestHideListeningPillPreservesFeedbackCapsule(t *testing.T) {
	a := &app{}

	// Listening state: hideListeningPill should hide it
	a.pill = pillView{status: "Listening…", shown: true}
	a.hideListeningPill()
	if a.pill.shown {
		t.Fatalf("expected listening pill to be hidden")
	}

	// Inserted feedback state: hideListeningPill must NOT hide it
	a.pill = pillView{insertedWords: 12, shown: true}
	a.hideListeningPill()
	if !a.pill.shown || a.pill.insertedWords != 12 {
		t.Fatalf("expected inserted feedback pill to stay visible, shown=%v words=%d", a.pill.shown, a.pill.insertedWords)
	}

	// Error state: hideListeningPill must NOT hide it
	a.pill = pillView{status: "Phone offline", shown: true, err: true}
	a.hideListeningPill()
	if !a.pill.shown || !a.pill.err {
		t.Fatalf("expected error pill to stay visible, shown=%v err=%v", a.pill.shown, a.pill.err)
	}

	// Explicit hidePill: should hide regardless
	a.hidePill()
	if a.pill.shown {
		t.Fatalf("expected hidePill to dismiss the pill")
	}
}
