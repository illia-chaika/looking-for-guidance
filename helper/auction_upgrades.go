package main

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"
)

// Auction upgrades: the helper picks the auctions for one equipment slot within the player's budget, the game says
// which the character can wear and what each variant gives, and the adviser weighs them for the class. Slot and
// item class come from the client's own Item table as wago.tools publishes it, downloaded at run time and kept
// in the settings folder only: Blizzard's item API misses most older gear.

const (
	upgradeTimeout     = 60 * time.Second
	maxUpgradeItems    = 400 // items sent to the game per search
	maxUpgradeVariants = 6   // random-suffix variants per item
	itemTableEvery     = 24 * time.Hour
)

var (
	wagoBuildsURL = "https://wago.tools/api/builds"
	wagoTableURL  = func(table, build string) string { return "https://wago.tools/db2/" + table + "/csv?build=" + build }
)

// Inventory types of the client's Item table per slot the adviser can name.
var upgradeSlots = map[string][]int{
	"head": {1}, "neck": {2}, "shoulder": {3}, "chest": {5, 20}, "waist": {6}, "legs": {7}, "feet": {8},
	"wrist": {9}, "hands": {10}, "finger": {11}, "trinket": {12}, "back": {16},
	"main_hand": {13, 17, 21}, "off_hand": {13, 14, 22, 23}, "ranged": {15, 25, 26, 28},
}

// itemTable is the gear of one game build: class, subclass and inventory type by item ID.
type itemTable struct {
	Build string         `json:"build"`
	Items map[int][3]int `json:"items"`
}

func loadItemTable(dir string) (*itemTable, error) {
	data, err := os.ReadFile(filepath.Join(dir, "items.json"))
	if err != nil {
		return nil, err
	}
	var t itemTable
	return &t, json.Unmarshal(data, &t)
}

// refreshItems keeps items.json at the newest WoW: Forever build wago.tools knows, checking once a day.
func (w *auctionWatcher) refreshItems(ctx context.Context) error {
	path := filepath.Join(w.dir, "items.json")
	if info, err := os.Stat(path); err == nil && w.now().Sub(info.ModTime()) < itemTableEvery {
		return nil
	}
	body, err := w.fetch(ctx, wagoBuildsURL)
	if err != nil {
		return err
	}
	var builds map[string][]struct {
		Version string `json:"version"`
	}
	err = json.NewDecoder(io.LimitReader(body, 16<<20)).Decode(&builds)
	body.Close()
	if err != nil || len(builds["wowf"]) == 0 {
		return fmt.Errorf("wago.tools lists no WoW: Forever build: %v", err)
	}
	build := builds["wowf"][0].Version
	if old, err := loadItemTable(w.dir); err == nil && old.Build == build {
		now := w.now()
		return os.Chtimes(path, now, now)
	}
	body, err = w.fetch(ctx, wagoTableURL("Item", build))
	if err != nil {
		return err
	}
	defer body.Close()
	table, err := parseItemTable(io.LimitReader(body, 64<<20))
	if err != nil {
		return err
	}
	table.Build = build
	data, err := json.Marshal(table)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func (w *auctionWatcher) fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	resp, err := w.bn.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return resp.Body, nil
}

// parseItemTable keeps weapons (class 2) and armour (class 4) from the Item table's CSV.
func parseItemTable(r io.Reader) (*itemTable, error) {
	rows := csv.NewReader(r)
	head, err := rows.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, name := range head {
		col[name] = i
	}
	for _, name := range []string{"ID", "ClassID", "SubclassID", "InventoryType"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("the Item table has no %s column", name)
		}
	}
	t := &itemTable{Items: map[int][3]int{}}
	for {
		row, err := rows.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		num := func(name string) int { n, _ := strconv.Atoi(row[col[name]]); return n }
		if class := num("ClassID"); class == 2 || class == 4 {
			t.Items[num("ID")] = [3]int{class, num("SubclassID"), num("InventoryType")}
		}
	}
	if len(t.Items) == 0 {
		return nil, errors.New("the Item table lists no gear")
	}
	return t, nil
}

type upgradeQuery struct {
	Slot  string        `json:"slot"`
	Ahead int           `json:"ahead"`
	Items []upgradeItem `json:"items"`
}

// upgradeItem lists the cheapest asking price of each random-suffix variant as [price, bonus IDs…].
type upgradeItem struct {
	ID       int       `json:"i"`
	Variants [][]int64 `json:"v"`
}

