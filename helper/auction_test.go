package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuctionMarketFollowsRegionRulesetAndFaction(t *testing.T) {
	cases := []struct {
		facts marketFacts
		house int
		fails string
	}{
		{marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}, 114, ""},
		{marketFacts{Region: "US", Ruleset: "PvP", Faction: "Alliance"}, 110, ""},
		{marketFacts{Region: "EU", Ruleset: "RP", Faction: "Horde"}, 0, "not known yet"},
		{marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde", Hardcore: true}, 0, "hardcore"},
		{marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Neutral"}, 0, "faction"},
		{marketFacts{Region: "CN", Ruleset: "PvE", Faction: "Horde"}, 0, "region"},
		{marketFacts{Ruleset: "PvE", Faction: "Horde"}, 0, "region"},
		{marketFacts{Region: "EU", Faction: "Horde"}, 0, "ruleset"},
	}
	for _, c := range cases {
		m, err := c.facts.market()
		if c.fails != "" {
			if err == nil || !strings.Contains(err.Error(), c.fails) {
				t.Errorf("%+v: want an error about %s, got %v", c.facts, c.fails, err)
			}
			continue
		}
		if err != nil || m.House != c.house || m.Region != strings.ToLower(c.facts.Region) {
			t.Errorf("%+v: got %+v, %v", c.facts, m, err)
		}
	}

	s := parseGameSnapshot("Game: WoW Forever 1.60.1.70938 · EU · PvE\nCharacter: level 15 Orc Warrior, Horde · 1g 0s 0c", 1)
	if m, err := snapshotMarket(s); err != nil || m.key() != "eu-114" || m.label() != "EU Normal Horde (house 114)" {
		t.Fatal(m, err)
	}
	s = parseGameSnapshot("Game: WoW Forever 1.60.1.70938 · EU · PvE · hardcore\nCharacter: level 15 Orc Warrior, Horde", 1)
	if _, err := snapshotMarket(s); err == nil {
		t.Fatal("a hardcore character has no listed market")
	}
	s = parseGameSnapshot("Game: WoW Forever 1.60.1.70938\nCharacter: level 15 Orc Warrior, Horde", 1)
	if _, err := snapshotMarket(s); err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatal("an older add-on reports no region:", err)
	}
}

func TestLinkVariantReadsBonusesAndContentTuning(t *testing.T) {
	bonus, tuning, err := linkVariant("|cff1eff00|Hitem:4562::::::::10:1490:::1:12721::::::|h[Axe]|h|r", 4562)
	if err != nil || len(bonus) != 1 || bonus[0] != 12721 || tuning != 0 {
		t.Fatal(bonus, tuning, err)
	}
	bonus, tuning, err = linkVariant("item:7469::::::::15::::2:12731:12718:1:28:5254", 7469)
	if err != nil || joinInts(bonus) != "12718,12731" || tuning != 5254 {
		t.Fatal(bonus, tuning, err)
	}
	if _, _, err := linkVariant("item:2589::::::::", 7469); err == nil {
		t.Fatal("a link for another item is refused")
	}
}

// A made-up house in the shape of Blizzard's responses.
const testHouse = `{"_links":{"self":{"href":"x"}},"id":114,"auctions":[
{"id":1,"item":{"id":7469,"bonus_lists":[12718],"modifiers":[{"type":28,"value":5254}]},"buyout":50000,"quantity":1,"time_left":"LONG"},
{"id":2,"item":{"id":7469,"bonus_lists":[12718],"modifiers":[{"type":28,"value":5254}]},"buyout":42000,"quantity":1,"time_left":"SHORT"},
{"id":3,"item":{"id":7469,"bonus_lists":[12719]},"buyout":30000,"quantity":1,"time_left":"LONG"},
{"id":4,"item":{"id":4306},"buyout":2000,"quantity":10,"time_left":"LONG"},
{"id":5,"item":{"id":9999},"bid":500,"quantity":1,"time_left":"LONG"}],"commodities":{"href":"x"}}`

const testCommodities = `{"_links":{"self":{"href":"x"}},"auctions":[
{"id":10,"item":{"id":2589},"quantity":20,"unit_price":25,"time_left":"LONG"},
{"id":11,"item":{"id":2589},"quantity":5,"unit_price":24,"time_left":"SHORT"},
{"id":12,"item":{"id":2589},"quantity":100,"unit_price":30,"time_left":"LONG"}]}`

