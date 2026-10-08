//go:build !windows

package windows

// SingleInstanceMutexName is unused off Windows but kept so callers compile everywhere.
const SingleInstanceMutexName = `SovereignClient.SingleInstance`

// AcquireSingleInstance is a no-op on non-Windows platforms; the tray is shipped Windows-first.
func AcquireSingleInstance(name string) (alreadyRunning bool, release func(), err error) {
	return false, func() {}, nil
}