// upgradeCandidates picks the slot's items whose cheapest variant fits the budget, dearest first, and says how
// many fitting items were left out. Variants that differ only by ContentTuningID showed the same stats in the
// game (20.11.2026), so they are merged.
func upgradeCandidates(book *auctionBook, items *itemTable, slot string, budget int64) ([]upgradeItem, int) {
	types := upgradeSlots[slot]
	var out []upgradeItem
	for id, it := range book.Items {
		info, ok := items.Items[id]
		if !ok || !slices.Contains(types, info[2]) {
			continue
		}
		cheapest := map[string]int64{}
		for _, v := range it.Variants {
			key := joinInts(v.Bonus)
			if old, seen := cheapest[key]; v.Unit <= budget && (!seen || v.Unit < old) {
				cheapest[key] = v.Unit
			}
		}
		if len(cheapest) == 0 {
			continue
		}
		c := upgradeItem{ID: id}
		for key, price := range cheapest {
			variant := []int64{price}
			for _, b := range splitInts(key) {
				variant = append(variant, int64(b))
			}
			c.Variants = append(c.Variants, variant)
		}
		sort.Slice(c.Variants, func(i, j int) bool { return c.Variants[i][0] < c.Variants[j][0] })
		c.Variants = c.Variants[:min(len(c.Variants), maxUpgradeVariants)]
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Variants[0][0], out[j].Variants[0][0]; a != b {
			return a > b
		}
		return out[i].ID < out[j].ID
	})
	left := max(0, len(out)-maxUpgradeItems)
	return out[:min(len(out), maxUpgradeItems)], left
}

type upgradeArgs struct {
	ID     string `json:"snapshot_id"`
	Slot   string `json:"slot"`
	Budget int64  `json:"budget"`
	Ahead  int    `json:"levels_ahead,omitempty"`
}

func upgradeToolSchema() mcpObject {
	slots := make([]string, 0, len(upgradeSlots))
	for slot := range upgradeSlots {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	return mcpObject{
		"slot":         mcpObject{"type": "string", "enum": slots},
		"budget":       mcpObject{"type": "integer", "minimum": 1, "description": "Most the player will pay for one item, in copper (10000 = 1 gold)"},
		"levels_ahead": mcpObject{"type": "integer", "minimum": 0, "maximum": 5, "description": "Also include items the character can wear within this many levels"},
	}
}

func queryAuctionUpgrades(dir string, raw json.RawMessage) (mcpObject, error) {
	var a upgradeArgs
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	if upgradeSlots[a.Slot] == nil || a.Budget < 1 || a.Ahead < 0 || a.Ahead > 5 {
		return nil, errors.New("name a slot from the list, a budget in copper and levels_ahead from 0 to 5")
	}
	s, err := loadGameSnapshot(dir, a.ID)
	if err != nil {
		return nil, err
	}
	m, err := snapshotMarket(s)
	if problem := auctionProblem(dir, m, err); problem != nil {
		return nil, problem
	}
	book, err := loadAuctionBook(filepath.Join(auctionDir(dir), m.key()+".json"))
	if err != nil {
		return nil, err
	}
	items, err := loadItemTable(auctionDir(dir))
	if err != nil {
		if why := loadAuctionStatus(auctionDir(dir)).Items; why != "" {
			return nil, &auctionUnavailable{"failing", "Downloading the item table for upgrade searches fails: " + why + ".",
				"The companion tries again every 20 minutes; meanwhile get_auction_prices still works for named items."}
		}
		return nil, &auctionUnavailable{"pending", "The item table for upgrade searches is still downloading.", "Ask again in a few minutes."}
	}
	if !s.Lookup || s.RunID == "" || s.Session == "" {
		return nil, errors.New("this snapshot has no live lookup; ask in the running game")
	}
	if err := verifyDetailBridge(dir, s, time.Now()); err != nil {
		return nil, err
	}
	candidates, left := upgradeCandidates(book, items, a.Slot, a.Budget)
	out := mcpObject{"captured": time.UnixMilli(book.Fetched).UTC().Format(time.RFC3339), "market": m.label(), "slot": a.Slot,
		"note": "Asking prices in copper; stats are the game's own for each variant, compared by you with what the character wears. Items the character cannot use were dropped by the game."}
	if at := book.modifiedAt(); !at.IsZero() {
		out["snapshot_at"] = at.UTC().Format(time.RFC3339)
		if time.Since(at) > auctionStaleAfter {
			out["stale"] = true
		}
	}
	if len(candidates) == 0 {
		out["checked"] = 0
		return out, nil
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	req := detailRequest{ID: hex.EncodeToString(key[:]), Session: s.Session,
		Upgrade: &upgradeQuery{Slot: a.Slot, Ahead: a.Ahead, Items: candidates}}
	data, err := askGame(dir, s, req, upgradeTimeout)
	if err != nil {
		return nil, err
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, errors.New("invalid upgrade reply from the game")
	}
	for k, v := range answer {
		out[k] = v
	}
	out["sent_to_game"] = len(candidates)
	if left > 0 {
		out["not_checked"] = fmt.Sprintf("%d cheaper items within the budget were not checked; lower the budget to see them", left)
	}
	return out, nil
}
