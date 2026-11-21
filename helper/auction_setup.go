package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// The tray's Auction prices item stores the player's own Battle.net API client after Blizzard accepts it, or removes
// it. Either way the watcher looks at once.

const battleNetClientsURL = "https://develop.battle.net/access/clients"

var auctionSetup sync.Mutex // one set of dialogs at a time

// noteAuctionClient shows in the menu whether a client is stored. Reading the store can wait on a keychain prompt, so
// it never runs on the tray's own goroutine.
func (a *app) noteAuctionClient() {
	_, _, ok := battleNetClient()
	a.mu.Lock()
	a.auctions = ok
	a.mu.Unlock()
	a.publish()
}

func (a *app) toggleAuctions() {
	if !auctionSetup.TryLock() {
		return
	}
	defer auctionSetup.Unlock()
	if a.view().auctions {
		a.turnOffAuctions()
	} else {
		a.setUpAuctions()
	}
	a.noteAuctionClient()
	select {
	case auctionWake <- struct{}{}:
	default:
	}
}

func (a *app) setUpAuctions() {
	openURL(battleNetClientsURL)
	id, ok := askText(tr("Auction prices need your own free Battle.net API client. Create one on the page that has just opened, then paste its Client ID here."), false)
	if !ok || strings.TrimSpace(id) == "" {
		return
	}
	secret, ok := askText(tr("Now paste the client secret from the same page."), true)
	if !ok || strings.TrimSpace(secret) == "" {
		return
	}
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	if err := checkBattleNetClient(ctx, id, secret); errors.Is(err, errBattleNetRefused) {
		logf("Auction prices: the client was not stored: %v", err)
		tell(tr("Battle.net did not accept this client ID and secret. Check both on develop.battle.net and choose Auction prices again."))
		return
	} else if err != nil {
		logf("Auction prices: the client was not stored: %v", err)
		tell(tr("Couldn't reach Battle.net to check the client: %s", err.Error()))
		return
	}
	if err := saveBattleNetClient(id, secret); err != nil {
		logf("Auction prices: couldn't store the client: %v", err)
		tell(tr("Couldn't change the stored client: %s", err.Error()))
		return
	}
	logf("Auction prices: Battle.net API client stored")
	tell(tr("Auction prices are on. Ask anything in the game, and the companion downloads your auction house within a few minutes."))
}

func (a *app) turnOffAuctions() {
	if !confirm(tr("Turn off auction prices? The companion removes your Battle.net API client from this computer; prices already downloaded stay.")) {
		return
	}
	if err := forgetBattleNetClient(); err != nil {
		logf("Auction prices: couldn't remove the client: %v", err)
		tell(tr("Couldn't change the stored client: %s", err.Error()))
		return
	}
	logf("Auction prices: Battle.net API client removed")
}