func fakeBattleNet(t *testing.T) (requests *[]string) {
	t.Helper()
	var seen []string
	modified := "Fri, 20 Nov 2026 17:23:20 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if id, secret, ok := r.BasicAuth(); !ok || id != "client" || secret != "secret" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"access_token":"tok","expires_in":86399}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("namespace") != "dynamic-forever-eu" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		seen = append(seen, r.URL.Path+" "+r.Header.Get("If-Modified-Since"))
		if r.Header.Get("If-Modified-Since") == modified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", modified)
		switch r.URL.Path {
		case "/data/wow/ruleset/2/auctions/114":
			fmt.Fprint(w, testHouse)
		case "/data/wow/ruleset/2/auctions/114/commodities":
			fmt.Fprint(w, testCommodities)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldToken, oldAPI := battleNetTokenURL, battleNetAPI
	battleNetTokenURL, battleNetAPI = srv.URL+"/token", func(string) string { return srv.URL }
	t.Cleanup(func() { battleNetTokenURL, battleNetAPI = oldToken, oldAPI })
	return &seen
}

func testWatcher(dir string, now time.Time) *auctionWatcher {
	clock := func() time.Time { return now }
	return &auctionWatcher{dir: dir, now: clock, bn: &battleNet{http: http.DefaultClient, now: clock,
		client: func() (string, string, bool) { return "client", "secret", true }}}
}

func TestAuctionDownloadSummarisesAndSkipsUnchangedSnapshots(t *testing.T) {
	requests := fakeBattleNet(t)
	root := t.TempDir()
	snapshots := filepath.Join(root, "snapshots")
	dir := auctionDir(snapshots)
	now := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	w := testWatcher(dir, now)
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()

	if changed, err := w.update(context.Background(), m); err != nil || !changed {
		t.Fatal(changed, err)
	}
	book, err := loadAuctionBook(filepath.Join(dir, "eu-114.json"))
	if err != nil {
		t.Fatal(err)
	}
	linen := book.Items[2589]
	if linen.Unit != 24 || linen.Listed != 125 || linen.Listings != 3 || len(linen.Variants) != 1 || linen.Variants[0].Tiers[0] != [2]int64{24, 5} {
		t.Fatalf("commodity summary: %+v", linen)
	}
	stack := book.Items[4306]
	if stack.Unit != 200 || stack.Listed != 10 {
		t.Fatalf("a stack is priced per unit: %+v", stack)
	}
	gear := book.Items[7469]
	if gear.Unit != 30000 || len(gear.Variants) != 2 || gear.Variants[1].Tuning != 5254 || gear.Variants[1].Unit != 42000 || gear.Variants[1].Listings != 2 {
		t.Fatalf("variants are kept apart, cheapest first: %+v", gear.Variants)
	}
	if book.Items[9999] != nil {
		t.Fatal("a bid-only auction has no price to buy now")
	}
	if !book.modifiedAt().Equal(time.Date(2026, 11, 20, 17, 23, 20, 0, time.UTC)) {
		t.Fatal(book.Modified)
	}
	history, err := os.ReadFile(filepath.Join(dir, "history", "eu-114", "2026-11-20.jsonl"))
	if err != nil || !strings.Contains(string(history), `"2589":[24,125]`) {
		t.Fatalf("history line: %s %v", history, err)
	}

	*requests = nil
	if changed, err := w.update(context.Background(), m); err != nil || changed {
		t.Fatal("an unchanged house is not downloaded again:", changed, err)
	}
	if len(*requests) != 2 || !strings.HasSuffix((*requests)[0], "GMT") {
		t.Fatal("both parts are asked with If-Modified-Since:", *requests)
	}

	w.bn.client = func() (string, string, bool) { return "", "", false }
	w.bn.token = ""
	if _, err := w.update(context.Background(), m); err != errNoBattleNetClient {
		t.Fatal("without a client nothing is fetched:", err)
	}
}

func TestAuctionHistoryCompressesFinishedDaysAndDropsOldOnes(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "history", "eu-114")
	os.MkdirAll(folder, 0o700)
	os.WriteFile(filepath.Join(folder, "2026-11-19.jsonl"), []byte("{}\n"), 0o600)
	os.WriteFile(filepath.Join(folder, "2026-07-01.jsonl.gz"), []byte("old"), 0o600)
	w := testWatcher(dir, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if err := w.remember(&auctionBook{Market: m, Fetched: 1, Items: map[int]*auctionItem{2589: {Unit: 24, Listed: 5}}}); err != nil {
		t.Fatal(err)
	}
	var names []string
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "2026-11-19.jsonl.gz 2026-11-20.jsonl" {
		t.Fatal(names)
	}
}

func TestRecordMarketWakesTheWatcherForANewMarketOnly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	for len(auctionWake) > 0 {
		<-auctionWake
	}
	if err := recordMarket(dir, m, now); err != nil || len(auctionWake) != 1 {
		t.Fatal("a new market wakes the watcher:", err, len(auctionWake))
	}
	<-auctionWake
	recordMarket(dir, m, now.Add(2*time.Hour))
	if len(auctionWake) != 0 {
		t.Fatal("a known market does not")
	}
	if got := recentMarkets(dir, now.Add(6*24*time.Hour)); len(got) != 1 || got[0].House != 114 {
		t.Fatal(got)
	}
	if got := recentMarkets(dir, now.Add(10*24*time.Hour)); len(got) != 0 {
		t.Fatal("a market not seen for a week is left alone:", got)
	}
}

