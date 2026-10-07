package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func selfSignedCert(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sovereign-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert, der
}

func TestFingerprintIsURLSafeSHA256OfSPKI(t *testing.T) {
	cert, der := selfSignedCert(t)
	fp := Fingerprint(cert)
	if got, err := FingerprintDER(der); err != nil || got != fp {
		t.Fatalf("FingerprintDER = %q, %v; want %q", got, err, fp)
	}
	if len(fp) != 43 { // base64url of 32 bytes, unpadded
		t.Errorf("fingerprint length %d, want 43", len(fp))
	}
	if strings.ContainsAny(fp, "+/=") {
		t.Errorf("fingerprint %q is not base64url-unpadded", fp)
	}
}

func TestTLSConfigEnforcesPin(t *testing.T) {
	cert, der := selfSignedCert(t)
	pin := Fingerprint(cert)

	if cfg := TLSConfig(pin, nil); cfg.VerifyPeerCertificate([][]byte{der}, nil) != nil {
		t.Fatalf("matching pin rejected")
	}

	cfg := TLSConfig("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", nil)
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("mismatched pin: want ErrPinMismatch, got %v", err)
	}

	var observed string
	cfg = TLSConfig("", func(fp string) { observed = fp })
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Fatalf("learn mode rejected: %v", err)
	}
	if observed != pin {
		t.Fatalf("learn mode observed %q, want %q", observed, pin)
	}

	if cfg := TLSConfig(pin, nil); cfg.VerifyPeerCertificate(nil, nil) == nil {
		t.Fatalf("no certificate must be refused")
	}
}
