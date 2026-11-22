package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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

// commoditiesModified lets a test give the commodities part a newer snapshot than the rest of the house.
var commoditiesModified string

func fakeBattleNet(t *testing.T) (requests *[]string) {
	t.Helper()
	var seen []string
	modified := "Fri, 20 Nov 2026 17:23:20 GMT"
	commoditiesModified = modified
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
		current := modified
		if strings.HasSuffix(r.URL.Path, "/commodities") {
			current = commoditiesModified
		}
		if r.Header.Get("If-Modified-Since") == current {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", current)
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
	return &auctionWatcher{dir: dir, now: clock, mirror: auctionMirrorURL, bn: &battleNet{http: http.DefaultClient, now: clock,
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

	if changed, err := w.update(context.Background(), w.bn, m); err != nil || !changed {
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
	if changed, err := w.update(context.Background(), w.bn, m); err != nil || changed {
		t.Fatal("an unchanged house is not downloaded again:", changed, err)
	}
	if len(*requests) != 2 || !strings.HasSuffix((*requests)[0], "GMT") {
		t.Fatal("both parts are asked with If-Modified-Since:", *requests)
	}

	// Only the commodities changed: they are read from the conditional answer, the rest once more in full.
	commoditiesModified = "Fri, 20 Nov 2026 18:23:20 GMT"
	*requests = nil
	if changed, err := w.update(context.Background(), w.bn, m); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if len(*requests) != 3 || !strings.HasSuffix((*requests)[2], "/auctions/114 ") {
		t.Fatal("one conditional request per part and one full one for the unchanged part:", *requests)
	}
	again, _ := loadAuctionBook(filepath.Join(dir, "eu-114.json"))
	if again.Items[7469] == nil || again.Items[2589].Listed != 125 || again.Modified["commodities"] != commoditiesModified {
		t.Fatalf("the summary covers both parts: %+v", again.Modified)
	}

	w.bn.client = func() (string, string, bool) { return "", "", false }
	w.bn.token = ""
	if _, err := w.update(context.Background(), w.bn, m); err != errNoBattleNetClient {
		t.Fatal("without a client nothing is fetched:", err)
	}
}

func TestAuctionHistoryCompressesFinishedDaysAndDropsOldOnes(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "history", "eu-114")
	os.MkdirAll(folder, 0o700)
	os.WriteFile(filepath.Join(folder, "2026-11-19.jsonl"), []byte("{}\n"), 0o600)
	os.WriteFile(filepath.Join(folder, "2026-07-01.jsonl.gz"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(folder, "2026-10-20.jsonl.gz"), []byte("31 days"), 0o600)
	os.WriteFile(filepath.Join(folder, "2026-10-21.jsonl.gz"), []byte("30 days"), 0o600)
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
	if strings.Join(names, " ") != "2026-10-21.jsonl.gz 2026-11-19.jsonl.gz 2026-11-20.jsonl" {
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

// unavailable checks a tool's answer without prices: JSON with the code, the reason and what the player can do.
func unavailable(t *testing.T, err error, code string) {
	t.Helper()
	var answer map[string]any
	if err == nil || json.Unmarshal([]byte(err.Error()), &answer) != nil || answer["available"] != false ||
		answer["code"] != code || answer["reason"] == "" || answer["what_to_do"] == "" {
		t.Fatalf("want an unavailable answer with code %s, got %v", code, err)
	}
}

// useBattleNetClient stores the test client in the in-memory credential store for one test.
func useBattleNetClient(t *testing.T) {
	t.Helper()
	if err := saveBattleNetClient("client", "secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { forgetBattleNetClient() })
}

func TestAuctionPricesToolAnswersForTheCharactersMarket(t *testing.T) {
	fakeBattleNet(t)
	root := t.TempDir()
	dir := filepath.Join(root, "snapshots")
	offered := map[any]bool{}
	for _, tool := range gameTools(dir) {
		offered[tool["name"]] = true
	}
	if !offered["get_auction_prices"] || !offered["find_auction_upgrades"] {
		t.Fatal("both auction tools are offered before there is any data, to say why there are no prices")
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
	linen := `"items":[{"id":2589}]`

	_, err := ask(horde, linen)
	unavailable(t, err, "setup")
	if !strings.Contains(err.Error(), "auctionMirror") || !strings.Contains(err.Error(), "Own Battle.net API client") || !strings.Contains(err.Error(), "develop.battle.net") {
		t.Fatal("without a client or the mirror the answer names the setting, the menu item and the page:", err)
	}
	useBattleNetClient(t)
	_, err = ask(horde, linen)
	unavailable(t, err, "pending")
	w := testWatcher(auctionDir(dir), time.Now())
	w.saveStatus(auctionStatus{Failing: map[string]string{"eu-114": "/data/wow/ruleset/2/auctions/114 answered 503 Service Unavailable"}})
	if _, err = ask(horde, linen); !strings.Contains(fmt.Sprint(err), "503") {
		t.Fatal("a failing download says why:", err)
	}
	unavailable(t, err, "failing")
	w.saveStatus(auctionStatus{Refused: "Battle.net refused the API client: 401 Unauthorized"})
	_, err = ask(horde, linen)
	unavailable(t, err, "refused")
	w.saveStatus(auctionStatus{})

	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if _, err := w.update(context.Background(), w.bn, m); err != nil {
		t.Fatal(err)
	}
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

	_, err = ask("Game: WoW Forever 1.60.1.70938 · EU · RP\nCharacter: level 15 Orc Warrior, Horde", linen)
	if unavailable(t, err, "market"); !strings.Contains(err.Error(), "not known yet") {
		t.Fatal("a Roleplay market is not guessed:", err)
	}
	_, err = ask("Game: WoW Forever 1.60.1.70938 · EU · PvP\nCharacter: level 15 Orc Warrior, Horde", linen)
	if unavailable(t, err, "pending"); !strings.Contains(err.Error(), "house 113") {
		t.Fatal("another market without data says so:", err)
	}
	forgetBattleNetClient()
	_, err = ask(horde, linen)
	unavailable(t, err, "setup")
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

func TestBagTriageReadsAndShowsCurrentAuctionPrices(t *testing.T) {
	useBattleNetClient(t)
	e, mu, prompts := testEngine(t, func(*insightJob, string) (string, error) {
		return `{"summary":"Две вещи на аукцион.","items":[{"item_id":765,"verdict":"auction","reason":"Травники платят 40 медяков."},{"item_id":4562,"verdict":"auction","reason":"Топор берут за 2,5 золотых."}]}`, nil
	})
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	dir := filepath.Join(filepath.Dir(e.dir), "auctions")
	os.MkdirAll(dir, 0o700)
	put := func(herb int64) {
		book := auctionBook{Market: m, Modified: map[string]string{"auctions": "Fri, 20 Nov 2026 17:23:20 GMT"}, Items: map[int]*auctionItem{
			765: {Unit: herb, Listed: 120, Listings: 3, Variants: []*auctionVariant{{Unit: herb, Listed: 120, Listings: 3, Tiers: [][2]int64{{herb, 120}}}}},
			4562: {Unit: 9000, Listed: 3, Listings: 3, Variants: []*auctionVariant{
				{Bonus: []int{12722}, Unit: 9000, Listed: 1, Listings: 1, Tiers: [][2]int64{{9000, 1}}},
				{Bonus: []int{12721}, Unit: 25000, Listed: 2, Listings: 2, Tiers: [][2]int64{{25000, 2}}}}},
		}}
		data, _ := json.Marshal(book)
		os.WriteFile(filepath.Join(dir, "eu-114.json"), data, 0o600)
	}
	put(40)
	req := insightRequest{Kind: "bags", Key: "WARLOCK:10:2-1", Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A",
		Market: &marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"},
		Facts:  json.RawMessage(`{"class":"WARLOCK","level":10,"items":[{"id":765,"name":"Silverleaf","q":1,"n":34,"sell":8},{"id":4562,"name":"Severing Axe of the Monkey","q":2,"n":1,"link":"item:4562::::::::10:1490:::1:12721"}],"priced":[{"id":2589}]}`)}
	runs := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), *prompts...)
	}
	// until polls the shown answer, since the engine answers on its own goroutine.
	until := func(ok func(bagsData) bool) bagsData {
		t.Helper()
		var data bagsData
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			for _, in := range e.view() {
				if in.Kind == "bags" && in.Key == req.Key && in.State == "ready" && json.Unmarshal(in.Data, &data) == nil && ok(data) {
					return data
				}
			}
		}
		t.Fatalf("no matching bags answer: %+v", e.view())
		return data
	}
	herbPrice := func(data bagsData) int64 {
		for _, p := range data.Prices {
			if p.ID == 765 {
				return p.Unit
			}
		}
		return 0
	}
	e.request(req)
	data := until(func(bagsData) bool { return true })
	prompt := runs()[0]
	if !strings.Contains(prompt, `{"4562":[25000,2],"765":[40,120]}`) || strings.Contains(prompt, "priced") || !strings.Contains(prompt, "about 5%") {
		t.Fatalf("the prompt carries the prices of the variants it judges and nothing ruled out: %s", prompt)
	}
	at := time.Date(2026, 11, 20, 17, 23, 20, 0, time.UTC).Unix()
	if len(data.Prices) != 2 || data.PricesAt != at || len(data.Items) != 2 {
		t.Fatalf("shown answer: %+v", data)
	}

	// A cached answer is shown again with the newest prices, without another run.
	put(55)
	e.request(req)
	until(func(d bagsData) bool { return herbPrice(d) == 55 })
	if n := len(runs()); n != 1 {
		t.Fatalf("prices follow the snapshot without another run: %d runs", n)
	}

	// Without a market the job runs as before.
	req.Market, req.Key = nil, "WARLOCK:10:2-2"
	e.request(req)
	until(func(bagsData) bool { return true })
	if p := runs(); len(p) != 2 || !strings.Contains(p[1], "no prices available") {
		t.Fatal("without auction data the prompt says so")
	}

	// Without a stored client the card hears why there are no prices.
	forgetBattleNetClient()
	req.Market, req.Key = &marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}, "WARLOCK:10:2-3"
	e.request(req)
	if data := until(func(d bagsData) bool { return d.PricesOff != "" }); data.PricesOff != "setup" || len(data.Prices) != 0 {
		t.Fatalf("the card is told prices are not set up: %+v", data)
	}
}

func TestAuctionWatcherFollowsTheStoredClient(t *testing.T) {
	fakeBattleNet(t)

	dir := filepath.Join(t.TempDir(), "auctions")
	now := time.Now()
	w := testWatcher(dir, now)
	id, secret := "client", "secret"
	w.bn.client = func() (string, string, bool) { return id, secret, id != "" }
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if err := recordMarket(dir, m, now); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w.refresh(ctx)
	status := loadAuctionStatus(dir)
	if _, err := os.Stat(filepath.Join(dir, "eu-114.json")); err != nil || status.Refused != "" || len(status.Failing) != 0 || !strings.Contains(status.Items, "world dataset") {
		t.Fatalf("the house is downloaded and the failing item table noted: %+v %v", status, err)
	}

	// Another client in the menu does not go on with the old one's token, and a refusal is noted for the tools.
	id, secret = "other", "wrong"
	w.refresh(ctx)
	if status := loadAuctionStatus(dir); !strings.Contains(status.Refused, "401") {
		t.Fatalf("a refused client is noted: %+v", status)
	}

	// Without a client the token is dropped at once and nothing is noted any more.
	id, secret = "client", "secret"
	w.refresh(ctx)
	id = ""
	w.refresh(ctx)
	if status := loadAuctionStatus(dir); w.bn.token != "" || status.Refused != "" || status.Items != "" {
		t.Fatalf("turned off: %q %+v", w.bn.token, status)
	}
}

// fakeMirror serves the test house the way the mirror does: gzip, Blizzard's Last-Modified, 304, no token.
func fakeMirror(t *testing.T) (requests *[]string) {
	t.Helper()
	var seen []string
	modified := "Fri, 20 Nov 2026 17:23:20 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+" "+r.Header.Get("If-Modified-Since"))
		body := map[string]string{"/v1/auctions/eu/114": testHouse, "/v1/auctions/eu/114/commodities": testCommodities}[r.URL.Path]
		if body == "" || r.Header.Get("Authorization") != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Last-Modified", modified)
		if r.Header.Get("If-Modified-Since") == modified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		z := gzip.NewWriter(w)
		z.Write([]byte(body))
		z.Close()
	}))
	t.Cleanup(srv.Close)
	old := auctionMirrorURL
	auctionMirrorURL = func() string { return srv.URL }
	t.Cleanup(func() { auctionMirrorURL = old })
	return &seen
}

func TestAuctionMirrorStandsInForAMissingClient(t *testing.T) {
	requests := fakeMirror(t)

	snapshots := filepath.Join(t.TempDir(), "snapshots")
	dir := auctionDir(snapshots)
	now := time.Now()
	w := testWatcher(dir, now)
	w.bn.client = func() (string, string, bool) { return "", "", false }
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	if p := auctionProblem(snapshots, m, nil); p == nil || p.Code != "pending" {
		t.Fatal("with the mirror on, a missing client is no setup problem:", p)
	}
	if err := recordMarket(dir, m, now); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w.refresh(ctx)
	book, err := loadAuctionBook(filepath.Join(dir, "eu-114.json"))
	if err != nil || book.Items[2589] == nil || book.Items[7469] == nil || book.Modified["commodities"] != "Fri, 20 Nov 2026 17:23:20 GMT" {
		t.Fatalf("the house comes through the mirror: %+v %v", book, err)
	}
	if p := auctionProblem(snapshots, m, nil); p != nil {
		t.Fatal("prices are there:", p)
	}
	*requests = nil
	w.refresh(ctx)
	if len(*requests) != 2 || (*requests)[0] != "/v1/auctions/eu/114 Fri, 20 Nov 2026 17:23:20 GMT" {
		t.Fatal("an unchanged house is only asked about:", *requests)
	}
	if status := loadAuctionStatus(dir); len(status.Failing) != 0 || status.Refused != "" {
		t.Fatalf("nothing failed: %+v", status)
	}

	// A path the mirror does not serve is refused before any request.
	if _, err := (&auctionMirror{base: "https://x", http: http.DefaultClient}).get(ctx, "eu", "/data/wow/token/index", ""); err == nil {
		t.Fatal("only auction house paths go to the mirror")
	}
}

func TestAuctionMirrorAddressComesFromSettings(t *testing.T) {
	for in, want := range map[string]string{
		"":                       defaultAuctionMirror,
		"off":                    "",
		"https://example.org/":   "https://example.org",
		"http://example.org":     "",
		" https://example.org  ": "https://example.org",
	} {
		if got := mirrorAddress(settings{AuctionMirror: in}); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestBattleNetClientIsCheckedAndStored(t *testing.T) {
	fakeBattleNet(t)
	ctx := context.Background()
	if err := checkBattleNetClient(ctx, "client", "wrong"); !errors.Is(err, errBattleNetRefused) {
		t.Fatal("a wrong secret is refused:", err)
	}
	if err := checkBattleNetClient(ctx, "client", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := battleNetClient(); ok {
		t.Fatal("nothing is stored before the menu saves it")
	}
	useBattleNetClient(t)
	if id, secret, ok := battleNetClient(); !ok || id != "client" || secret != "secret" {
		t.Fatal("the stored client is read back:", id, ok)
	}
	if err := forgetBattleNetClient(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := battleNetClient(); ok {
		t.Fatal("a removed client is gone")
	}
	if err := forgetBattleNetClient(); err != nil {
		t.Fatal("removing it again is no error:", err)
	}
}

func TestANullUpgradeAnswerIsRefused(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(detailDir(dir), 0o700)
	id := strings.Repeat("ab", 16)
	env, _ := json.Marshal(detailEnvelope{detailRequest: detailRequest{ID: id, Session: "player", Upgrade: &upgradeQuery{Slot: "chest"}},
		RunID: "app", Expires: time.Now().Add(time.Minute).UnixMilli()})
	os.WriteFile(filepath.Join(detailDir(dir), id+".req"), env, 0o600)
	p := packet{Type: "details", RequestID: id, Session: "player", RunID: "app", Upgrades: json.RawMessage("null")}
	if err := acceptGameDetails(dir, "app", p, time.Now()); err == nil {
		t.Fatal("a null answer is an error, not a crash")
	}
}

func TestAuctionUpgradesAskTheGameAboutTheSlotsCandidates(t *testing.T) {
	useBattleNetClient(t)
	root := t.TempDir()
	dir := filepath.Join(root, "snapshots")
	auctions := auctionDir(dir)
	os.MkdirAll(auctions, 0o700)
	m, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	book := auctionBook{Market: m, Fetched: time.Now().UnixMilli(), Modified: map[string]string{"auctions": "Fri, 20 Nov 2026 17:23:20 GMT"}, Items: map[int]*auctionItem{
		14158: {Unit: 1399, Variants: []*auctionVariant{{Bonus: []int{12719}, Unit: 1399}, {Bonus: []int{12718}, Tuning: 5246, Unit: 8000},
			{Bonus: []int{12718}, Unit: 7999}, {Bonus: []int{12720}, Unit: 50000}}},
		2589: {Unit: 24, Variants: []*auctionVariant{{Unit: 24}}},
		6096: {Unit: 100, Variants: []*auctionVariant{{Unit: 100}}},
	}}
	data, _ := json.Marshal(book)
	os.WriteFile(filepath.Join(auctions, "eu-114.json"), data, 0o600)
	items, _ := json.Marshal(itemTable{Build: "1.60.1.71037", Items: map[int][3]int{14158: {4, 1, 20}, 6096: {4, 0, 4}}})
	os.WriteFile(filepath.Join(auctions, "items.json"), items, 0o600)

	prompt, err := gamePrompt(dir, packet{Type: "reply", Session: "player", RunID: "app", At: float64(time.Now().Unix()), Text: "Что купить на грудь?",
		ContextText: "Game: WoW Forever 1.60.1.71037 · EU · PvE\nCharacter: level 16 Orc Warlock, Horde · 1g 0s 0c\nDetail lookup: supported"})
	if err != nil {
		t.Fatal(err)
	}
	id := snapshotEnvelope.FindStringSubmatch(prompt)[1]
	tray := newSession("", nil, func() {})
	tray.runID = "app"
	tray.pulseDetailBridge(dir, time.Now())
	type result struct {
		out mcpObject
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := queryGameSnapshot(dir, "find_auction_upgrades", json.RawMessage(`{"snapshot_id":"`+id+`","slot":"chest","budget":10000,"levels_ahead":5}`))
		done <- result{out, err}
	}()
	var req *detailRequest
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until) && req == nil; time.Sleep(10 * time.Millisecond) {
		tray.syncDetailRequest(dir, time.Now())
		req = tray.payload().DetailRequest
	}
	if req == nil || req.Upgrade == nil || req.Upgrade.Slot != "chest" || req.Upgrade.Ahead != 5 || len(req.Refs) != 0 {
		t.Fatalf("upgrade request: %+v", req)
	}
	if got, _ := json.Marshal(req.Upgrade.Items); string(got) != `[{"i":14158,"v":[[1399,12719],[7999,12718]]}]` {
		t.Fatalf("only the slot's variants within budget, merged across level modifiers: %s", got)
	}
	var p packet
	json.Unmarshal([]byte(`{"type":"details","requestId":"`+req.ID+`","session":"player","runId":"app","upgrades":{"items":[{"link":"item:14158::::::::::::1:12719","price":1399,"ilvl":26,"stats":{"SPIRIT":6}}],"worn":[],"level":16,"usable":2}}`), &p)
	if err := acceptGameDetails(dir, "app", p, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.out["market"] != "EU Normal Horde (house 114)" || r.out["sent_to_game"] != 1 || r.out["read_at"] == nil || len(r.out["items"].([]any)) != 1 {
		t.Fatalf("the game's verdict comes back with the market: %+v %v", r.out, r.err)
	}
}
