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

// ErrNoPeer means no v1 Sovereign phone answered before the timeout. It is not fatal: manual
// host:port and the TCP probe are the fallbacks (PROTOCOL.md §2).
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

// How a Peer was found.
const (
	ViaMDNS  = "mdns"
	ViaProbe = "probe"
)

// Peer is one phone discovered on the LAN (PROTOCOL.md §2): the SRV record plus the TXT keys.
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
	Via      string // ViaMDNS or ViaProbe
}

// virtualInterfaceHints are name fragments of adapters that must never carry LAN discovery: VPNs,
// hypervisor bridges and container networks either never reach the LAN or would route the query
// off it (PROTOCOL.md §2 explicitly cites VPN / multi-NIC hosts as a discovery hazard).
var virtualInterfaceHints = []string{
	"tailscale", "wsl", "vethernet", "hyper-v", "vmware", "virtualbox", "vbox",
	"docker", "zerotier", "wireguard", "wintun", "vgate", "teredo", "isatap",
	"bluetooth", "npcap", "loopback", "tun", "tap", "wg",
}

// isVirtualInterface reports whether an interface name looks like a VPN/virtual adapter.
func isVirtualInterface(name string) bool {
	n := strings.ToLower(name)
	for _, h := range virtualInterfaceHints {
		if strings.Contains(n, h) {
			return true
		}
	}
	return false
}

// localIPv4Interfaces returns up, non-loopback, non-virtual interfaces that hold a usable IPv4
// address (never a link-local 169.254 address).
func localIPv4Interfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifc := range all {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(ifc.Name) {
			continue
		}
		if ipv4Of(&ifc) == "" {
			continue
		}
		out = append(out, ifc)
	}
	return out
}

// ipv4Of returns the first usable IPv4 address of an interface, or "".
func ipv4Of(ifc *net.Interface) string {
	addrs, err := ifc.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipn.IP.To4()
		if ip4 == nil || ip4.IsLinkLocalUnicast() {
			continue
		}
		return ip4.String()
	}
	return ""
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
	if dialHost == "" || strings.HasPrefix(dialHost, "fe80:") {
		// Prefer IPv4 if host resolves to an IPv4 address on the LAN
		if host != "" {
			if ips, err := net.LookupIP(host); err == nil {
				for _, ipCandidate := range ips {
					if ip4 := ipCandidate.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
						dialHost = ip4.String()
						ip = dialHost
						break
					}
				}
			}
		}
	}
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

// Browse discovers phones over mDNS. It queries each physical interface in turn (virtual adapters
// are filtered out, per the plan's "bind all NICs; filter virtual adapters") and returns the first
// v1 server it hears. A negative result returns ErrNoPeer, never a dead end.
func Browse(ctx context.Context, timeout time.Duration) (Peer, error) {
	if timeout <= 0 {
		timeout = time.Second
	}
	ifaces := localIPv4Interfaces()
	if len(ifaces) == 0 {
		// No usable physical NIC: fall back to the OS default so discovery is still attempted.
		return browseInterface(ctx, nil, timeout)
	}
	per := timeout / time.Duration(len(ifaces))
	if per < 500*time.Millisecond {
		per = 500 * time.Millisecond
	}
	for i := range ifaces {
		p, err := browseInterface(ctx, &ifaces[i], per)
		if err == nil {
			p.Via = ViaMDNS
			return p, nil
		}
		if ctx.Err() != nil {
			return Peer{}, ctx.Err()
		}
	}
	return Peer{}, ErrNoPeer
}

// browseInterface runs one mDNS query on a single interface (nil = the OS default).
func browseInterface(ctx context.Context, iface *net.Interface, timeout time.Duration) (Peer, error) {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *mdns.ServiceEntry, 16)
	go func() {
		defer close(entries)
		_ = mdns.QueryContext(qctx, &mdns.QueryParam{
			Service:   mdnsQueryService,
			Domain:    "local",
			Timeout:   timeout,
			Interface: iface,
			Entries:   entries,
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
