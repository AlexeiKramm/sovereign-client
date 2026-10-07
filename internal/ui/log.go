package ui

import (
	"log"
	"os"
	"path/filepath"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
)

// OpenLog opens the tray app's log file under the Sovereign data directory, truncating the previous
// run. A GUI-subsystem binary launched from the tray has no console, so this file is the only place
// its diagnostics are visible. It returns the logger and a close function.
func OpenLog() (*log.Logger, func(), error) {
	dir, err := core.DataDir()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "sovereign-desktop.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", log.LstdFlags|log.Lmicroseconds), func() { _ = f.Close() }, nil
}
