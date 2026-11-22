//go:build !windows

package main

// The macOS bundle already includes the notices in Contents/Resources.
func refreshInstalledLicenses(string) error { return nil }
