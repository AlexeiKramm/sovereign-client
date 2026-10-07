package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	pairToken = "AQIDBAUG"
	pairNonce = "server-nonce-xyz"
	pairProof = "UBIGhzprUqoIpiv8X_vBUYPXBN8V-rIBpSnmtAevV6w"
)

func TestPairingProofIsThePhoneRecipe(t *testing.T) {
	cases := []struct{ token, nonce, want string }{
		{pairToken, pairNonce, pairProof},
		{"t", "n", "vkbacFVLVmAgPt0kL0LxsuFO42PRV7eAfscuajaJ-Pg"},
	}
	for _, c := range cases {
		if got := pairingProof(c.token, c.nonce); got != c.want {
			t.Errorf("pairingProof(%q, %q) = %q, want %q", c.token, c.nonce, got, c.want)
		}
	}
}

// pairingPhone is a fake phone that runs the PROTOCOL.md §4 mutual-accept exchange. challengeSpki
// lets a test lie about the advertised fingerprint to exercise the MITM check.
func pairingPhone(t *testing.T, decision string, challengeSpki func(real string) string, gotProof *string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(MaxFrameBytes)
		ctx := r.Context()

		hello, err := readMsg(ctx, ws)
		if err != nil || hello.Type.Wire != "PAIR_HELLO" {
			t.Errorf("first pairing frame = %v (%v), want PAIR_HELLO", hello.Type, err)
			return
		}
		real := Fingerprint(server.Certificate())
		advertised := real
		if challengeSpki != nil {
			advertised = challengeSpki(real)
		}
		_ = writeMsg(ctx, ws, mustMessageType("PAIR_CHALLENGE"), &PairChallengePayload{
			ServerNonce: pairNonce, ServerSpki: advertised,
		})
		if decision == "" {
			_ = writeMsg(ctx, ws, mustMessageType("PAIR_RESPONSE"), &PairResponsePayload{
				DeviceID: "phone-1", ServerName: "Test Phone", Token: pairToken,
			})
		} else {
			_ = writeMsg(ctx, ws, mustMessageType("PAIR_FAIL"), &PairFailPayload{Reason: decision, AttemptsLeft: 4})
		}

		done, err := readMsg(ctx, ws)
		if err != nil {
			return
		}
		if p, ok := done.Payload.(*PairDonePayload); ok && gotProof != nil {
			*gotProof = p.Proof
		}
	}))
	return server
}

func TestPairCompletesHandshakeAndReturnsCredentials(t *testing.T) {
	var gotProof string
	server := pairingPhone(t, "", nil, &gotProof)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var shown string
	creds, err := Pair(ctx, PairOptions{
		Addr:          hostOf(t, server.URL),
		Ident:         Identity{DeviceID: "desk-1", Name: "Desk"},
		OnFingerprint: func(fp string) { shown = fp },
		Logf:          t.Logf,
	})
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if creds.Token != pairToken || creds.ServerID != "phone-1" || creds.ServerName != "Test Phone" {
		t.Fatalf("credentials = %+v", creds)
	}
	if creds.ServerSpki != Fingerprint(server.Certificate()) {
		t.Fatalf("pinned spki %q, want the observed fingerprint", creds.ServerSpki)
	}
	if shown != creds.ServerSpki {
		t.Fatalf("OnFingerprint saw %q, want %q", shown, creds.ServerSpki)
	}
	if gotProof != pairProof {
		t.Fatalf("phone received proof %q, want %q", gotProof, pairProof)
	}
}

func TestPairRejectsTamperedFingerprint(t *testing.T) {
	server := pairingPhone(t, "", func(string) string { return "not-the-real-fingerprint" }, nil)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Pair(ctx, PairOptions{Addr: hostOf(t, server.URL), Ident: Identity{DeviceID: "d"}})
	if err == nil || !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("Pair = %v, want ErrPinMismatch", err)
	}
}

func TestPairSurfacesRefusal(t *testing.T) {
	server := pairingPhone(t, PairFailLocked, nil, nil)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Pair(ctx, PairOptions{Addr: hostOf(t, server.URL), Ident: Identity{DeviceID: "d"}})
	var pe *PairError
	if !errors.As(err, &pe) || pe.Reason != PairFailLocked {
		t.Fatalf("Pair = %v, want PairError{LOCKED}", err)
	}
}
