package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	// HeartbeatInterval is how often the client sends PING while a dictation session is in flight
	// (PROTOCOL.md §1/§9).
	HeartbeatInterval = 5 * time.Second
	// HeartbeatTimeout is how long the client waits for any inbound frame before it treats a
	// dictating link as dead (PROTOCOL.md §1/§9).
	HeartbeatTimeout = 15 * time.Second
	// IdleHeartbeatInterval is the PING cadence on an authenticated but idle link. The desktop keeps
	// the link open while connected, but relaxes the tick so an open, unused socket does not wake the
	// phone's radio every 5 s (D-131).
	IdleHeartbeatInterval = 30 * time.Second
	// IdleHeartbeatTimeout is the dead-link timeout on an idle link. It must exceed
	// IdleHeartbeatInterval so a single late PONG does not kill a healthy link, and it sits well
	// under the phone's own 90 s idle reap.
	IdleHeartbeatTimeout = 60 * time.Second

	dialTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
)

// ErrNotConnected is returned by command methods when no authenticated link is up.
var ErrNotConnected = errors.New("not connected to the phone")

// AuthError is a non-recoverable rejection of this client's identity (AUTH_FAIL, or a fatal
// ERROR before/at authentication). Reconnecting cannot fix it; the user must pair again.
type AuthError struct{ Reason string }

func (e *AuthError) Error() string { return "authentication failed: " + e.Reason }

// ConnState is the transport's coarse state, mapped by the tray (Chunk 4) to an icon.
type ConnState string

const (
	StateDisconnected ConnState = "DISCONNECTED"
	StateConnecting   ConnState = "CONNECTING"
	StateReady        ConnState = "READY"
	StateAuthFailed   ConnState = "AUTH_FAILED"
)

// Handlers receives connection and session events. Every field is optional. Callbacks are
// invoked from the client's reader goroutine and MUST NOT block: hand off to a queue instead.
type Handlers struct {
	OnConn        func(state ConnState, detail string)
	OnStarted     func(sessionID string)
	OnBusy        func(holderName string, canTakeover bool)
	OnServerState func(state ServerStatePayload)
	OnState       func(sessionID, state string)
	OnCommitted   func(sessionID, text string, perf CommitPerf)
	OnProvisional func(sessionID, text string)
	OnUtterance   func(sessionID, deliveryText, rawText, reason string)
	OnReleased    func(sessionID, reason string)
	OnError       func(code, message string, fatal bool)
	OnLatency     func(line string)
}

// ClientConfig parameterises the connection to one phone.
type ClientConfig struct {
	Addr     string // phone host:port (from the mDNS SRV record, or manual)
	Pin      string // expected server SPKI fingerprint; "" only during first-time pairing
	Ident    Identity
	Creds    Credentials
	Handlers Handlers
	Logf     func(format string, args ...any)
}

// Client owns one logical link to a phone: it dials, authenticates, heartbeats, and dispatches
// session frames to Handlers. A drop ends the link; the caller decides whether to connect again.
// It is safe for concurrent command calls.
type Client struct {
	cfg    ClientConfig
	tlsCfg *tls.Config

	mu        sync.Mutex
	live      *liveConn
	state     ConnState
	sessionID string
}

// liveConn is one authenticated WebSocket connection. Its mutable state is touched only by the
// reader goroutine except writeMu/rtt/lastRx, which are synchronised.
type liveConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	rtt     *RTT
	lastRx  atomic.Int64 // time.Now().UnixNano of the last inbound frame
	utt     *Utterance   // current utterance timing; reader-goroutine confined
}

func (lc *liveConn) touch()                { lc.lastRx.Store(time.Now().UnixNano()) }
func (lc *liveConn) lastRxTime() time.Time { return time.Unix(0, lc.lastRx.Load()) }

