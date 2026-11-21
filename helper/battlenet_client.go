package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/zalando/go-keyring"
)

// The player's own Battle.net API client lives in the system's credential store: the login keychain on macOS
// (items lfg-battlenet-id and lfg-battlenet-secret, account battlenet, the ones earlier versions read) and
// Credential Manager on Windows (lfg-battlenet-id:battlenet and lfg-battlenet-secret:battlenet). The tray's
// Auction prices item puts it there; without both items the auction features stay off.

var battleNetClient = func() (id, secret string, ok bool) {
	id, _ = keyring.Get(battleNetIDService, battleNetAccount)
	secret, _ = keyring.Get(battleNetSecretService, battleNetAccount)
	return id, secret, id != "" && secret != ""
}

func saveBattleNetClient(id, secret string) error {
	if err := keyring.Set(battleNetIDService, battleNetAccount, id); err != nil {
		return err
	}
	return keyring.Set(battleNetSecretService, battleNetAccount, secret)
}

func forgetBattleNetClient() error {
	for _, service := range []string{battleNetIDService, battleNetSecretService} {
		if err := keyring.Delete(service, battleNetAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
	}
	return nil
}

// checkBattleNetClient asks Blizzard for a token with the given client, before the tray stores it.
func checkBattleNetClient(ctx context.Context, id, secret string) error {
	bn := &battleNet{client: func() (string, string, bool) { return id, secret, true },
		http: &http.Client{Timeout: time.Minute}, now: time.Now}
	_, err := bn.bearer(ctx)
	return err
}
