package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakePhone is a minimal but faithful v1 server: it authenticates HELLO, answers PING, and drives
// one dictation session whose COMMIT_TEXT snapshot is rewritten mid-utterance (exactly the case
// PROTOCOL.md §6 warns about) so the client proves replace-not-append.
func fakePhone(t *testing.T, observeHello func(*HelloPayload)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Logf("accept: %v", err)
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(MaxFrameBytes)
		ctx := r.Context()

		hello, err := readMsg(ctx, ws)
		if err != nil {
			t.Logf("read HELLO: %v", err)
			return
		}
		if hello.Type.Wire != "HELLO" {
			t.Errorf("first frame = %s, want HELLO", hello.Type.Wire)
			return
		}
		if observeHello != nil {
			observeHello(hello.Payload.(*HelloPayload))
		}
		if err := writeMsg(ctx, ws, mustMessageType("WELCOME"), &WelcomePayload{
			Protover: Version, DeviceID: "phone-1", ServerName: "Test Phone",
			Capabilities: []string{}, MaxFrameBytes: MaxFrameBytes,
		}); err != nil {
			return
		}
		// An unknown type must be ignored, not fatal (§3).
		_ = ws.Write(ctx, websocket.MessageText,
			[]byte(`{"v":1,"type":"FUTURE_THING","id":"x","ts":1,"payload":{}}`))
		// An unsolicited availability frame (SERVER_STATE, v1.1) must reach OnServerState.
		_ = writeMsg(ctx, ws, mustMessageType("SERVER_STATE"), &ServerStatePayload{
			State: ServerStateReserved, HolderDeviceID: "phone-1", HolderName: "Test Phone",
		})

		for {
			m, err := readMsg(ctx, ws)
			if err != nil {
				return
			}
			switch p := m.Payload.(type) {
			case *PingPayload:
				_ = writeMsg(ctx, ws, mustMessageType("PONG"), &PongPayload{ClientTs: p.ClientTs, ServerTs: time.Now().UnixMilli()})
			case *StartDictationPayload:
				_ = writeMsg(ctx, ws, mustMessageType("START_ACK"), &StartAckPayload{SessionID: "s1"})
				_ = writeMsg(ctx, ws, mustMessageType("STATE_CHANGED"), &StateChangedPayload{SessionID: "s1", State: StateWarming})
				_ = writeMsg(ctx, ws, mustMessageType("STATE_CHANGED"), &StateChangedPayload{SessionID: "s1", State: StateListening})
				_ = writeMsg(ctx, ws, mustMessageType("PROVISIONAL_TEXT"), &ProvisionalTextPayload{SessionID: "s1", Seq: 1, Text: "hello wor"})
				_ = writeMsg(ctx, ws, mustMessageType("COMMIT_TEXT"), &CommitTextPayload{
					SessionID: "s1", Seq: 1, Text: "Hello world", Perf: CommitPerf{AsrMs: 120, CleanupMs: 30},
				})
				_ = writeMsg(ctx, ws, mustMessageType("COMMIT_TEXT"), &CommitTextPayload{
					SessionID: "s1", Seq: 2, Text: "Hello, world!", Perf: CommitPerf{AsrMs: 20, CleanupMs: 5},
				})
			case *StopDictationPayload:
				_ = writeMsg(ctx, ws, mustMessageType("STATE_CHANGED"), &StateChangedPayload{SessionID: "s1", State: StateProcessing})
				_ = writeMsg(ctx, ws, mustMessageType("UTTERANCE_DONE"), &UtteranceDonePayload{
					SessionID: "s1", DeliveryText: "Hello, world!", RawText: "hello world", Reason: DoneFinal,
				})
				_ = writeMsg(ctx, ws, mustMessageType("SESSION_RELEASED"), &SessionReleasedPayload{SessionID: "s1", Reason: ReleaseStopped})
			}
		}
	}))
	return server
}

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return u.Host
}