// NewClient builds a client for cfg. The TLS pin is fixed for the client's lifetime.
func NewClient(cfg ClientConfig) *Client {
	c := &Client{cfg: cfg}
	c.tlsCfg = TLSConfig(cfg.Pin, func(fp string) {
		if cfg.Pin == "" {
			c.logf("learn mode: observed server SPKI %s", fp)
		}
	})
	return c
}

// Run makes one connection attempt, serves the link until it drops, and returns. It does NOT
// reconnect: the desktop is connect-on-demand (D-131), so a drop returns to Idle and the next hotkey
// drives the next attempt. It returns ctx.Err() on shutdown, an *AuthError if the phone rejects this
// client's identity (a re-pair, not a retry), ErrPinMismatch on a possible MITM, or the transport
// error when an established link drops.
func (c *Client) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.setState(StateConnecting, "")
	lc, err := c.connect(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ae *AuthError
		switch {
		case errors.As(err, &ae):
			c.setState(StateAuthFailed, err.Error())
		// A pin mismatch is a possible MITM: surface it, never retry silently (§1/§10).
		case errors.Is(err, ErrPinMismatch):
			c.setState(StateDisconnected, err.Error())
		default:
			c.setState(StateDisconnected, err.Error())
			c.logf("connect failed: %v", err)
		}
		return err
	}

	c.setLive(lc)
	c.setState(StateReady, "")
	err = c.readLoop(ctx, lc)
	c.setLive(nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.setState(StateDisconnected, errString(err))
	c.logf("link down: %v", err)
	return err
}

// StartDictation asks the phone to begin a session; the reply arrives as OnStarted or OnBusy.
func (c *Client) StartDictation(ctx context.Context, mode string) error {
	return c.writeTo(ctx, "START_DICTATION", &StartDictationPayload{RequestID: newUUID(), Mode: mode})
}

// StopDictation ends the active session; the phone answers with UTTERANCE_DONE (the injection trigger).
func (c *Client) StopDictation(ctx context.Context) error {
	id, err := c.requireSession()
	if err != nil {
		return err
	}
	return c.writeTo(ctx, "STOP_DICTATION", &StopDictationPayload{SessionID: id})
}

// CancelDictation discards the active session; per PROTOCOL.md §6 it yields no UTTERANCE_DONE.
func (c *Client) CancelDictation(ctx context.Context) error {
	id, err := c.requireSession()
	if err != nil {
		return err
	}
	return c.writeTo(ctx, "CANCEL_DICTATION", &CancelDictationPayload{SessionID: id})
}

// WebSocketURL constructs a "wss://" URL from a host:port address and a path, properly
// escaping IPv6 zone identifiers (RFC 6874) so that url.Parse does not fail on zone indices
// like "%Ethernet 4" or "%Wi-Fi".
func WebSocketURL(addr, path string) string {
	idx := strings.Index(addr, "%")
	if idx < 0 {
		return "wss://" + addr + path
	}
	end := strings.Index(addr, "]")
	if end < 0 || idx >= end {
		return "wss://" + addr + path
	}
	zone := addr[idx+1 : end]
	escapedZone := url.QueryEscape(zone)
	return "wss://" + addr[:idx] + "%25" + escapedZone + addr[end:] + path
}

func (c *Client) connect(ctx context.Context) (*liveConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: c.tlsCfg}}
	url := WebSocketURL(c.cfg.Addr, Path)
	ws, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	ws.SetReadLimit(MaxFrameBytes)

	lc := &liveConn{ws: ws, rtt: newRTT()}
	lc.touch()
	if err := c.write(ctx, lc, mustMessageType("HELLO"), &HelloPayload{
		Token:        c.cfg.Creds.Token,
		DeviceID:     c.cfg.Ident.DeviceID,
		Protover:     Version,
		Capabilities: clientCapabilities,
	}); err != nil {
		ws.CloseNow()
		return nil, fmt.Errorf("send HELLO: %w", err)
	}
	if err := c.awaitWelcome(ctx, lc); err != nil {
		ws.CloseNow()
		return nil, err
	}
	return lc, nil
}

