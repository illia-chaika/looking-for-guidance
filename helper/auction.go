package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The auction house of the player's market, from Blizzard's Game Data API. The tray downloads the houses the game
// reported recently and keeps a short summary per item; the game data server reads it for get_auction_prices.
// Snapshots come through the player's own Battle.net API client when one is in the credential store
// (battleNetClient), otherwise from the project's mirror, which serves Blizzard's snapshots unchanged to players who
// signed in with Battle.net (backend_login.go).

const (
	battleNetAccount       = "battlenet"
	battleNetIDService     = "lfg-battlenet-id"
	battleNetSecretService = "lfg-battlenet-secret"

	auctionEvery       = 20 * time.Minute
	auctionMarketAge   = 7 * 24 * time.Hour // markets the game reported this recently are kept fresh
	auctionHistoryDays = 30
	auctionTiers       = 10       // cheapest price levels kept per variant
	auctionContentTune = 28       // the API's modifier type for ContentTuningID: where the item dropped decides its level
	auctionMaxBody     = 64 << 20 // one house was 27 MB of JSON in November 2026

	defaultAuctionMirror = "https://api.lfguidance.com"
)

var (
	battleNetTokenURL = "https://oauth.battle.net/token"
	battleNetAPI      = func(region string) string { return "https://" + region + ".api.blizzard.com" }

	errNoBattleNetClient = errors.New("no Battle.net API client is set up")
	errBattleNetRefused  = errors.New("Battle.net refused the API client")

	// auctionWake asks the tray's watcher to look now, when the game reports a market it has not downloaded yet.
	auctionWake = make(chan struct{}, 1)
	marketsMu   sync.Mutex

	auctionMirrorURL = func() string { return mirrorAddress(loadSettings()) }
	mirrorPath       = regexp.MustCompile(`^/data/wow/ruleset/\d+/auctions/(\d+)(/commodities)?$`)
)

// mirrorAddress is auctionMirror from settings.json when it names an https address, nothing when it says "off" or
// names anything else, and the project's own mirror when it is absent.
func mirrorAddress(s settings) string {
	switch v := strings.TrimSpace(s.AuctionMirror); v {
	case "":
		return defaultAuctionMirror
	case "off":
		return ""
	default:
		return strings.TrimSuffix(safeWebURL(v), "/")
	}
}

func auctionDir(snapshots string) string { return filepath.Join(filepath.Dir(snapshots), "auctions") }

// auctionBook is one house at one moment, reduced to what advice needs.
type auctionBook struct {
	Market   auctionMarket        `json:"market"`
	Fetched  int64                `json:"fetched"`  // Unix ms of the download
	Modified map[string]string    `json:"modified"` // Last-Modified of the auctions and commodities responses
	Items    map[int]*auctionItem `json:"items"`
}

type auctionItem struct {
	Unit     int64             `json:"unit"` // lowest buyout per unit, copper
	Listed   int               `json:"listed"`
	Listings int               `json:"listings"`
	Variants []*auctionVariant `json:"variants"` // cheapest first; commodities have one without bonuses
}

type auctionVariant struct {
	Bonus    []int      `json:"bonus,omitempty"`
	Tuning   int        `json:"tuning,omitempty"`
	Unit     int64      `json:"unit"`
	Listed   int        `json:"listed"`
	Listings int        `json:"listings"`
	Tiers    [][2]int64 `json:"tiers"` // unit price and quantity of the cheapest levels
}

