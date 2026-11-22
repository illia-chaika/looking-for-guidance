package main

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBackendToken = "lfg1.test.signature"

// useBackendToken stores a sign-in token in the in-memory credential store for one test.
func useBackendToken(t *testing.T) {
	t.Helper()
	if err := saveBackendToken(testBackendToken); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { forgetBackendToken("") })
}

// comeBack plays the server's last redirect: the browser opens the companion's listener with the given query.
func comeBack(t *testing.T, page string, query url.Values) (int, string) {
	t.Helper()
	u, err := url.Parse(page)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://127.0.0.1:" + u.Query().Get("port") + "/signed-in?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestBattleNetSignInTakesTheTokenFromTheBrowser(t *testing.T) {
	var opened string
	token, err := signInWithBattleNet(context.Background(), "https://server.test/", func(page string) {
		opened = page
		u, _ := url.Parse(page)
		if u.Host != "server.test" || u.Path != "/v1/login" || len(u.Query().Get("nonce")) < 16 {
			t.Errorf("the sign-in starts at the server with a port and a nonce: %s", page)
		}
		if code, _ := comeBack(t, page, url.Values{"nonce": {"wrong"}, "token": {"lfg1.planted.x"}}); code != http.StatusNotFound {
			t.Errorf("another page cannot hand over a token: %d", code)
		}
		code, body := comeBack(t, page, url.Values{"nonce": {u.Query().Get("nonce")}, "token": {testBackendToken}})
		if code != http.StatusOK || !strings.Contains(body, html.EscapeString(tr("Signed in with Battle.net. You can close this tab and go back to the game."))) {
			t.Errorf("the browser is told it is done: %d %s", code, body)
		}
	})
	if err != nil || token != testBackendToken || opened == "" {
		t.Fatalf("the token comes from the listener: %q %v", token, err)
	}
}

func TestBattleNetSignInCancelledFailedOrAbandoned(t *testing.T) {
	answer := func(query url.Values) func(string) {
		return func(page string) {
			u, _ := url.Parse(page)
			query.Set("nonce", u.Query().Get("nonce"))
			comeBack(t, page, query)
		}
	}
	if _, err := signInWithBattleNet(context.Background(), "https://server.test", answer(url.Values{"error": {"cancelled"}})); !errors.Is(err, errLoginEnded) {
		t.Fatal("a cancelled sign-in ends quietly:", err)
	}
	_, err := signInWithBattleNet(context.Background(), "https://server.test", answer(url.Values{"error": {"failed"}}))
	if err == nil || errors.Is(err, errLoginEnded) {
		t.Fatal("a failed exchange is an error to show:", err)
	}
	if _, err := signInWithBattleNet(context.Background(), "https://server.test", answer(url.Values{"token": {"not-ours"}})); err == nil {
		t.Fatal("a token of another shape is refused")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := signInWithBattleNet(ctx, "https://server.test", func(string) {}); !errors.Is(err, errLoginEnded) {
		t.Fatal("a browser that never comes back ends the wait:", err)
	}
}

func TestRefusedTokenIsForgottenOnlyWhileStored(t *testing.T) {
	useBackendToken(t)
	if forgetBackendToken("lfg1.older.one"); backendToken() != testBackendToken {
		t.Fatal("refusing an older token keeps a newer sign-in")
	}
	called := false
	if dropBackendToken(testBackendToken, func() { called = true }); backendToken() != "" || !called {
		t.Fatal("the refused token is forgotten and the tray told")
	}
}

func TestAuctionPricesWaitForTheSignIn(t *testing.T) {
	requests := fakeMirror(t)
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	dir := auctionDir(snapshots)
	now := time.Now()
	w := testWatcher(dir, now)
	w.bn.client = func() (string, string, bool) { return "", "", false }
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if err := recordMarket(dir, m, now); err != nil {
		t.Fatal(err)
	}
	unavailable(t, auctionProblem(snapshots, m, nil), "sign-in")
	w.refresh(context.Background())
	if len(*requests) != 0 {
		t.Fatal("without the sign-in the mirror is not asked:", *requests)
	}
	useBattleNetClient(t)
	if p := auctionProblem(snapshots, m, nil); p != nil && p.Code == "sign-in" {
		t.Fatal("an own API client needs no sign-in")
	}
	forgetBattleNetClient()

	// A token the server turns down is forgotten, and the tools ask for the sign-in again.
	if err := saveBackendToken("lfg1.revoked.x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { forgetBackendToken("") })
	lost := false
	w.lost = func() { lost = true }
	w.refresh(context.Background())
	if backendToken() != "" || !lost || len(*requests) == 0 {
		t.Fatal("a refused token is forgotten after one request:", backendToken(), lost, *requests)
	}
	unavailable(t, auctionProblem(snapshots, m, nil), "sign-in")
}

func TestWorldDatasetWaitsForTheSignIn(t *testing.T) {
	f := newFakeWorld(t, testWorldRecords, "")
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	w := &worldWatcher{dir: worldDir(snapshots), http: http.DefaultClient, base: func() string { return f.srv.URL }, now: time.Now}
	if err := w.refresh(context.Background()); err != nil || len(f.asked) != 0 {
		t.Fatal("without the sign-in nothing is asked:", err, f.asked)
	}
	if err := w.refreshChanges(context.Background()); err != nil || len(f.asked) != 0 {
		t.Fatal("nor the changes feed:", err, f.asked)
	}
	if _, err := queryWorldSearch(snapshots, []byte(`{"snapshot_id":"x","query":"potion"}`)); !errors.Is(err, errWorldSignIn) {
		t.Fatal("the tools say how to get the dataset:", err)
	}
	if answer, _ := answerChanges(insightRequest{Facts: []byte(`{"class":"MAGE"}`)}, insightEnv{}); !answer.(changesAnswer).SignIn {
		t.Fatal("the journal's card is told the dataset waits for the sign-in")
	}

	if err := saveBackendToken("lfg1.revoked.x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { forgetBackendToken("") })
	lost := false
	w.lost = func() { lost = true }
	if err := w.refresh(context.Background()); !errors.Is(err, errSignInNeeded) || backendToken() != "" || !lost {
		t.Fatal("a refused token is forgotten:", err, backendToken(), lost)
	}

	useBackendToken(t)
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := queryWorldSearch(snapshots, []byte(`{"snapshot_id":"x","query":"potion"}`)); err != nil {
		t.Fatal("after the sign-in the dataset is here:", err)
	}
}