// awaitWelcome reads frames until the phone answers HELLO with WELCOME, or refuses it.
func (c *Client) awaitWelcome(ctx context.Context, lc *liveConn) error {
	for {
		_, data, err := lc.ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("handshake read: %w", err)
		}
		lc.touch()
		switch r := Decode(data).(type) {
		case Decoded:
			switch p := r.Message.Payload.(type) {
			case *WelcomePayload:
				if p.Protover != Version {
					return &AuthError{Reason: fmt.Sprintf("server protover %d != %d", p.Protover, Version)}
				}
				c.logf("ready: %q (device %s, maxFrameBytes=%d)", p.ServerName, p.DeviceID, p.MaxFrameBytes)
				return nil
			case *AuthFailPayload:
				return &AuthError{Reason: p.Reason}
			case *ErrorPayload:
				if p.Fatal {
					return &AuthError{Reason: p.Code + ": " + p.Message}
				}
			}
		case Rejected:
			return fmt.Errorf("handshake frame rejected: %s (%s)", r.Fault, r.Detail)
		case Ignored:
			// Unknown type before WELCOME: ignore (§3) and keep waiting.
		}
	}
}

func (c *Client) readLoop(ctx context.Context, lc *liveConn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.heartbeat(ctx, lc, cancel)

	for {
		_, data, err := lc.ws.Read(ctx)
		if err != nil {
			return err
		}
		lc.touch()
		switch r := Decode(data).(type) {
		case Decoded:
			c.dispatch(lc, r.Message)
		case Ignored:
			c.logf("ignoring unknown message type %q", r.Type)
		case Rejected:
			c.logf("rejected frame: %s (%s)", r.Fault, r.Detail)
		}
	}
}

// heartbeat sends PING and declares the link dead when no inbound frame arrives. The cadence is
// adaptive (D-131): 5 s while a dictation session is in flight, relaxed to IdleHeartbeatInterval when
// idle, so an open but unused link does not wake the phone's radio every 5 s. cancel tears down the
// read loop so Run returns.
func (c *Client) heartbeat(ctx context.Context, lc *liveConn, cancel context.CancelFunc) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	lastPing := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			interval, timeout := heartbeatTiming(c.currentSession() != "")
			if time.Since(lc.lastRxTime()) >= timeout {
				c.logf("heartbeat: no frame in %s, declaring link dead", timeout)
				lc.ws.Close(websocket.StatusPolicyViolation, "heartbeat timeout")
				cancel()
				return
			}
			if time.Since(lastPing) < interval {
				continue
			}
			lastPing = time.Now()
			ts := lastPing.UnixMilli()
			lc.rtt.sent(ts)
			if err := c.write(ctx, lc, mustMessageType("PING"), &PingPayload{ClientTs: ts}); err != nil {
				c.logf("ping failed: %v", err)
			}
		}
	}
}

// heartbeatTiming returns the PING cadence and dead-link timeout for the current activity: the
// protocol's 5 s / 15 s while a dictation is in flight, and the relaxed 30 s / 60 s otherwise
// (D-131).
func heartbeatTiming(active bool) (interval, timeout time.Duration) {
	if active {
		return HeartbeatInterval, HeartbeatTimeout
	}
	return IdleHeartbeatInterval, IdleHeartbeatTimeout
}

