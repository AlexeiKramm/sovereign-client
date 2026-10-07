package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLogAtAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")

	l1, close1, err := openLogAt(path)
	if err != nil {
		t.Fatalf("openLogAt: %v", err)
	}
	l1.Print("first run")
	close1()

	l2, close2, err := openLogAt(path)
	if err != nil {
		t.Fatalf("openLogAt (second): %v", err)
	}
	l2.Print("second run")
	close2()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "first run") || !strings.Contains(string(b), "second run") {
		t.Fatalf("second launch lost the first run's line:\n%s", b)
	}
}

func TestOpenLogAtRotatesOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxLogBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	l, closeFn, err := openLogAt(path)
	if err != nil {
		t.Fatalf("openLogAt: %v", err)
	}
	l.Print("after rotation")
	closeFn()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) > maxLogBytes {
		t.Fatalf("log not rotated: %d bytes", len(b))
	}
	if !strings.Contains(string(b), "after rotation") {
		t.Fatalf("rotated log missing new line:\n%s", b)
	}
}
