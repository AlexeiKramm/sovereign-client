package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// PairOptions configures a first-time mutual-accept pairing (PROTOCOL.md §4).
type PairOptions struct {
	Addr          string
	Ident         Identity
	OnFingerprint func(fingerprint string) // called with the observed SPKI, to show the user
	Timeout       time.Duration            // defaults to 2 minutes: the user must tap Approve
	Logf          func(format string, args ...any)
}

// PairError is a refusal from the phone.
type PairError struct {
	Reason       string // DENIED | LOCKED | VERSION
	AttemptsLeft int64
}

func (e *PairError) Error() string {
	return fmt.Sprintf("pairing refused: %s (%d attempts left)", e.Reason, e.AttemptsLeft)
}

// Pair runs the mutual-accept handshake against a not-yet-pinned phone and returns the credentials
// to persist. The phone shows an approval dialog naming this device; the desktop's only job is to
// present its own identity and, before pinning, report the server fingerprint it observed so the
// user can compare it with the phone's screen — the man-in-the-middle defence for the first,
// un-authenticated exchange (PROTOCOL.md §4/§10).
func Pair(ctx context.Context, opts PairOptions) (Credentials, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}
	pctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	logf := func(format string, args ...any) {
		if opts.Logf != nil {
			opts.Logf(format, args...)
		}
	}

	// Learn mode: no pin exists yet, so accept the self-signed certificate but capture its SPKI.
	var observed string
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: TLSConfig("", func(fp string) {
		observed = fp
	})}}
	url := WebSocketURL(opts.Addr, Path)
	ws, _, err := websocket.Dial(pctx, url, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return Credentials{}, fmt.Errorf("dial %s: %w", url, err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(MaxFrameBytes)

	if err := writeMsg(pctx, ws, mustMessageType("PAIR_HELLO"), &PairHelloPayload{
		DeviceID: opts.Ident.DeviceID,
		Name:     opts.Ident.Name,
		Protover: Version,
	}); err != nil {
		return Credentials{}, fmt.Errorf("send PAIR_HELLO: %w", err)
	}

	challenge, err := readMsg(pctx, ws)
	if err != nil {
		return Credentials{}, fmt.Errorf("read PAIR_CHALLENGE: %w", err)
	}
	pc, ok := challenge.Payload.(*PairChallengePayload)
	if !ok {
		if pf, ok := challenge.Payload.(*PairFailPayload); ok {
			return Credentials{}, &PairError{Reason: pf.Reason, AttemptsLeft: pf.AttemptsLeft}
		}
		return Credentials{}, fmt.Errorf("expected PAIR_CHALLENGE, got %s", challenge.Type.Wire)
	}

	// The fingerprint the phone advertises must equal the one its certificate actually presented,
	// or the discovery/challenge data was tampered with.
	if pc.ServerSpki != observed {
		return Credentials{}, fmt.Errorf("%w: phone advertises %s but presented %s", ErrPinMismatch, pc.ServerSpki, observed)
	}
	if opts.OnFingerprint != nil {
		opts.OnFingerprint(observed)
	}
	logf("pairing: server fingerprint %s — confirm it matches the phone's screen", observed)

	resp, err := readMsg(pctx, ws)
	if err != nil {
		return Credentials{}, fmt.Errorf("read PAIR_RESPONSE: %w", err)
	}
	pr, ok := resp.Payload.(*PairResponsePayload)
	if !ok {
		if pf, ok := resp.Payload.(*PairFailPayload); ok {
			return Credentials{}, &PairError{Reason: pf.Reason, AttemptsLeft: pf.AttemptsLeft}
		}
		return Credentials{}, fmt.Errorf("expected PAIR_RESPONSE, got %s", resp.Type.Wire)
	}

	// Prove possession of the token without putting it on the wire.
	proof := pairingProof(pr.Token, pc.ServerNonce)
	if err := writeMsg(pctx, ws, mustMessageType("PAIR_DONE"), &PairDonePayload{Proof: proof}); err != nil {
		return Credentials{}, fmt.Errorf("send PAIR_DONE: %w", err)
	}

	// On success the phone answers nothing (it moves to AWAITING_AUTH); on a bad proof it sends
	// PAIR_FAIL. Read briefly: a refusal is an error, a silence/close is success.
	dctx, dcancel := context.WithTimeout(pctx, 2*time.Second)
	defer dcancel()
	if m, err := readMsg(dctx, ws); err == nil {
		if pf, ok := m.Payload.(*PairFailPayload); ok {
			return Credentials{}, &PairError{Reason: pf.Reason, AttemptsLeft: pf.AttemptsLeft}
		}
	}

	logf("paired with %q; token stored", pr.ServerName)
	return Credentials{
		ServerID:   pr.DeviceID,
		ServerName: pr.ServerName,
		ServerSpki: observed,
		Token:      pr.Token,
	}, nil
}

// pairingProof computes base64url-nopad(HMAC-SHA256(key, msg)) exactly as the phone's
// PairingAuthority.PairingProof.compute does: the key is the token **string as transmitted**
// (not its decoded bytes), and the message is the serverNonce string.
func pairingProof(token, serverNonce string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(serverNonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func writeMsg(ctx context.Context, ws *websocket.Conn, mt MessageType, p Payload) error {
	b, err := Encode(Message{Type: mt, ID: newUUID(), Ts: time.Now().UnixMilli(), Payload: p})
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, b)
}

// readMsg reads the next catalogued message, skipping unknown types (§3).
func readMsg(ctx context.Context, ws *websocket.Conn) (Message, error) {
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return Message{}, err
		}
		switch r := Decode(data).(type) {
		case Decoded:
			return r.Message, nil
		case Ignored:
			continue
		case Rejected:
			return Message{}, fmt.Errorf("rejected frame: %s (%s)", r.Fault, r.Detail)
		}
	}
}
