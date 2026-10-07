package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// probePerHostTimeout bounds one TCP connect during a scan.
	probePerHostTimeout = 350 * time.Millisecond
	// probeConcurrency caps simultaneous connects: fast on a /24, but not abusive.
	probeConcurrency = 64
	// probeMaxHosts caps the total number of addresses scanned.
	probeMaxHosts = 2048
)

// LocalProbeHosts returns the candidate addresses for the probe fallback: for every physical IPv4
// address on this host, the other 254 addresses of that /24. Virtual adapters are already excluded
// by localIPv4Interfaces.
func LocalProbeHosts() []string {
	var hosts []string
	seen := make(map[string]bool)
	for _, ifc := range localIPv4Interfaces() {
		ipStr := ipv4Of(&ifc)
		if ipStr == "" {
			continue
		}
		ip := net.ParseIP(ipStr).To4()
		if ip == nil {
			continue
		}
		base := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8
		for i := 1; i <= 254; i++ {
			v := base | uint32(i)
			s := fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
			if !seen[s] {
				seen[s] = true
				hosts = append(hosts, s)
			}
			if len(hosts) >= probeMaxHosts {
				return hosts
			}
		}
	}
	return hosts
}

// ProbePort returns "ip:port" for every host in hosts that accepts a TCP connection on port. The
// result is only ever a *candidate*: 8765 being open does not make a host the phone, so the caller
// confirms each one with the real pinned-TLS handshake (IdentifySovereign).
func ProbePort(ctx context.Context, hosts []string, port int, perHost time.Duration) []string {
	if perHost <= 0 {
		perHost = probePerHostTimeout
	}
	sem := make(chan struct{}, probeConcurrency)
	var mu sync.Mutex
	var found []string
	var wg sync.WaitGroup
	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			c, err := net.DialTimeout("tcp", addr, perHost)
			if err != nil {
				return
			}
			_ = c.Close()
			mu.Lock()
			found = append(found, addr)
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	sort.Strings(found)
	return found
}

// IdentifySovereign reports whether addr speaks the v1 protocol. It completes the self-signed TLS
// handshake and sends a HELLO with an empty token, which any Sovereign server answers with
// AUTH_FAIL — proof it is our server, and (unlike PAIR_HELLO) without raising a pairing dialog.
func IdentifySovereign(ctx context.Context, addr string) bool {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: TLSConfig("", nil)}}
	ws, _, err := websocket.Dial(ctx, "wss://"+addr+Path, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return false
	}
	defer ws.CloseNow()
	ws.SetReadLimit(MaxFrameBytes)

	if err := writeMsg(ctx, ws, mustMessageType("HELLO"), &HelloPayload{
		Token: "", DeviceID: newUUID(), Protover: Version,
	}); err != nil {
		return false
	}
	m, err := readMsg(ctx, ws)
	if err != nil {
		return false
	}
	switch m.Payload.(type) {
	case *WelcomePayload, *AuthFailPayload:
		return true
	}
	return false
}

// Discover finds the phone: mDNS first (standard, cheap, and does no scanning), then a unicast TCP
// probe of the local subnets confirmed by IdentifySovereign. The probe exists because mDNS
// multicast is routinely blocked between a wired host and a Wi-Fi phone (AP isolation / IGMP
// snooping) and is misrouted when a VPN is the phone's default network — both cases PROTOCOL.md §2
// names, and both of which make mDNS a dead end on their own.
func Discover(ctx context.Context, timeout time.Duration) (Peer, error) {
	if p, err := Browse(ctx, timeout); err == nil {
		return p, nil
	} else if ctx.Err() != nil {
		return Peer{}, ctx.Err()
	}

	for _, addr := range ProbePort(ctx, LocalProbeHosts(), DefaultPort, probePerHostTimeout) {
		if ctx.Err() != nil {
			return Peer{}, ctx.Err()
		}
		if IdentifySovereign(ctx, addr) {
			return Peer{Addr: addr, Port: DefaultPort, Via: ViaProbe}, nil
		}
	}
	return Peer{}, ErrNoPeer
}
