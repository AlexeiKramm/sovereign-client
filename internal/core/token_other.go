//go:build !windows

package core

import "errors"

// errNoSecureStore is returned on platforms without an implemented OS key store. Writing the
// token in the clear is refused rather than accepted silently; a future port needs libsecret /
// Keychain here. The rest of the client (discovery, codec, manual connect) works unpaired.
var errNoSecureStore = errors.New("secure credential storage is not implemented on this platform (Windows DPAPI only); refusing to store the token in the clear")

func protect([]byte) ([]byte, error)   { return nil, errNoSecureStore }
func unprotect([]byte) ([]byte, error) { return nil, errNoSecureStore }
