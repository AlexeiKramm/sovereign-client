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
