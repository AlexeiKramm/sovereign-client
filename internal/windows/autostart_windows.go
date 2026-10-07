//go:build windows

package windows

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Sovereign Desktop"
)

// SetAutostart adds or removes the per-user Run entry that launches the tray at sign-in. The value
// is the quoted executable path: HKCU\...\Run, not a scheduled task or a service, so it needs no
// elevation and is visible/removable by the user in Task Manager's Startup tab.
func SetAutostart(exe string, enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	if enable {
		return k.SetStringValue(runValueName, `"`+exe+`"`)
	}
	if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// AutostartEnabled reports whether the Run entry exists and points at exe. An entry pointing at a
// stale path (e.g. after the binary moved) reads as disabled, which is what the tray checkbox
// should then show.
func AutostartEnabled(exe string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.Trim(v, `"`), exe)
}
