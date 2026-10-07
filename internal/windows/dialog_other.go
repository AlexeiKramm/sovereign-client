//go:build !windows

package windows

// InfoBox is a no-op on non-Windows platforms.
func InfoBox(title, text string) {}

// ConfirmBox proceeds as if the user said yes on non-Windows platforms.
func ConfirmBox(title, text string) bool { return true }
