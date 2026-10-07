//go:build !windows

package windows

// SetAutostart is a no-op on non-Windows platforms.
func SetAutostart(exe string, enable bool) error { return nil }

// AutostartEnabled is always false on non-Windows platforms.
func AutostartEnabled(exe string) bool { return false }
