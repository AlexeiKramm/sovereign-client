package core

import (
	"net"
	"strings"
	"testing"

	"github.com/hashicorp/mdns"
)

func TestParseTXT(t *testing.T) {
	m := parseTXT([]string{
		"role=server", "id=dev1", "name=Phone",
		"protover=1", "spki=abc123", "paired=1",
	})
	if m[txtRole] != "server" || m[txtSpki] != "abc123" || m[txtPaired] != "1" {
		t.Fatalf("parseTXT = %v", m)
	}
}

func TestPeerFromEntryAcceptsV1Server(t *testing.T) {
	e := &mdns.ServiceEntry{
		Name:   "Sovereign-1a2b3c4d",
		Host:   "phone.local.",
		Port:   8765,
		AddrV4: net.ParseIP("192.168.1.5"),
		InfoFields: []string{
			"role=server", "id=dev1", "name=Phone", "protover=1", "spki=SPKI", "paired=1",
		},
	}
	p, err := peerFromEntry(e)
	if err != nil {
		t.Fatalf("peerFromEntry: %v", err)
	}
	if p.Addr != "192.168.1.5:8765" {
		t.Errorf("Addr = %q, want 192.168.1.5:8765", p.Addr)
	}
	if p.DeviceID != "dev1" || p.Spki != "SPKI" || !p.Paired || p.Port != 8765 {
		t.Errorf("peer = %+v", p)
	}
}

func TestPeerFromEntryRejectsNonPhones(t *testing.T) {
	cases := map[string]string{
		"client role":     "role=client,id=x,protover=1,spki=s",
		"wrong protover":  "role=server,id=x,protover=2,spki=s",
		"missing spki":    "role=server,id=x,protover=1",
		"missing version": "role=server,id=x,spki=s",
	}
	for name, fields := range cases {
		e := &mdns.ServiceEntry{Port: 8765, AddrV4: net.ParseIP("10.0.0.1"), InfoFields: strings.Split(fields, ",")}
		if _, err := peerFromEntry(e); err == nil {
			t.Errorf("%s: expected rejection, got none", name)
		}
	}
}

func TestPeerFromEntryFallsBackToHostname(t *testing.T) {
	e := &mdns.ServiceEntry{
		Host: "phone.local.",
		Port: 8765,
		InfoFields: []string{
			"role=server", "id=dev1", "name=Phone", "protover=1", "spki=SPKI", "paired=0",
		},
	}
	p, err := peerFromEntry(e)
	if err != nil {
		t.Fatalf("peerFromEntry: %v", err)
	}
	if p.Addr != "phone.local:8765" {
		t.Errorf("Addr = %q, want phone.local:8765", p.Addr)
	}
}
