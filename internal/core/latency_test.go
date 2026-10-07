package core

import (
	"strings"
	"testing"
)

func TestRTTMatchesOnlyKnownPings(t *testing.T) {
	r := newRTT()
	r.sent(100)
	r.sent(200)

	if _, ok := r.received(999); ok {
		t.Fatal("unsolicited PONG matched")
	}
	d, ok := r.received(100)
	if !ok || d < 0 {
		t.Fatalf("known PONG not matched: d=%v ok=%v", d, ok)
	}
	if _, ok := r.received(100); ok {
		t.Fatal("duplicate PONG matched twice")
	}
	if _, worst, samples := r.Stats(); worst < d || samples != 1 {
		t.Fatalf("Stats = worst %v samples %d; want worst>=%v samples=1", worst, samples, d)
	}
}

func TestRTTPendingIsCapped(t *testing.T) {
	r := newRTT()
	for i := int64(0); i < rttPendingCap+32; i++ {
		r.sent(i)
	}
	if got := len(r.pending); got != rttPendingCap {
		t.Fatalf("pending pings = %d, want cap %d", got, rttPendingCap)
	}
}

func TestUtteranceLineReportsTimings(t *testing.T) {
	u := newUtterance("s1")
	u.provisional()
	u.provisional()
	u.commit(1, CommitPerf{AsrMs: 120, CleanupMs: 30}, 5)
	u.commit(2, CommitPerf{AsrMs: 80, CleanupMs: 10}, 9)

	line := u.line(DoneFinal)
	for _, want := range []string{
		"session=s1", "reason=FINAL", "asr=200ms", "cleanup=40ms",
		"units=2", "committed=9", "provisional=2",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q missing %q", line, want)
		}
	}
}
