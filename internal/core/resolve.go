package core

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ViaHint is discovery provenance for the remembered address of a previous successful connection.
// It is the first rung of Resolve's ladder (PROTOCOL.md §2). ViaMDNS and ViaProbe live in
// discovery.go and probe.go respectively.
const ViaHint = "hint"

// Ladder timings. The hint probe and the mDNS browse are deliberately short: the ladder runs on the
// dictation hotkey path, where a multi-second stall before the phone starts is unacceptable. Only
// the final sweep is allowed to take a couple of seconds, and it runs last.
const (
	// HintProbeTimeout bounds the first rung: is the remembered address still our phone?
	HintProbeTimeout = 1500 * time.Millisecond
	// MDNSTimeout bounds the mDNS browse rung when none is supplied.
	MDNSTimeout = 1200 * time.Millisecond
)

// ResolveOptions parameterises the discovery ladder.
type ResolveOptions struct {
	// Hint is the last-known host:port to try first; empty skips the rung.
	Hint string
	// Hints is the list of remembered host:ports to probe concurrently on the hint rung; empty falls back to Hint.
	Hints []string
	// MDNSTimeout overrides the mDNS browse budget; zero uses MDNSTimeout.
	MDNSTimeout time.Duration
	// Logf, if non-nil, receives one line per rung outcome.
	Logf func(format string, args ...any)
}

// Reachable reports whether addr answers as a Sovereign server within HintProbeTimeout. It is the
// cheap, silent first rung: a single host, no mDNS, no sweep.
func Reachable(ctx context.Context, addr string) bool {
	hctx, cancel := context.WithTimeout(ctx, HintProbeTimeout)
	defer cancel()
	return IdentifySovereign(hctx, addr)
}

// ProbeHints probes multiple candidate addresses concurrently within HintProbeTimeout.
// It returns the first address that successfully identifies as a Sovereign server.
func ProbeHints(ctx context.Context, hints []string) (string, bool) {
	var valid []string
	seen := make(map[string]bool)
	for _, h := range hints {
		h = strings.TrimSpace(h)
		if h != "" && !seen[h] {
			seen[h] = true
			valid = append(valid, h)
		}
	}
	if len(valid) == 0 {
		return "", false
	}
	if len(valid) == 1 {
		if Reachable(ctx, valid[0]) {
			return valid[0], true
		}
		return "", false
	}

	hctx, cancel := context.WithTimeout(ctx, HintProbeTimeout)
	defer cancel()

	result := make(chan string, 1)
	var wg sync.WaitGroup

	for _, h := range valid {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if IdentifySovereign(hctx, addr) {
				select {
				case result <- addr:
					cancel()
				default:
				}
			}
		}(h)
	}

	go func() {
		wg.Wait()
		close(result)
	}()

	select {
	case <-ctx.Done():
		return "", false
	case addr, ok := <-result:
		if ok && addr != "" {
			return addr, true
		}
		return "", false
	}
}

func collectHints(opts ResolveOptions) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, h := range opts.Hints {
		add(h)
	}
	if opts.Hint != "" {
		add(opts.Hint)
	}
	return out
}

// Resolve finds the phone using the on-demand ladder: the remembered address, then a short mDNS
// browse, then the full-subnet TCP sweep (PROTOCOL.md §2). It is called only when the user asks for a
// connection — never on a timer — so an idle client never scans the network. A negative result is
// ErrNoPeer, never a dead end.
func Resolve(ctx context.Context, opts ResolveOptions) (Peer, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	mdnsTimeout := opts.MDNSTimeout
	if mdnsTimeout <= 0 {
		mdnsTimeout = MDNSTimeout
	}

	hints := collectHints(opts)
	if len(hints) > 0 {
		if addr, ok := ProbeHints(ctx, hints); ok {
			logf("discovery: remembered address %s answered", addr)
			return Peer{Addr: addr, Port: DefaultPort, Via: ViaHint}, nil
		}
		if ctx.Err() != nil {
			return Peer{}, ctx.Err()
		}
		if len(hints) == 1 {
			logf("discovery: remembered address %s silent; trying mDNS", hints[0])
		} else {
			logf("discovery: remembered addresses %v silent; trying mDNS", hints)
		}
	}

	if p, err := Browse(ctx, mdnsTimeout); err == nil {
		logf("discovery: mDNS found %s", p.Addr)
		return p, nil
	} else if ctx.Err() != nil {
		return Peer{}, ctx.Err()
	}
	logf("discovery: mDNS found nothing; sweeping the local subnet")

	for _, addr := range ProbePort(ctx, LocalProbeHosts(), DefaultPort, probePerHostTimeout) {
		if ctx.Err() != nil {
			return Peer{}, ctx.Err()
		}
		if IdentifySovereign(ctx, addr) {
			logf("discovery: sweep found %s", addr)
			return Peer{Addr: addr, Port: DefaultPort, Via: ViaProbe}, nil
		}
	}
	return Peer{}, ErrNoPeer
}
