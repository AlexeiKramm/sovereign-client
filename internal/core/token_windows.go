//go:build windows

package core

import "github.com/billgraziano/dpapi"

// protect encrypts plaintext with the Windows Data Protection API, current-user scope. The
// ciphertext is bound to this machine and user, so it is useless if the file is copied elsewhere.
func protect(plaintext []byte) ([]byte, error) { return dpapi.EncryptBytes(plaintext) }

// unprotect reverses protect.
func unprotect(ciphertext []byte) ([]byte, error) { return dpapi.DecryptBytes(ciphertext) }
