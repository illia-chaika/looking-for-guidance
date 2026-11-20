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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The auction house of the player's market, from Blizzard's Game Data API. The tray downloads the houses the game
// reported recently and keeps a short summary per item; the game data server reads it for get_auction_prices.
// Nothing happens without the player's own Battle.net API client in the keychain (battleNetClient).

const (
	battleNetAccount       = "battlenet"
	battleNetIDService     = "lfg-battlenet-id"
	battleNetSecretService = "lfg-battlenet-secret"

	auctionEvery       = 20 * time.Minute
	auctionMarketAge   = 7 * 24 * time.Hour // markets the game reported this recently are kept fresh
	auctionHistoryDays = 90
	auctionTiers       = 10       // cheapest price levels kept per variant
	auctionContentTune = 28       // the API's modifier type for ContentTuningID: where the item dropped decides its level
	auctionMaxBody     = 64 << 20 // one house was 27 MB of JSON in November 2026
)

var (
	battleNetTokenURL = "https://oauth.battle.net/token"
	battleNetAPI      = func(region string) string { return "https://" + region + ".api.blizzard.com" }

	errNoBattleNetClient = errors.New("no Battle.net API client in the keychain")

	// auctionWake asks the tray's watcher to look now, when the game reports a market it has not downloaded yet.
	auctionWake = make(chan struct{}, 1)
	marketsMu   sync.Mutex
)

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
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Battle.net refused the API client: %s", resp.Status)
	}
	var answer struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil || answer.Token == "" {
		return "", fmt.Errorf("Battle.net sent no token: %v", err)
	}
	b.token, b.expires = answer.Token, b.now().Add(time.Duration(answer.Expires)*time.Second-10*time.Minute)
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

type auctionWatcher struct {
	dir string // <settings folder>/auctions
	bn  *battleNet
	now func() time.Time
}

func newAuctionWatcher(dir string) *auctionWatcher {
	return &auctionWatcher{dir: dir, now: time.Now,
		bn: &battleNet{client: battleNetClient, http: &http.Client{Timeout: 3 * time.Minute}, now: time.Now}}
}

// update downloads one house when Blizzard has a newer snapshot of it, and reports whether it did.
func (w *auctionWatcher) update(ctx context.Context, m auctionMarket) (bool, error) {
	path := filepath.Join(w.dir, m.key()+".json")
	old, _ := loadAuctionBook(path)
	base := fmt.Sprintf("/data/wow/ruleset/%d/auctions/%d", rulesetIDs[m.Ruleset], m.House)
	parts := [][2]string{{"auctions", base}, {"commodities", base + "/commodities"}}
	if old != nil {
		changed := false
		for _, part := range parts {
			resp, err := w.bn.get(ctx, m.Region, part[1], old.Modified[part[0]])
			if err != nil {
				return false, err
			}
			if resp != nil {
				resp.Body.Close()
				changed = true
			}
		}
		if !changed {
			return false, nil
		}
	}
	build := newBookBuilder()
	book := &auctionBook{Market: m, Modified: map[string]string{}}
	for _, part := range parts {
		resp, err := w.bn.get(ctx, m.Region, part[1], "")
		if err != nil {
			return false, err
		}
		err = decodeAuctions(io.LimitReader(resp.Body, auctionMaxBody), build.add)
		book.Modified[part[0]] = resp.Header.Get("Last-Modified")
		resp.Body.Close()
		if err != nil {
			return false, fmt.Errorf("%s: %w", part[1], err)
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
	row := struct {
		T     int64            `json:"t"`
		Items map[int][2]int64 `json:"items"`
	}{book.Fetched, map[int][2]int64{}}
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

func (w *auctionWatcher) refresh(ctx context.Context) {
	for _, m := range recentMarkets(w.dir, w.now()) {
		changed, err := w.update(ctx, m)
		switch {
		case errors.Is(err, errNoBattleNetClient):
			return
		case err != nil:
			logf("Auction house %s: %v", m.label(), err)
		case changed:
			logf("Auction house %s: new snapshot downloaded", m.label())
		}
	}
}

// watchAuctions runs in the tray for its whole life, with or without the game.
func (a *app) watchAuctions() {
	w := newAuctionWatcher(auctionDir(snapshotDir()))
	wait := time.NewTimer(30 * time.Second)
	defer wait.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-auctionWake:
		case <-wait.C:
		}
		w.refresh(a.ctx)
		wait.Reset(auctionEvery)
	}
}
