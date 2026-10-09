package core

import (
	"context"
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

	if opts.Hint != "" {
		if Reachable(ctx, opts.Hint) {
			logf("discovery: remembered address %s answered", opts.Hint)
			return Peer{Addr: opts.Hint, Port: DefaultPort, Via: ViaHint}, nil
		}
		if ctx.Err() != nil {
			return Peer{}, ctx.Err()
		}
		logf("discovery: remembered address %s silent; trying mDNS", opts.Hint)
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
