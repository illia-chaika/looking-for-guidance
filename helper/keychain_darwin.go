package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// battleNetClient reads the player's own Battle.net API client from the login keychain, where they put it with
//
//	security add-generic-password -U -s lfg-battlenet-id -a battlenet -w
//	security add-generic-password -U -s lfg-battlenet-secret -a battlenet -w
//
// Without both items the auction features stay off.
func battleNetClient() (id, secret string, ok bool) {
	read := func(service string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password",
			"-s", service, "-a", battleNetAccount, "-w").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	id, secret = read(battleNetIDService), read(battleNetSecretService)
	return id, secret, id != "" && secret != ""
}
