package ui

import (
	"log"
	"os"
	"path/filepath"

	"github.com/alexeikramm/sovereign-client/internal/core"
)

// maxLogBytes caps the tray log. On startup an over-size log is truncated; otherwise output is
// appended, so a run's evidence survives the next launch.
const maxLogBytes = 1 << 20 // 1 MiB

// OpenLog opens the tray app's log file under the Sovereign data directory. A GUI-subsystem binary
// launched from the tray has no console, so this file is the only place its diagnostics are visible.
// It returns the logger and a close function.
func OpenLog() (*log.Logger, func(), error) {
	dir, err := core.DataDir()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	return openLogAt(filepath.Join(dir, "sovereign-client.log"))
}

func openLogAt(path string) (*log.Logger, func(), error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		_ = os.Truncate(path, 0)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", log.LstdFlags|log.Lmicroseconds), func() { _ = f.Close() }, nil
}
