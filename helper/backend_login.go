package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// The project's server shares Blizzard's data (auction snapshots and the world dataset) only with the app's players,
// as Blizzard's API terms ask. A player shows they are one by signing in with Battle.net once: the server sends the
// browser to Blizzard's sign-in page without asking for any scope, learns only that the sign-in worked and hands the
// companion a random token that says nothing about the account. The token lives in the credential store beside the
// player's own API client (lfg-backend-token, account battlenet) and goes with every request to the server.

const (
	backendTokenService = "lfg-backend-token"
	backendTokenPrefix  = "lfg1."
	backendLoginWait    = 10 * time.Minute // as long as the server's sign-in link lasts

	// backendSignIn is what the tools tell the adviser to pass on when data from the server needs the sign-in.
	backendSignIn = "In the Looking for Guidance menu (its icon in the menu bar on macOS, in the notification area on Windows), choose Sign in with Battle.net. Blizzard's sign-in page opens in the browser. Signing in only shows that the player has a Battle.net account, and the project keeps nothing about the account. Blizzard's page says the app may see the Account ID and BattleTag, as it does for every app that uses its sign-in, but the project never reads them. It is needed because Blizzard's API terms let the project share Blizzard's data only with its own app's players."
)

var (
	backendURL      = defaultAuctionMirror // where the sign-in starts; a var so tests can point it at a fake
	errSignInNeeded = errors.New("the project's server asks for the Battle.net sign-in")
	errLoginEnded   = errors.New("the Battle.net sign-in was cancelled or not finished in time")

	// worldWake asks the tray's world watcher to look now, after a sign-in.
	worldWake = make(chan struct{}, 1)

	backendLogin struct {
		sync.Mutex
		busy bool   // a dialog or a sign-in is under way
		page string // the sign-in page while the browser has it
	}
)

func backendToken() string {
	token, _ := keyring.Get(backendTokenService, battleNetAccount)
	return token
}

func saveBackendToken(token string) error {
	return keyring.Set(backendTokenService, battleNetAccount, token)
}

// forgetBackendToken removes the stored token. Given a token, it removes it only while it is still the stored one, so
// that a late refusal of an old token does not undo a sign-in that has just finished.
func forgetBackendToken(only string) error {
	if only != "" && backendToken() != only {
		return nil
	}
	if err := keyring.Delete(backendTokenService, battleNetAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// dropBackendToken forgets a token the server turned down and then tells the tray.
func dropBackendToken(token string, then func()) {
	if err := forgetBackendToken(token); err != nil {
		logf("Couldn't remove the refused Battle.net sign-in: %v", err)
	}
	if then != nil {
		then()
	}
}

// noteBackendToken shows in the menu whether the player has signed in. Reading the store can wait on a keychain
// prompt, so it never runs on the tray's own goroutine.
func (a *app) noteBackendToken() {
	signed := backendToken() != ""
	a.mu.Lock()
	a.battleNet = signed
	a.mu.Unlock()
	a.publish()
}

// toggleBattleNetSignIn signs in or out from the menu. A second click while the browser has the sign-in page opens
// that page again.
func (a *app) toggleBattleNetSignIn() {
	backendLogin.Lock()
	busy, page := backendLogin.busy, backendLogin.page
	backendLogin.busy = true
	backendLogin.Unlock()
	if busy {
		if page != "" {
			openURL(page)
		}
		return
	}
	defer func() {
		backendLogin.Lock()
		backendLogin.busy, backendLogin.page = false, ""
		backendLogin.Unlock()
		a.noteBackendToken()
	}()
	if a.view().battleNet {
		if !confirm(tr("Sign out of Battle.net? The companion forgets its key, and auction prices and world data stop coming from the project's server until you sign in again.")) {
			return
		}
		if err := forgetBackendToken(""); err != nil {
			logf("Battle.net sign-in: couldn't remove the key: %v", err)
			tell(tr("Couldn't remove the Battle.net sign-in: %s", err.Error()))
			return
		}
		logf("Battle.net sign-in: key removed")
		return
	}
	if !confirm(tr("Auction prices and the world database come from the project's server. Blizzard's terms let the project share this data only with its own players, so the server asks for a one-time sign-in with Battle.net.\n\nBlizzard's sign-in page opens in your browser. It will say the app may see your Account ID and BattleTag: Blizzard shows that for every app that uses its sign-in. The server never reads them, does not ask for your e-mail or characters, and keeps nothing about your account. It only learns that the sign-in worked. Continue?")) {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, backendLoginWait)
	defer cancel()
	token, err := signInWithBattleNet(ctx, backendURL, func(page string) {
		backendLogin.Lock()
		backendLogin.page = page
		backendLogin.Unlock()
		openURL(page)
	})
	if err == nil {
		err = saveBackendToken(token)
	}
	switch {
	case errors.Is(err, errLoginEnded):
		logf("Battle.net sign-in: %v", err)
	case err != nil:
		logf("Battle.net sign-in: %v", err)
		tell(tr("Couldn't finish the Battle.net sign-in: %s", err.Error()))
	default:
		logf("Battle.net sign-in: done")
		for _, wake := range []chan struct{}{auctionWake, worldWake} {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
}

const loginPage = `<!doctype html><meta charset="utf-8"><meta name="color-scheme" content="light dark"><title>Looking for Guidance</title><body style="font:17px/1.5 -apple-system,system-ui,sans-serif;margin:3em auto;max-width:34em;padding:0 1em"><p>%s</p></body>`

type loginResult struct {
	token string
	err   error
}

// signInWithBattleNet opens the server's sign-in page with the port of a listener on this computer. The server sends
// the browser back to that listener with the token, or with why there is none; the nonce keeps other pages out.
func signInWithBattleNet(ctx context.Context, base string, open func(string)) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		ln.Close()
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	result := make(chan loginResult, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/signed-in" || q.Get("nonce") != nonce {
			http.NotFound(w, r)
			return
		}
		var res loginResult
		var text string
		switch token := q.Get("token"); {
		case q.Get("error") == "cancelled":
			res.err, text = errLoginEnded, tr("The Battle.net sign-in was cancelled. You can close this tab.")
		case q.Get("error") != "" || !strings.HasPrefix(token, backendTokenPrefix) || len(token) > 200:
			res.err = errors.New("the project's server could not finish it")
			text = tr("The Battle.net sign-in did not finish. Close this tab and try again from the Looking for Guidance menu.")
		default:
			res.token, text = token, tr("Signed in with Battle.net. You can close this tab and go back to the game.")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, loginPage, html.EscapeString(text))
		select {
		case result <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer func() {
		done, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(done) // lets the page reach the browser first
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	open(strings.TrimSuffix(base, "/") + "/v1/login?" + url.Values{"port": {strconv.Itoa(port)}, "nonce": {nonce}}.Encode())
	select {
	case res := <-result:
		return res.token, res.err
	case <-ctx.Done():
		return "", errLoginEnded
	}
}