// dispatch routes one decoded frame to Handlers and maintains the session/latency bookkeeping.
func (c *Client) dispatch(lc *liveConn, m Message) {
	switch p := m.Payload.(type) {
	case *PongPayload:
		if d, ok := lc.rtt.received(p.ClientTs); ok {
			if h := c.cfg.Handlers.OnLatency; h != nil {
				h(fmt.Sprintf("rtt=%dms", d.Milliseconds()))
			}
		}
	case *StartAckPayload:
		c.mu.Lock()
		c.sessionID = p.SessionID
		c.mu.Unlock()
		lc.utt = newUtterance(p.SessionID)
		if h := c.cfg.Handlers.OnStarted; h != nil {
			h(p.SessionID)
		}
	case *SessionBusyPayload:
		if h := c.cfg.Handlers.OnBusy; h != nil {
			h(p.HolderName, p.CanTakeover)
		}
	case *ServerStatePayload:
		// Unsolicited availability (SERVER_STATE, v1.1). Advisory: the tray records it so a later
		// window can show "phone taken"; it never gates a session on its own.
		if h := c.cfg.Handlers.OnServerState; h != nil {
			h(*p)
		}
	case *StateChangedPayload:
		if h := c.cfg.Handlers.OnState; h != nil {
			h(p.SessionID, p.State)
		}
	case *ProvisionalTextPayload:
		if lc.utt != nil {
			lc.utt.provisional()
		}
		if h := c.cfg.Handlers.OnProvisional; h != nil {
			h(p.SessionID, p.Text) // non-cumulative live tail: replace, never append
		}
	case *CommitTextPayload:
		if lc.utt != nil {
			lc.utt.commit(int(p.Seq), p.Perf, len(p.Text))
		}
		if h := c.cfg.Handlers.OnCommitted; h != nil {
			h(p.SessionID, p.Text, p.Perf) // full snapshot: replace, never append
		}
	case *UtteranceDonePayload:
		c.finishUtterance(lc, p.Reason)
		c.clearSession(p.SessionID)
		if h := c.cfg.Handlers.OnUtterance; h != nil {
			h(p.SessionID, p.DeliveryText, p.RawText, p.Reason)
		}
	case *SessionReleasedPayload:
		c.finishUtterance(lc, p.Reason)
		c.clearSession(p.SessionID)
		if h := c.cfg.Handlers.OnReleased; h != nil {
			h(p.SessionID, p.Reason)
		}
	case *ErrorPayload:
		if h := c.cfg.Handlers.OnError; h != nil {
			h(p.Code, p.Message, p.Fatal)
		}
	case *WelcomePayload:
		// A stray/duplicate WELCOME after auth: ignore.
	}
}

func (c *Client) finishUtterance(lc *liveConn, reason string) {
	if lc.utt == nil {
		return
	}
	if h := c.cfg.Handlers.OnLatency; h != nil {
		h(lc.utt.line(reason))
	}
	lc.utt = nil
}

func (c *Client) writeTo(ctx context.Context, wire string, p Payload) error {
	lc := c.liveConn()
	if lc == nil {
		return ErrNotConnected
	}
	return c.write(ctx, lc, mustMessageType(wire), p)
}

func (c *Client) write(ctx context.Context, lc *liveConn, mt MessageType, p Payload) error {
	b, err := Encode(Message{Type: mt, ID: newUUID(), Ts: time.Now().UnixMilli(), Payload: p})
	if err != nil {
		return err
	}
	lc.writeMu.Lock()
	defer lc.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return lc.ws.Write(wctx, websocket.MessageText, b)
}

func (c *Client) setLive(lc *liveConn) {
	c.mu.Lock()
	c.live = lc
	c.mu.Unlock()
}

func (c *Client) liveConn() *liveConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live
}

func (c *Client) currentSession() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *Client) requireSession() (string, error) {
	if id := c.currentSession(); id != "" {
		return id, nil
	}
	return "", errors.New("no active session")
}

func (c *Client) clearSession(id string) {
	c.mu.Lock()
	if c.sessionID == id {
		c.sessionID = ""
	}
	c.mu.Unlock()
}

func (c *Client) setState(s ConnState, detail string) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
	if h := c.cfg.Handlers.OnConn; h != nil {
		h(s, detail)
	}
}

func (c *Client) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
	}
}

// clientCapabilities are the HELLO capabilities this v1 client advertises. Remote profile/model
// selection is post-v1 (PLAN §6), so the list is empty for now.
var clientCapabilities = []string{}

func mustMessageType(wire string) MessageType {
	mt, ok := Lookup(wire)
	if !ok {
		panic("sovereign: unknown catalogue message type " + wire)
	}
	return mt
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
