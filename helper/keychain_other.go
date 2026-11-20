//go:build !darwin

package main

// The Battle.net client is read from the macOS keychain only; elsewhere the auction features stay off.
func battleNetClient() (id, secret string, ok bool) { return "", "", false }