func (b *auctionBook) modifiedAt() time.Time {
	var latest time.Time
	for _, value := range b.Modified {
		if t, err := http.ParseTime(value); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

func loadAuctionBook(path string) (*auctionBook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b auctionBook
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

type rawAuction struct {
	Item struct {
		ID        int   `json:"id"`
		Bonus     []int `json:"bonus_lists"`
		Modifiers []struct {
			Type  int `json:"type"`
			Value int `json:"value"`
		} `json:"modifiers"`
	} `json:"item"`
	Buyout    int64 `json:"buyout"`
	UnitPrice int64 `json:"unit_price"`
	Quantity  int   `json:"quantity"`
}

// decodeAuctions streams the "auctions" array of a house response; an empty house has none.
func decodeAuctions(r io.Reader, add func(rawAuction)) error {
	dec := json.NewDecoder(r)
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return fmt.Errorf("not an auction house response: %v", err)
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if t != "auctions" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
			continue
		}
		if t, err := dec.Token(); err != nil || t != json.Delim('[') {
			return fmt.Errorf("auctions is not a list: %v", err)
		}
		for dec.More() {
			var a rawAuction
			if err := dec.Decode(&a); err != nil {
				return err
			}
			add(a)
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	return nil
}

type variantKey struct {
	id     int
	bonus  string
	tuning int
}

type bookBuilder struct {
	prices map[variantKey]map[int64]int // unit price → quantity
	counts map[variantKey]int
}

func newBookBuilder() *bookBuilder {
	return &bookBuilder{prices: map[variantKey]map[int64]int{}, counts: map[variantKey]int{}}
}

func (b *bookBuilder) add(a rawAuction) {
	if a.Item.ID <= 0 || a.Quantity <= 0 {
		return
	}
	unit := a.UnitPrice
	if unit <= 0 && a.Buyout > 0 {
		unit = (a.Buyout + int64(a.Quantity) - 1) / int64(a.Quantity)
	}
	if unit <= 0 {
		return // bid only: nothing to buy now
	}
	bonus := slices.Clone(a.Item.Bonus)
	slices.Sort(bonus)
	k := variantKey{id: a.Item.ID, bonus: joinInts(bonus)}
	for _, m := range a.Item.Modifiers {
		if m.Type == auctionContentTune {
			k.tuning = m.Value
		}
	}
	if b.prices[k] == nil {
		b.prices[k] = map[int64]int{}
	}
	b.prices[k][unit] += a.Quantity
	b.counts[k]++
}

func (b *bookBuilder) items() map[int]*auctionItem {
	items := map[int]*auctionItem{}
	for k, levels := range b.prices {
		v := &auctionVariant{Bonus: splitInts(k.bonus), Tuning: k.tuning, Listings: b.counts[k]}
		units := make([]int64, 0, len(levels))
		for unit, qty := range levels {
			units = append(units, unit)
			v.Listed += qty
		}
		slices.Sort(units)
		for _, unit := range units[:min(len(units), auctionTiers)] {
			v.Tiers = append(v.Tiers, [2]int64{unit, int64(levels[unit])})
		}
		v.Unit = units[0]
		it := items[k.id]
		if it == nil {
			it = &auctionItem{Unit: v.Unit}
			items[k.id] = it
		}
		it.Unit = min(it.Unit, v.Unit)
		it.Listed += v.Listed
		it.Listings += v.Listings
		it.Variants = append(it.Variants, v)
	}
	for _, it := range items {
		sort.Slice(it.Variants, func(i, j int) bool {
			a, b := it.Variants[i], it.Variants[j]
			if a.Unit != b.Unit {
				return a.Unit < b.Unit
			}
			if ka, kb := joinInts(a.Bonus), joinInts(b.Bonus); ka != kb {
				return ka < kb
			}
			return a.Tuning < b.Tuning
		})
	}
	return items
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func splitInts(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}

// battleNet holds a client-credentials token for the player's own API client.
type battleNet struct {
	client  func() (id, secret string, ok bool)
	http    *http.Client
	now     func() time.Time
	id      string // the client the token was issued to
	token   string
	expires time.Time
}

func (b *battleNet) bearer(ctx context.Context) (string, error) {
	if b.token != "" && b.now().Before(b.expires) {
		return b.token, nil
	}
	id, secret, ok := b.client()
	if !ok {
		return "", errNoBattleNetClient
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, battleNetTokenURL,
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	resp, err := b.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return "", fmt.Errorf("%w: %s", errBattleNetRefused, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Battle.net token service answered %s", resp.Status)
	}
	var answer struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil || answer.Token == "" {
		return "", fmt.Errorf("Battle.net sent no token: %v", err)
	}
	b.id, b.token, b.expires = id, answer.Token, b.now().Add(time.Duration(answer.Expires)*time.Second-10*time.Minute)
	return b.token, nil
}

// get asks for one API path. With since set, an unchanged response comes back as nil.
func (b *battleNet) get(ctx context.Context, region, path, since string) (*http.Response, error) {
	token, err := b.bearer(ctx)
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		battleNetAPI(region)+path+sep+"namespace=dynamic-forever-"+region, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	if since != "" {
		req.Header.Set("If-Modified-Since", since)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusOK && (since == "" || resp.Header.Get("Last-Modified") != since):
		return resp, nil
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified:
		resp.Body.Close()
		return nil, nil
	case resp.StatusCode == http.StatusUnauthorized:
		b.token = ""
	}
	resp.Body.Close()
	return nil, fmt.Errorf("%s answered %s", path, resp.Status)
}

// auctionSource gives one snapshot part as Blizzard sent it, or nil when it has not changed since the given time.
type auctionSource interface {
	get(ctx context.Context, region, path, since string) (*http.Response, error)
}

// auctionMirror is the project's mirror of every Forever auction house: the same JSON and Last-Modified as Blizzard,
// for the token the server gave at the Battle.net sign-in.
type auctionMirror struct {
	base  string
	http  *http.Client
	token string
}

func (m *auctionMirror) get(ctx context.Context, region, path, since string) (*http.Response, error) {
	part := mirrorPath.FindStringSubmatch(path)
	if part == nil {
		return nil, fmt.Errorf("the auction mirror has no %s", path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+"/v1/auctions/"+region+"/"+part[1]+part[2], nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	req.Header.Set("Authorization", "Bearer "+m.token)
	if since != "" {
		req.Header.Set("If-Modified-Since", since)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		resp.Body.Close()
		return nil, errSignInNeeded
	case resp.StatusCode == http.StatusOK && (since == "" || resp.Header.Get("Last-Modified") != since):
		return resp, nil
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified:
		resp.Body.Close()
		return nil, nil
	}
	resp.Body.Close()
	return nil, fmt.Errorf("the auction mirror answered %s for %s", resp.Status, req.URL.Path)
}

type auctionWatcher struct {
	dir    string // <settings folder>/auctions
	bn     *battleNet
	mirror func() string // the mirror's address, or "" when it is off
	now    func() time.Time
	lost   func() // after the server turned the sign-in token down

	tried    map[string]time.Time // market key: when it was last asked about
	modified map[string]time.Time // market key: Blizzard's Last-Modified of the stored book
}

func newAuctionWatcher(dir string) *auctionWatcher {
	return &auctionWatcher{dir: dir, now: time.Now, mirror: auctionMirrorURL,
		bn: &battleNet{client: battleNetClient, http: &http.Client{Timeout: 3 * time.Minute}, now: time.Now}}
}

// update downloads one house when the source has a newer snapshot of it, and reports whether it did.
func (w *auctionWatcher) update(ctx context.Context, src auctionSource, m auctionMarket) (bool, error) {
	path := filepath.Join(w.dir, m.key()+".json")
	old, _ := loadAuctionBook(path)
	base := fmt.Sprintf("/data/wow/ruleset/%d/auctions/%d", rulesetIDs[m.Ruleset], m.House)
	parts := [][2]string{{"auctions", base}, {"commodities", base + "/commodities"}}
	build := newBookBuilder()
	book := &auctionBook{Market: m, Modified: map[string]string{}}
	// read decodes one part unless Blizzard answers that it has not changed since the given time.
	read := func(part [2]string, since string) (bool, error) {
		resp, err := src.get(ctx, m.Region, part[1], since)
		if err != nil || resp == nil {
			return false, err
		}
		defer resp.Body.Close()
		book.Modified[part[0]] = resp.Header.Get("Last-Modified")
		if err := decodeAuctions(io.LimitReader(resp.Body, auctionMaxBody), build.add); err != nil {
			return false, fmt.Errorf("%s: %w", part[1], err)
		}
		return true, nil
	}
	got := map[string]bool{}
	if old != nil {
		for _, part := range parts {
			fresh, err := read(part, old.Modified[part[0]])
			if err != nil {
				return false, err
			}
			got[part[0]] = fresh
		}
		if !got["auctions"] && !got["commodities"] {
			if w.modified != nil {
				w.modified[m.key()] = old.modifiedAt() // the stored book may be newer than this watcher knew
			}
			now := w.now()
			return false, os.Chtimes(path, now, now) // confirmed current
		}
	}
	// A summary covers both parts, so the one that did not change is read again in full.
	for _, part := range parts {
		if !got[part[0]] {
			if _, err := read(part, ""); err != nil {
				return false, err
			}
		}
	}
	book.Items, book.Fetched = build.items(), w.now().UnixMilli()
	data, err := json.Marshal(book)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return false, err
	}
	if w.modified != nil {
		w.modified[m.key()] = book.modifiedAt()
	}
	return true, w.remember(book)
}

// remember appends the lowest price and quantity of every item to the house's history, one line per snapshot and
// one file per day, so that later advice can tell goods that sell from goods that only sit there.
func (w *auctionWatcher) remember(book *auctionBook) error {
	folder := filepath.Join(w.dir, "history", book.Market.key())
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	now := w.now().UTC()
	row := auctionRow{T: book.Fetched, Items: map[int][2]int64{}}
	for id, it := range book.Items {
		row.Items[id] = [2]int64{it.Unit, int64(it.Listed)}
	}
	line, err := json.Marshal(row)
	if err != nil {
		return err
	}
	today := now.Format(time.DateOnly)
	f, err := os.OpenFile(filepath.Join(folder, today+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	// Blizzard's API terms keep data no longer than 30 days; derived prices are kept no longer either.
	cutoff := now.AddDate(0, 0, -auctionHistoryDays).Format(time.DateOnly)
	for _, e := range entries {
		name := e.Name()
		day, _, _ := strings.Cut(name, ".")
		switch {
		case day < cutoff:
			os.Remove(filepath.Join(folder, name))
		case strings.HasSuffix(name, ".jsonl") && day != today:
			if err := gzipFile(filepath.Join(folder, name)); err != nil {
				return err
			}
		}
	}
	// A finished day is also kept as one summary, which get_auction_prices reads (auction_history.go).
	if err := summarizeAuctionHistory(folder, today, cutoff); err != nil {
		logf("Price history of %s: %v", book.Market.label(), err)
	}
	return nil
}

func gzipFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gz-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	z := gzip.NewWriter(f)
	if _, err := z.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replaceFile(f.Name(), path+".gz"); err != nil {
		return err
	}
	return os.Remove(path)
}

type seenMarket struct {
	auctionMarket
	Seen int64 `json:"seen"` // Unix ms
}

// recordMarket notes a market the game reported, so the watcher keeps it fresh for a week.
func recordMarket(dir string, m auctionMarket, now time.Time) error {
	marketsMu.Lock()
	defer marketsMu.Unlock()
	path := filepath.Join(dir, "markets.json")
	seen := map[string]seenMarket{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &seen)
	}
	old, known := seen[m.key()]
	if known && now.Sub(time.UnixMilli(old.Seen)) < time.Hour {
		return nil
	}
	seen[m.key()] = seenMarket{m, now.UnixMilli()}
	data, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	if !known {
		select {
		case auctionWake <- struct{}{}:
		default:
		}
	}
	return nil
}

func recentMarkets(dir string, now time.Time) []auctionMarket {
	marketsMu.Lock()
	defer marketsMu.Unlock()
	seen := map[string]seenMarket{}
	if data, err := os.ReadFile(filepath.Join(dir, "markets.json")); err == nil {
		json.Unmarshal(data, &seen)
	}
	var out []auctionMarket
	for _, m := range seen {
		if now.Sub(time.UnixMilli(m.Seen)) < auctionMarketAge {
			out = append(out, m.auctionMarket)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

// auctionStatus is what the watcher met last time, so that the tools and the journal can say why prices are missing.
type auctionStatus struct {
	Refused string            `json:"refused,omitempty"` // Blizzard's answer when it turned the client down
	Failing map[string]string `json:"failing,omitempty"` // market key: why its last download failed
	Items   string            `json:"items,omitempty"`   // why the item table could not be fetched
}

func loadAuctionStatus(dir string) auctionStatus {
	var s auctionStatus
	if data, err := os.ReadFile(filepath.Join(dir, "status.json")); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

func (w *auctionWatcher) saveStatus(s auctionStatus) {
	data, err := json.Marshal(s)
	if err == nil {
		if err = os.MkdirAll(w.dir, 0o700); err == nil {
			err = writeFileAtomic(filepath.Join(w.dir, "status.json"), data)
		}
	}
	if err != nil {
		logf("Couldn't note the auction status: %v", err)
	}
}

// source is where snapshots come from: the player's own Battle.net client, otherwise the project's mirror with the
// sign-in token (also returned, to drop it when the server refuses it); nil when neither is set up.
func (w *auctionWatcher) source() (auctionSource, string) {
	// Read the client every time: one removed or replaced in the menu must not go on with the old token.
	id, _, ok := w.bn.client()
	if !ok || id != w.bn.id {
		w.bn.token = ""
	}
	if ok {
		return w.bn, ""
	}
	base, token := w.mirror(), backendToken()
	if base == "" || token == "" {
		return nil, "" // without the sign-in the tools and the journal say how to get it
	}
	return &auctionMirror{base: base, http: w.bn.http, token: token}, token
}

// refresh asks about every market whose next snapshot is due (nextAuctionCheck) and returns when the next one is.
func (w *auctionWatcher) refresh(ctx context.Context) time.Time {
	now := w.now()
	next := now.Add(auctionEvery)
	status := auctionStatus{Failing: map[string]string{}}
	src, token := w.source()
	if src == nil {
		w.saveStatus(status)
		return next
	}
	if w.tried == nil {
		w.tried, w.modified = map[string]time.Time{}, map[string]time.Time{}
	}
	previous := loadAuctionStatus(w.dir)
	markets := recentMarkets(w.dir, now)
	for _, m := range markets {
		key := m.key()
		if _, known := w.modified[key]; !known {
			if book, err := loadAuctionBook(filepath.Join(w.dir, key+".json")); err == nil {
				w.modified[key] = book.modifiedAt()
			} else {
				w.modified[key] = time.Time{}
			}
		}
		if due := nextAuctionCheck(w.modified[key], w.tried[key], now); due.After(now) {
			if why := previous.Failing[key]; why != "" {
				status.Failing[key] = why
			}
			if due.Before(next) {
				next = due
			}
			continue
		}
		w.tried[key] = now
		changed, err := w.update(ctx, src, m)
		switch {
		case errors.Is(err, errNoBattleNetClient):
			w.saveStatus(auctionStatus{})
			return now.Add(auctionEvery)
		case errors.Is(err, errBattleNetRefused):
			logf("Auction prices: %v", err)
			w.saveStatus(auctionStatus{Refused: err.Error()})
			return now.Add(auctionEvery)
		case errors.Is(err, errSignInNeeded):
			logf("Auction prices: %v; the stored sign-in is removed", err)
			dropBackendToken(token, w.lost)
			w.saveStatus(auctionStatus{})
			return now.Add(auctionEvery)
		case err != nil:
			logf("Auction house %s: %v", m.label(), err)
			status.Failing[key] = err.Error()
		case changed:
			logf("Auction house %s: new snapshot downloaded", m.label())
		}
		if due := nextAuctionCheck(w.modified[key], now, now); due.Before(next) {
			next = due
		}
	}
	if len(markets) > 0 {
		if err := w.refreshItems(ctx); err != nil {
			logf("Item table for auction upgrades: %v", err)
			status.Items = err.Error()
		}
	}
	w.saveStatus(status)
	return next
}

// watchAuctions runs in the tray for its whole life, with or without the game.
func (a *app) watchAuctions() {
	w := newAuctionWatcher(auctionDir(snapshotDir()))
	w.lost = a.noteBackendToken
	wait := time.NewTimer(30 * time.Second)
	defer wait.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-auctionWake:
		case <-wait.C:
		}
		next := w.refresh(a.ctx)
		wait.Reset(min(max(time.Until(next), time.Minute), auctionEvery))
	}
}
