package ui

import (
	"bytes"
	"image"
	"testing"
	"time"

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

// TestBlendImages pins the cross-dissolve: t=0 is the from frame, t=1 the to frame, and the midpoint
// is their rounded average, so a state change dissolves rather than fading through a blank.
func TestBlendImages(t *testing.T) {
	from := image.NewRGBA(image.Rect(0, 0, 2, 1))
	from.Pix = []byte{0, 0, 0, 0, 200, 100, 50, 255}
	to := image.NewRGBA(image.Rect(0, 0, 2, 1))
	to.Pix = []byte{100, 200, 50, 255, 0, 0, 0, 0}

	if got := blendImages(from, to, 0); !bytes.Equal(got.Pix, from.Pix) {
		t.Fatalf("t=0 blend = %v, want the from frame %v", got.Pix, from.Pix)
	}
	if got := blendImages(from, to, 1); !bytes.Equal(got.Pix, to.Pix) {
		t.Fatalf("t=1 blend = %v, want the to frame %v", got.Pix, to.Pix)
	}
	want := []byte{50, 100, 25, 128, 100, 50, 25, 128}
	if got := blendImages(from, to, 0.5); !bytes.Equal(got.Pix, want) {
		t.Fatalf("t=0.5 blend = %v, want %v", got.Pix, want)
	}
}

// TestFadeTimings pins the chosen motion (the Balanced preset from the animation lab): the fade-in is
// snappier than the fade-out, and the step is positive.
func TestFadeTimings(t *testing.T) {
	if fadeInDuration >= fadeOutDuration {
		t.Fatalf("fade-in (%v) must be snappier than fade-out (%v)", fadeInDuration, fadeOutDuration)
	}
	if fadeStep <= 0 || fadeStep > fadeInDuration {
		t.Fatalf("fadeStep %v is not a sane step for the fade-in %v", fadeStep, fadeInDuration)
	}
	if crossfadeDuration <= 0 {
		t.Fatal("crossfadeDuration must be positive")
	}
}

// TestFadeOutCompletesToZero verifies that fadeOutPill animates monotonically down to zero and stays there.
func TestFadeOutCompletesToZero(t *testing.T) {
	a := &app{logf: func(string, ...any) {}}
	a.pillAlpha = 255
	a.pill = pillView{status: "Listening…", shown: false}

	a.fadeOutPill()

	// Wait for the animation plus margin.
	time.Sleep(fadeOutDuration + 50*time.Millisecond)

	a.pillMu.Lock()
	alpha := a.pillAlpha
	a.pillMu.Unlock()

	if alpha != 0 {
		t.Fatalf("expected alpha to settle at 0, got %d", alpha)
	}
}

// TestFadeRapidInterruptionSettlesCleanly tests rapid interrupts between fading in and fading out,
// ensuring the final fade-out wins and settles at alpha 0.
func TestFadeRapidInterruptionSettlesCleanly(t *testing.T) {
	a := &app{logf: func(string, ...any) {}}
	a.pillAlpha = 0
	a.pill = pillView{status: "Listening…", shown: true}

	// Trigger a fade in, then rapidly trigger fade out after 30ms.
	a.fadePillTo(255, fadeInDuration)
	time.Sleep(30 * time.Millisecond)
	a.pill.shown = false
	a.fadeOutPill()

	// Wait for fade-out to finish.
	time.Sleep(fadeOutDuration + 60*time.Millisecond)

	a.pillMu.Lock()
	alpha := a.pillAlpha
	a.pillMu.Unlock()

	if alpha != 0 {
		t.Fatalf("expected interrupted fade-out to settle cleanly at alpha 0, got %d", alpha)
	}
}
