package core

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsVirtualInterface(t *testing.T) {
	virtual := []string{
		"Tailscale", "vEthernet (WSL (Hyper-V firewall))", "VMware Network Adapter VMnet1",
		"VirtualBox Host-Only Network", "Docker0", "WireGuard Tunnel", "wg0", "tun1", "utun3",
		"Teredo Tunneling Pseudo-Interface", "Bluetooth Network Connection", "vgate0",
	}
	for _, n := range virtual {
		if !isVirtualInterface(n) {
			t.Errorf("%q should be treated as virtual", n)
		}
	}
	physical := []string{"Ethernet 4", "WiFi 2", "en0", "eth0", "wlan0", "Local Area Connection"}
	for _, n := range physical {
		if isVirtualInterface(n) {
			t.Errorf("%q should be treated as physical", n)
		}
	}
}

func TestProbePortFindsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	want := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	got := ProbePort(context.Background(), []string{"127.0.0.1"}, port, time.Second)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ProbePort = %v, want [%s]", got, want)
	}
}

func TestProbePortReturnsNothingWhenClosed(t *testing.T) {
	got := ProbePort(context.Background(), []string{"127.0.0.1"}, 1, 200*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("ProbePort on a closed port = %v, want none", got)
	}
}

func TestIdentifySovereignRecognisesOurServer(t *testing.T) {
	server := fakePhone(t, nil)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !IdentifySovereign(ctx, hostOf(t, server.URL)) {
		t.Fatal("a Sovereign server must be identified")
	}
}

func TestIdentifySovereignRejectsForeignServer(t *testing.T) {
	// A TLS server that upgrades nothing: it must not be mistaken for the phone.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if IdentifySovereign(ctx, hostOf(t, server.URL)) {
		t.Fatal("a non-Sovereign TLS server must not be identified")
	}
}

func TestLocalProbeHostsShape(t *testing.T) {
	for _, h := range LocalProbeHosts() {
		if net.ParseIP(h) == nil {
			t.Fatalf("LocalProbeHosts returned a non-IP %q", h)
		}
		if strings.HasSuffix(h, ".0") || strings.HasSuffix(h, ".255") {
			t.Fatalf("LocalProbeHosts scanned network/broadcast address %q", h)
		}
	}
}
