package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// ErrNoPeer means no v1 Sovereign phone answered the mDNS browse before the timeout. It is not
// fatal: manual host:port is the first-class fallback (PROTOCOL.md §2).
var ErrNoPeer = errors.New("no Sovereign phone found on the LAN")

// mdnsQueryService is ServiceType without its ".local" suffix: hashicorp/mdns appends the domain
// itself, forming "_sovereign._tcp.local.".
const mdnsQueryService = "_sovereign._tcp"

// TXT record keys (PROTOCOL.md §2), matching the phone's ServerDiscovery.txtAttributes.
const (
	txtRole     = "role"
	txtID       = "id"
	txtName     = "name"
	txtProtover = "protover"
	txtSpki     = "spki"
	txtPaired   = "paired"
)

// Peer is one phone discovered over mDNS (PROTOCOL.md §2): the SRV record plus the TXT keys.
type Peer struct {
	Instance string // mDNS instance, e.g. "Sovereign-1a2b3c4d"
	DeviceID string // phone deviceId from TXT `id`
	Name     string // human label from TXT `name`
	Protover int    // TXT `protover`
	Spki     string // TXT `spki` — the fingerprint to pin
	Paired   bool   // TXT `paired` — hint only
	IP       string // resolved IPv4/IPv6, may be empty
	Host     string // mDNS host name (no trailing dot), used when no IP resolved
	Port     int    // SRV port
	Addr     string // host:port to dial
}

// parseTXT turns mDNS `InfoFields` ("k=v") into a map; a repeated key keeps the last value.
func parseTXT(fields []string) map[string]string {
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		if i := strings.IndexByte(f, '='); i >= 0 {
			m[f[:i]] = f[i+1:]
		}
	}
	return m
}

// peerFromEntry builds a Peer from one mDNS service entry, returning an error for anything that is
// not a v1 phone (wrong role, wrong/absent protover or spki) so callers can skip it silently.
func peerFromEntry(e *mdns.ServiceEntry) (Peer, error) {
	txt := parseTXT(e.InfoFields)
	if txt[txtRole] != "server" {
		return Peer{}, fmt.Errorf("not a server (role=%q)", txt[txtRole])
	}
	ver, err := strconv.Atoi(txt[txtProtover])
	if err != nil {
		return Peer{}, fmt.Errorf("bad protover %q", txt[txtProtover])
	}
	if ver != Version {
		return Peer{}, fmt.Errorf("protover %d != %d", ver, Version)
	}
	if txt[txtSpki] == "" {
		return Peer{}, errors.New("missing spki TXT key")
	}
	if e.Port == 0 {
		return Peer{}, errors.New("missing SRV port")
	}

	ip := ""
	switch {
	case e.AddrV4 != nil:
		ip = e.AddrV4.String()
	case e.AddrV6IPAddr != nil:
		ip = e.AddrV6IPAddr.String()
	}
	host := strings.TrimSuffix(e.Host, ".")
	dialHost := ip
	if dialHost == "" {
		dialHost = host
	}
	return Peer{
		Instance: e.Name,
		DeviceID: txt[txtID],
		Name:     txt[txtName],
		Protover: ver,
		Spki:     txt[txtSpki],
		Paired:   txt[txtPaired] == "1",
		IP:       ip,
		Host:     host,
		Port:     e.Port,
		Addr:     net.JoinHostPort(dialHost, strconv.Itoa(e.Port)),
	}, nil
}

// Browse discovers phones via mDNS and returns the first v1 server it finds, or ErrNoPeer. mDNS is
// blocked by AP/client isolation and misrouted by VPNs, so a negative result must never be a dead
// end — the caller falls back to a manual host:port.
func Browse(ctx context.Context, timeout time.Duration) (Peer, error) {
	if timeout <= 0 {
		timeout = time.Second
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *mdns.ServiceEntry, 16)
	go func() {
		defer close(entries)
		_ = mdns.QueryContext(qctx, &mdns.QueryParam{
			Service: mdnsQueryService,
			Domain:  "local",
			Timeout: timeout,
			Entries: entries,
		})
	}()

	for {
		select {
		case <-ctx.Done():
			return Peer{}, ctx.Err()
		case e, ok := <-entries:
			if !ok {
				return Peer{}, ErrNoPeer
			}
			if p, err := peerFromEntry(e); err == nil {
				return p, nil
			}
		}
	}
}
