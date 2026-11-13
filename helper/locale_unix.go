//go:build unix && !darwin

package main

import "os"

// systemLanguages reads the locale the session was started with.
func systemLanguages() []string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if tag := os.Getenv(name); tag != "" {
			return []string{tag}
		}
	}
	return nil
}
