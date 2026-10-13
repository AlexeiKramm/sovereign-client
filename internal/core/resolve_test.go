package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReachableRecognisesOurServer pins that the ladder's first rung accepts a Sovereign server.
func TestReachableRecognisesOurServer(t *testing.T) {
	server := fakePhone(t, nil)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !Reachable(ctx, hostOf(t, server.URL)) {
		t.Fatal("Reachable must accept a Sovereign server")
	}
}

// TestReachableRejectsForeignServer pins that the hint rung does not mistake an unrelated TLS host
// for the phone.
func TestReachableRejectsForeignServer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if Reachable(ctx, hostOf(t, server.URL)) {
		t.Fatal("Reachable must reject a non-Sovereign TLS server")
	}
}

// TestResolvePrefersRememberedAddress pins the first rung of the ladder: a live remembered address
// is returned before mDNS or the sweep are ever attempted.
func TestResolvePrefersRememberedAddress(t *testing.T) {
	server := fakePhone(t, nil)
	defer server.Close()
	addr := hostOf(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := Resolve(ctx, ResolveOptions{Hint: addr})
	if err != nil {
		t.Fatalf("Resolve(hint) = %v", err)
	}
	if p.Via != ViaHint {
		t.Fatalf("Resolve Via = %q, want %q", p.Via, ViaHint)
	}
	if p.Addr != addr {
		t.Fatalf("Resolve Addr = %q, want %q", p.Addr, addr)
	}
}

// TestResolvePrefersLiveAddressFromMultipleHints pins that when multiple remembered hints are provided,
// the live address is returned promptly even if dead addresses are also in the hint pool.
func TestResolvePrefersLiveAddressFromMultipleHints(t *testing.T) {
	server := fakePhone(t, nil)
	defer server.Close()
	liveAddr := hostOf(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Dead hint first, then live hint, then another dead hint
	hints := []string{"192.0.2.1:8765", liveAddr, "192.0.2.2:8765"}
	p, err := Resolve(ctx, ResolveOptions{Hints: hints})
	if err != nil {
		t.Fatalf("Resolve(hints) = %v", err)
	}
	if p.Via != ViaHint {
		t.Fatalf("Resolve Via = %q, want %q", p.Via, ViaHint)
	}
	if p.Addr != liveAddr {
		t.Fatalf("Resolve Addr = %q, want %q", p.Addr, liveAddr)
	}
}

func TestProbeHintsAllDead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addr, ok := ProbeHints(ctx, []string{"192.0.2.1:8765", "192.0.2.2:8765"})
	if ok || addr != "" {
		t.Fatalf("ProbeHints(all dead) = (%q, %v), want (\"\", false)", addr, ok)
	}
}

func TestProbeHintsContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	addr, ok := ProbeHints(ctx, []string{"192.0.2.1:8765", "192.0.2.2:8765"})
	if ok || addr != "" {
		t.Fatalf("ProbeHints(cancelled) = (%q, %v), want (\"\", false)", addr, ok)
	}
}

func TestCollectHints(t *testing.T) {
	opts := ResolveOptions{
		Hint:  " 10.0.0.1:8765 ",
		Hints: []string{"10.0.0.2:8765", "  ", "10.0.0.1:8765", "", "10.0.0.3:8765"},
	}
	got := collectHints(opts)
	want := []string{"10.0.0.2:8765", "10.0.0.1:8765", "10.0.0.3:8765"}
	if len(got) != len(want) {
		t.Fatalf("collectHints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("collectHints[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