func TestClientAuthenticatesAndDrivesSession(t *testing.T) {
	var hello *HelloPayload
	server := fakePhone(t, func(h *HelloPayload) { hello = h })
	defer server.Close()
	pin := Fingerprint(server.Certificate())

	conn := make(chan ConnState, 16)
	commits := make(chan string, 8)
	delivered := make(chan string, 4)
	phone := make(chan ServerStatePayload, 4)

	client := NewClient(ClientConfig{
		Addr:  hostOf(t, server.URL),
		Pin:   pin,
		Ident: Identity{DeviceID: "desk-1", Name: "Desk"},
		Creds: Credentials{Token: "tok", ServerSpki: pin},
		Handlers: Handlers{
			OnConn:        func(s ConnState, _ string) { conn <- s },
			OnCommitted:   func(_, text string, _ CommitPerf) { commits <- text },
			OnUtterance:   func(_, delivery, _, _ string) { delivered <- delivery },
			OnServerState: func(s ServerStatePayload) { phone <- s },
		},
		Logf: t.Logf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	waitState(t, conn, StateReady)
	if hello == nil || hello.Token != "tok" || hello.DeviceID != "desk-1" || hello.Protover != Version {
		t.Fatalf("HELLO = %+v", hello)
	}

	// The phone advertised its pipeline availability right after WELCOME; the client must surface it.
	select {
	case s := <-phone:
		if s.State != ServerStateReserved || s.HolderName != "Test Phone" || s.HolderDeviceID != "phone-1" {
			t.Fatalf("SERVER_STATE = %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SERVER_STATE delivered to OnServerState")
	}

	if err := client.StartDictation(ctx, ModeHold); err != nil {
		t.Fatalf("StartDictation: %v", err)
	}

	// COMMIT_TEXT is a full snapshot: the client must see the rewrite, not an append.
	if got := waitString(t, commits); got != "Hello world" {
		t.Fatalf("first COMMIT_TEXT = %q", got)
	}
	if got := waitString(t, commits); got != "Hello, world!" {
		t.Fatalf("rewritten COMMIT_TEXT = %q (append, not replace?)", got)
	}

	if err := client.StopDictation(ctx); err != nil {
		t.Fatalf("StopDictation: %v", err)
	}
	if got := waitString(t, delivered); got != "Hello, world!" {
		t.Fatalf("UTTERANCE_DONE deliveryText = %q", got)
	}
}

func TestClientRejectsPinMismatchAsFatal(t *testing.T) {
	server := fakePhone(t, nil)
	defer server.Close()

	client := NewClient(ClientConfig{
		Addr:  hostOf(t, server.URL),
		Pin:   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Ident: Identity{DeviceID: "desk-1"},
		Creds: Credentials{Token: "tok"},
		Logf:  t.Logf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run must fail fast with the pin error, not retry until the context expires.
	err := client.Run(ctx)
	if !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("Run = %v, want ErrPinMismatch", err)
	}
}

func waitState(t *testing.T, ch <-chan ConnState, want ConnState) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case got := <-ch:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for state %s", want)
		}
	}
}

func waitString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for value")
		return ""
	}
}

func TestWebSocketURL(t *testing.T) {
	cases := []struct {
		addr string
		path string
		want string
	}{
		{"192.168.1.10:8765", "/sovereign", "wss://192.168.1.10:8765/sovereign"},
		{"[::1]:8765", "/sovereign", "wss://[::1]:8765/sovereign"},
		{"[fe80::1%eth0]:8765", "/sovereign", "wss://[fe80::1%25eth0]:8765/sovereign"},
		{"[fe80::8c4d:fdff:fe3f:77c%Ethernet 4]:8765", "/sovereign", "wss://[fe80::8c4d:fdff:fe3f:77c%25Ethernet+4]:8765/sovereign"},
	}
	for _, c := range cases {
		got := WebSocketURL(c.addr, c.path)
		if got != c.want {
			t.Errorf("WebSocketURL(%q, %q) = %q, want %q", c.addr, c.path, got, c.want)
		}
		if _, err := url.Parse(got); err != nil {
			t.Errorf("url.Parse(%q) failed: %v", got, err)
		}
	}
}
