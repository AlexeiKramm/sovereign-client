package core

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrPinMismatch is returned when a server presents a certificate whose SPKI fingerprint
// differs from the pinned one (PROTOCOL.md §1/§10). This is a hard failure — a possible MITM —
// and MUST be surfaced, never retried silently.
var ErrPinMismatch = errors.New("server SPKI fingerprint mismatch")

// Fingerprint returns the base64url-encoded SHA-256 of a certificate's SubjectPublicKeyInfo
// (SPKI). This is exactly the value the phone advertises in the mDNS `spki` TXT key and
// exchanges during pairing (PROTOCOL.md §2/§4).
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// FingerprintDER parses a DER certificate and returns its SPKI fingerprint.
func FingerprintDER(der []byte) (string, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "", fmt.Errorf("parse server certificate: %w", err)
	}
	return Fingerprint(cert), nil
}

// TLSConfig builds a *tls.Config that replaces CA verification with SPKI pinning, because the
// phone presents a self-signed certificate (PROTOCOL.md §1) that no public root can vouch for.
//
//   - pin == "" is "learn" mode, used only for the first, not-yet-pinned pairing exchange: any
//     certificate is accepted and its fingerprint is reported to observe.
//   - pin != "" enforces the pin; a mismatch fails the handshake with ErrPinMismatch.
//
// observe, if non-nil, is always called with the fingerprint actually observed, so the caller
// can log it and compare it against the mDNS/pairing value.
func TLSConfig(pin string, observe func(fingerprint string)) *tls.Config {
	return &tls.Config{
		// Pinning below replaces the PKI; the phone's certificate is self-signed by design.
		InsecureSkipVerify: true, //nolint:gosec // deliberate: self-signed + SPKI pinning
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("pinned TLS: server presented no certificate")
			}
			got, err := FingerprintDER(rawCerts[0])
			if err != nil {
				return err
			}
			if observe != nil {
				observe(got)
			}
			if pin == "" {
				return nil
			}
			if got != pin {
				return fmt.Errorf("%w: got %s, want %s", ErrPinMismatch, got, pin)
			}
			return nil
		},
	}
}