func TestAuctionPricesToolAnswersForTheCharactersMarket(t *testing.T) {
	fakeBattleNet(t)
	root := t.TempDir()
	dir := filepath.Join(root, "snapshots")
	if auctionReady(dir) {
		t.Fatal("nothing downloaded yet")
	}
	for _, tool := range gameTools(dir) {
		if tool["name"] == "get_auction_prices" {
			t.Fatal("the tool is not offered without auction data")
		}
	}
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if _, err := testWatcher(auctionDir(dir), time.Now()).update(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, tool := range gameTools(dir) {
		offered = offered || tool["name"] == "get_auction_prices"
	}
	if !offered {
		t.Fatal("the tool is offered once a house is downloaded")
	}

	ask := func(context, args string) (mcpObject, error) {
		t.Helper()
		id, err := saveGameSnapshot(dir, parseGameSnapshot(context, 1770000000))
		if err != nil {
			t.Fatal(err)
		}
		return queryGameSnapshot(dir, "get_auction_prices", json.RawMessage(fmt.Sprintf(`{"snapshot_id":%q,%s}`, id, args)))
	}
	horde := "Game: WoW Forever 1.60.1.70938 · EU · PvE\nCharacter: level 15 Orc Warrior, Horde · 1g 0s 0c"
	out, err := ask(horde, `"items":[{"id":2589},{"id":7469},{"id":7469,"link":"item:7469::::::::15::::1:12718:1:28:5254"},{"id":7469,"link":"item:7469::::::::15::::1:12720::::"},{"id":5}],"quantity":30`)
	if err != nil {
		t.Fatal(err)
	}
	items := out["items"].([]mcpObject)
	if out["captured"] == "" || out["market"] != "EU Normal Horde (house 114)" || out["snapshot_at"] != "2026-11-20T17:23:20Z" {
		t.Fatal(out)
	}
	if linen := items[0]; linen["unit"] != int64(24) || linen["listed"] != 125 || linen["cost_for_quantity"] != int64(5*24+20*25+5*30) {
		t.Fatal("commodity:", linen)
	}
	if any := items[1]; any["unit"] != int64(30000) || len(any["variants"].([]mcpObject)) != 2 {
		t.Fatal("all variants:", any)
	}
	if exact := items[2]; exact["unit"] != int64(42000) || exact["listed"] != 2 || exact["variants"] != nil {
		t.Fatal("the linked variant:", exact)
	}
	if missing := items[3]; missing["listed"] != 0 || len(missing["other_variants"].([]mcpObject)) != 2 {
		t.Fatal("a variant nobody sells:", missing)
	}
	if none := items[4]; none["listed"] != 0 || none["unit"] != nil {
		t.Fatal("an item nobody sells:", none)
	}

	if _, err := ask("Game: WoW Forever 1.60.1.70938 · EU · RP\nCharacter: level 15 Orc Warrior, Horde", `"items":[{"id":2589}]`); err == nil || !strings.Contains(err.Error(), "not known yet") {
		t.Fatal("a Roleplay market is not guessed:", err)
	}
	if _, err := ask("Game: WoW Forever 1.60.1.70938 · EU · PvP\nCharacter: level 15 Orc Warrior, Horde", `"items":[{"id":2589}]`); err == nil || !strings.Contains(err.Error(), "house 113") {
		t.Fatal("another market without data says so:", err)
	}
	if _, err := ask(horde, `"items":[]`); err == nil {
		t.Fatal("an empty list is refused")
	}
}

func TestAuctionToolShowsInTheActivityLine(t *testing.T) {
	const f = `{"id":"t","status":"completed","items":[{"type":"mcpToolCall","id":"1","server":"wow_game","tool":"get_auction_prices","status":"completed","arguments":{"items":[{"id":2589}],"snapshot_id":"x"},"result":{"content":[{"text":"{\"captured\":\"2026-11-20T17:30:00Z\",\"items\":[]}"}]}},{"type":"agentMessage","text":"24 медяка"}]}`
	var turn cxTurn
	if err := json.Unmarshal([]byte(f), &turn); err != nil {
		t.Fatal(err)
	}
	got := transcript([]cxTurn{turn}, "", nil)
	if len(got) != 2 || len(got[0].Events) != 1 || got[0].Events[0].Label != "Аукцион" || got[0].Events[0].Status != "ok" {
		t.Fatalf("%+v", got)
	}
}
