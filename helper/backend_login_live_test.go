package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveSignIn signs in with Battle.net through the project's server in the default browser, keeps the key in the
// credential store as the menu does, and reads the world dataset's manifest with it:
//
//	FOREVER_SIGNIN=1 go test -run TestLiveSignIn -v ./helper
func TestLiveSignIn(t *testing.T) {
	if os.Getenv("FOREVER_SIGNIN") == "" {
		t.Skip("set FOREVER_SIGNIN=1 to sign in with Battle.net in the browser")
	}
	// The other tests pin the menu to Russian; the browser pages here are in the language the player gets.
	old := menuLanguage
	menuLanguage = chosenMenuLanguage
	t.Cleanup(func() { menuLanguage = old })
	t.Logf("browser pages in %q", menuLanguage())
	ctx, cancel := context.WithTimeout(context.Background(), backendLoginWait)
	defer cancel()
	token, err := signInWithBattleNet(ctx, backendURL, openURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveBackendToken(token); err != nil {
		t.Fatal(err)
	}
	w := &worldWatcher{dir: t.TempDir(), http: &http.Client{Timeout: time.Minute}, now: time.Now}
	body, err := w.get(ctx, defaultWorldMirror+"/world/manifest.json", 1<<20)
	if err != nil {
		t.Fatal("the key should open the world dataset:", err)
	}
	t.Logf("signed in; the key is in the credential store and the world manifest (%d bytes) came with it", len(body))
}
