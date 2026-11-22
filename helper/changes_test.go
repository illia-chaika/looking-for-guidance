package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Made-up notes in the shape of Blizzard's hotfix article after parseHotfixes.
const testNotes = `Classes
- Druid
  - Fixed a druid thing.
- Priest
  - [With scheduled weekly maintenance] Vampiric Embrace healing is now based on 10% of the Shadow damage done by the casting Priest (was 20%).
  - Developers’ notes: This change results in no difference for Priest solo play.
Dungeons
- Killing enemies in dungeons now awards zero experience if the enemy is trivial to one of the party members.
- The Shaper’s Terrace
  - Vitrix, Snowtalon, and Cinder now correctly drop higher-level skinning materials.
- The Drowned City
  - Bloated sharks now drop thicker leather for skinners.
Enemies and NPCs
- Thousand Needles - Fixed an issue where a Pesterhide Snarler could spawn inside a rock.
Items
- Bronze Mace is now Bind on Equip (was unbound).
- Darkspear signet rings have had their Mp5 corrected.
- Arcane Salvager now requires the Runed Golden Rod tool to craft.
- Nifty Stopwatch no longer shares a cooldown with Dash, Blink, Sprint, and other movement abilities.
- Lowered the damage of the Deathless Sorcerer's Fireball by 25%.
Quests
- There are now more Gravelsnout Diggers and Surveyors available for the quest "A New Ore Sample".
- Zephras Isle - “Camping 101: Herbalism” now completes in Shen'dar Village instead of Valanaar.
- Fixed an issue during the quest "The Talisman of Kazdor" where rogues would enter the wrong version of Zul'Farrak.
Races
- Night Elf
  - Fixed a bug granting Shadowmelded Druids access to Ravage and Pounce.
- Orc
  - Fixes an issue where some Warlock pet abilities were benefitting too much from the Orc Command racial ability.
Professions
- Herbalism
  - Fixed a bug that prevented Cultivation from working correctly on Mageroyal.`

var changesWorld = map[string]map[string]any{
	"items": {
		"2848":  map[string]any{"name": map[string]string{"en_US": "Bronze Mace"}, "foreverdb": map[string]any{"created_by": []any{map[string]any{"profession": "Blacksmithing"}}}},
		"11128": map[string]any{"name": map[string]string{"en_US": "Golden Rod"}, "foreverdb": map[string]any{"created_by": []any{map[string]any{"profession": "Blacksmithing"}}}},
		"11130": map[string]any{"name": map[string]string{"en_US": "Runed Golden Rod"}, "foreverdb": map[string]any{"created_by": []any{map[string]any{"profession": "Enchanting"}}}},
		"12345": map[string]any{"name": map[string]string{"en_US": "Signet Ring"}, "foreverdb": map[string]any{"created_by": []any{map[string]any{"profession": "Blacksmithing"}}}},
		"99":    map[string]any{"name": map[string]string{"en_US": "Unowned Blade"}},
	},
	"quests": {
		"1153":  map[string]any{"name": map[string]string{"en_US": "A New Ore Sample"}, "foreverdb": map[string]any{"forever": map[string]any{"level": 29}}},
		"2936":  map[string]any{"name": map[string]string{"en_US": "The Talisman of Kazdor"}, "foreverdb": map[string]any{"forever": map[string]any{"level": 46}}},
		"90001": map[string]any{"name": map[string]string{"en_US": "Camping 101: Herbalism"}, "foreverdb": map[string]any{"forever": map[string]any{"level": 2}}},
	},
	"spells": {
		"15286": map[string]any{"name": map[string]string{"en_US": "Vampiric Embrace"}, "foreverdb": map[string]any{"class_spell": map[string]any{"class": "PRIEST", "level": 20}}},
		"2983":  map[string]any{"name": map[string]string{"en_US": "Sprint"}, "foreverdb": map[string]any{"class_spell": map[string]any{"class": "ROGUE", "level": 10}}},
		"133":   map[string]any{"name": map[string]string{"en_US": "Fireball"}, "foreverdb": map[string]any{"class_spell": map[string]any{"class": "MAGE", "level": 1}}},
	},
	"places": {
		"zone-1441":   map[string]any{"name": map[string]string{"en_US": "Thousand Needles"}, "foreverdb": map[string]any{"zone": map[string]any{"ui_map": 1441}}},
		"dungeon-209": map[string]any{"name": map[string]string{"en_US": "Zul'Farrak"}, "foreverdb": map[string]any{"dungeon": map[string]any{"levels": []int{44, 54}}}},
		"dungeon-999": map[string]any{"name": map[string]string{"en_US": "The Shaper's Terrace"}, "foreverdb": map[string]any{"dungeon": map[string]any{"levels": []int{58, 60}}}},
	},
}

// changesFixture sets up a world dataset, a cached hotfix list and a changes feed under one settings folder.
func changesFixture(t *testing.T) (snapshots string, world *worldData) {
	t.Helper()
	f := newFakeWorld(t, changesWorld, "")
	snapshots = filepath.Join(t.TempDir(), "snapshots")
	w := &worldWatcher{dir: worldDir(snapshots), http: http.DefaultClient, base: func() string { return f.srv.URL }, now: time.Now}
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	world, err := loadWorld(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	return snapshots, world
}

func testFeed() *gameFeed {
	return &gameFeed{URL: defaultHotfixURL, Fetched: time.Now().Unix(), Hash: "h1", Days: []feedDay{{Date: time.Now().Format("2006-01-02"), Text: testNotes}}}
}

func facts(class, race string, level int, extra map[string]any) changeFacts {
	raw := map[string]any{"class": class, "race": race, "level": level}
	for k, v := range extra {
		raw[k] = v
	}
	body, _ := json.Marshal(raw)
	var f changeFacts
	json.Unmarshal(body, &f)
	return f
}

func tiers(mine, class, general []changeItem) map[string]string {
	out := map[string]string{}
	for _, group := range []struct {
		items []changeItem
		tier  string
	}{{mine, tierMine}, {class, tierClass}, {general, tierGeneral}} {
		for _, it := range group.items {
			key := it.Text
			if it.Kind == "data" {
				key = "data:" + it.Name
			}
			out[key] = group.tier + " " + it.Reason
		}
	}
	return out
}

func find(got map[string]string, part string) string {
	for k, v := range got {
		if strings.Contains(k, part) {
			return v
		}
	}
	return ""
}

func TestParseNotesKeepsHeadingsAndDevelopersNotes(t *testing.T) {
	notes := parseNotes([]feedDay{{Date: "2026-11-16", Text: testNotes}})
	var ve, terrace hotfixNote
	for _, n := range notes {
		switch {
		case strings.Contains(n.Text, "Vampiric Embrace"):
			ve = n
		case strings.Contains(n.Text, "skinning materials"):
			terrace = n
		}
		if n.Text == "Druid" || n.Text == "Priest" || strings.HasPrefix(n.Text, "Developers") {
			t.Fatalf("headings and developers' notes are not notes: %+v", n)
		}
	}
	if ve.Category != "Classes" || strings.Join(ve.Path, "/") != "Priest" || !strings.Contains(ve.Dev, "no difference for Priest solo play") {
		t.Fatalf("the Priest note with its heading and the developers' note below it: %+v", ve)
	}
	if terrace.Category != "Dungeons" || strings.Join(terrace.Path, "/") != "The Shaper’s Terrace" || terrace.Date != "2026-11-16" {
		t.Fatalf("a dungeon heading: %+v", terrace)
	}
}

func TestChangesMatchByIDsAndDropWhatDoesNotConcernTheCharacter(t *testing.T) {
	_, world := changesFixture(t)
	feed := testFeed()

	// An Orc shaman in Thousand Needles with an herbalist's and alchemist's trade and a quest in the log.
	got := tiers(matchChanges(facts("SHAMAN", "Orc", 34, map[string]any{"map": 1441,
		"professions": []any{map[string]any{"name": "Herbalism"}, map[string]any{"name": "Alchemy"}},
		"quests":      []any{map[string]any{"id": 1153, "title": "A New Ore Sample"}}}), feed, nil, world, time.Now()))
	for part, want := range map[string]string{
		"Pesterhide Snarler":            "mine zone",
		"Gravelsnout Diggers":           "mine quest",
		"Cultivation":                   "mine profession",
		"awards zero experience":        "general ",
		"Camping 101":                   "", // a quest's title, not the character's profession
		"Warlock pet abilities":         "", // the Orc heading, but about warlocks
		"Shadowmelded Druids":           "", // another race's heading
		"Fixed a druid thing":           "", // another class's heading
		"Deathless Sorcerer's Fireball": "",
	} {
		if g := find(got, part); g != want {
			t.Errorf("shaman, %q: got %q, want %q", part, g, want)
		}
	}

	// A Tauren druid at level 12 who skins: the dungeon's materials are far above the level.
	got = tiers(matchChanges(facts("DRUID", "Tauren", 12, map[string]any{"professions": []any{map[string]any{"name": "Skinning"}}}), feed, nil, world, time.Now()))
	if find(got, "Fixed a druid thing") != "class class" || find(got, "skinning materials") != "" || find(got, "Shadowmelded") != "" ||
		find(got, "thicker leather") != "" {
		t.Errorf("druid: %v", got)
	}

	// A rogue who knows Sprint and carries a Bronze Mace: the Kazdor quest is far above level 26.
	got = tiers(matchChanges(facts("ROGUE", "Human", 26, map[string]any{"spells": "1752, 2983", "bags": "2848,6948"}), feed, nil, world, time.Now()))
	if find(got, "Nifty Stopwatch") != "mine spell" || find(got, "Bronze Mace") != "mine item" || find(got, "Talisman of Kazdor") != "" {
		t.Errorf("rogue: %v", got)
	}

	// A priest who knows Vampiric Embrace and enchants: the longest name wins (Runed Golden Rod, not Golden Rod).
	got = tiers(matchChanges(facts("PRIEST", "Scourge", 44, map[string]any{"spells": "15286",
		"professions": []any{map[string]any{"name": "Enchanting"}, map[string]any{"name": "Tailoring"}}}), feed, nil, world, time.Now()))
	if find(got, "Vampiric Embrace") != "mine spell" || find(got, "Runed Golden Rod") != "mine profession" {
		t.Errorf("priest: %v", got)
	}

	// A blacksmith: "signet rings" in lower case is not the Signet Ring; the Runed Golden Rod is an enchanter's.
	got = tiers(matchChanges(facts("PALADIN", "Human", 58, map[string]any{"professions": []any{map[string]any{"name": "Blacksmithing"}}}), feed, nil, world, time.Now()))
	if find(got, "Bronze Mace") != "mine profession" || strings.HasPrefix(find(got, "signet rings"), "mine") || find(got, "Runed Golden Rod") != "" {
		t.Errorf("paladin: %v", got)
	}

	// A mage who knows Fireball: a creature's Fireball is not the mage's.
	got = tiers(matchChanges(facts("MAGE", "Gnome", 30, map[string]any{"spells": "133"}), feed, nil, world, time.Now()))
	if find(got, "Deathless Sorcerer's Fireball") != "" {
		t.Errorf("mage: %v", got)
	}
}

func TestChangesMatchDataChangesOfTheCharactersItemsAndSpells(t *testing.T) {
	_, world := changesFixture(t)
	today := time.Now().Format("2006-01-02")
	ch := &changesFile{Built: time.Now().UTC().Format(time.RFC3339), Changes: []dataChange{
		{Date: today, Table: "ItemSparse", Row: 2848, Kind: "items", ID: 2848, Change: "changed",
			Fields: map[string][]json.RawMessage{"Bonding": {json.RawMessage("0"), json.RawMessage("2")}}},
		{Date: today, Table: "SpellEffect", Row: 1, Kind: "spells", ID: 15286, Change: "changed",
			Fields: map[string][]json.RawMessage{"EffectBasePointsF": {json.RawMessage("20"), json.RawMessage("10")}}},
		{Date: today, Table: "ItemSparse", Row: 99, Kind: "items", ID: 99, Change: "changed",
			Fields: map[string][]json.RawMessage{"ItemLevel": {json.RawMessage("61"), json.RawMessage("60")}},
			Before: map[string]json.RawMessage{"item_level": json.RawMessage("61")}, After: map[string]json.RawMessage{"item_level": json.RawMessage("60")}},
		{Date: "2020-01-01", Table: "ItemSparse", Row: 2848, Kind: "items", ID: 2848, Change: "changed",
			Fields: map[string][]json.RawMessage{"SellPrice": {json.RawMessage("1"), json.RawMessage("2")}}},
	}}
	mine, _, _ := matchChanges(facts("PRIEST", "Scourge", 44, map[string]any{"spells": "15286", "gear": "2848"}), nil, ch, world, time.Now())
	if len(mine) != 2 {
		t.Fatalf("the worn mace and the known spell, not the unowned blade or an old change: %+v", mine)
	}
	for _, it := range mine {
		switch it.Name {
		case "Bronze Mace":
			if it.Reason != "item" || len(it.Fields) != 1 || it.Fields[0] != [3]string{"binding", "0", "2"} {
				t.Errorf("mace: %+v", it)
			}
		case "Vampiric Embrace":
			if it.Reason != "spell" || it.Fields[0] != [3]string{"value", "20", "10"} {
				t.Errorf("spell: %+v", it)
			}
		default:
			t.Errorf("unexpected %+v", it)
		}
	}
	if f := dataFields(ch.Changes[2]); len(f) != 1 || f[0] != [3]string{"item_level", "61", "60"} {
		t.Errorf("Battle.net's readable values come first: %v", f)
	}
	added := dataChange{Change: "added", Fields: map[string][]json.RawMessage{"ItemLevel": {json.RawMessage("null"), json.RawMessage("60")}}}
	if f := dataFields(added); len(f) != 1 || f[0] != [3]string{"item_level", "", "60"} {
		t.Errorf("an added row has no old value: %v", f)
	}
	// Battle.net's values the way the backend publishes them: tooltip words become the client's numbers.
	weapon := dataChange{Kind: "items", ID: 424242, Change: "changed",
		Before: map[string]json.RawMessage{"quality": json.RawMessage(`"Epic"`), "binding": json.RawMessage(`"Binds when equipped"`),
			"stats": json.RawMessage(`[{"type":"Agility","value":5}]`), "weapon_damage": json.RawMessage(`[40,100]`), "weapon_speed": json.RawMessage(`1900`)},
		After: map[string]json.RawMessage{"name": json.RawMessage(`"Brand New Blade"`), "quality": json.RawMessage(`"Legendary"`),
			"binding": json.RawMessage(`"Binds when picked up"`), "stats": json.RawMessage(`[{"type":"Agility","value":7},{"type":"Stamina","value":4}]`),
			"weapon_damage": json.RawMessage(`[44,115]`), "weapon_speed": json.RawMessage(`1900`)}}
	want := [][3]string{{"binding", "2", "1"}, {"quality", "4", "5"}, {"stats", "Agility +5", "Agility +7, Stamina +4"}, {"weapon_damage", "40–100", "44–115"}}
	if f := dataFields(weapon); len(f) != len(want) || f[0] != want[0] || f[1] != want[1] || f[2] != want[2] || f[3] != want[3] {
		t.Errorf("Battle.net values as a tooltip reads them: %v", f)
	}
	m := newChangeMatcher(changeFacts{Bags: "424242"}, world)
	if it := m.data(weapon); it.Name != "Brand New Blade" || it.tier != tierMine {
		t.Errorf("a new item is named by Battle.net: %+v", it)
	}
}

func TestChangesCardAnswersWithoutTheAdviser(t *testing.T) {
	snapshots, _ := changesFixture(t)
	dir := filepath.Join(filepath.Dir(snapshots), "insights")
	os.MkdirAll(dir, 0o700)
	body, _ := json.Marshal(testFeed())
	os.WriteFile(filepath.Join(dir, "feed.json"), body, 0o600)
	e := newInsightEngine(dir, func() {}, func() (string, string) { return "codex", "/nonexistent" })
	e.fetch = func(context.Context, string) (string, error) { return "", errors.New("no network in tests") }
	var ran atomic.Bool
	e.run = func(context.Context, *insightJob, string, string, string) error {
		ran.Store(true)
		return errors.New("no runs")
	}
	factsJSON := `{"class":"SHAMAN","race":"Orc","level":34,"map":1441,"quests":[{"id":1153,"title":"A New Ore Sample"}]}`
	e.request(insightRequest{Kind: "changes", Key: "me", Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A", Facts: json.RawMessage(factsJSON)})
	var got insight
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(10 * time.Millisecond) {
		if v := e.view(); len(v) == 1 && v[0].State != "pending" {
			got = v[0]
			break
		}
	}
	if got.State != "ready" || ran.Load() {
		t.Fatalf("answered at once, with no adviser run: %+v", got)
	}
	var a changesAnswer
	json.Unmarshal(got.Data, &a)
	if len(a.Mine) != 2 || a.Key == "" || a.General != 2 || a.NoWorld || a.NoNotes || a.Newest == "" {
		t.Fatalf("%+v", a)
	}
	if _, err := os.Stat(filepath.Join(dir, "usage.json")); err == nil {
		t.Fatal("a local answer spends nothing of the daily cap")
	}
	if _, err := os.Stat(changeFactsPath(dir, "Player-1-0A")); err != nil {
		t.Fatal("the facts are kept for get_changes:", err)
	}

	// The adviser's tool matches the same character from those facts.
	id, err := saveGameSnapshot(snapshots, gameSnapshot{Character: "Player-1-0A", Captured: "now", Sections: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := queryChanges(snapshots, json.RawMessage(`{"snapshot_id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if notes := out["notes"].([]mcpObject); len(notes) != 2 || out["general_notes"] != 2 {
		t.Fatalf("%v", out)
	}
	out, err = queryChanges(snapshots, json.RawMessage(`{"snapshot_id":"`+id+`","mine":false,"about":[{"kind":"items","id":"2848"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if notes := out["notes"].([]mcpObject); len(notes) != 1 || !strings.Contains(notes[0]["text"].(string), "Bronze Mace") {
		t.Fatalf("asked about a record: %v", out)
	}
}

func TestSummaryReadsTheShortlistOnly(t *testing.T) {
	snapshots, world := changesFixture(t)
	env := insightEnv{Feed: testFeed(), World: world}
	req := insightRequest{Kind: "digest", Locale: "ruRU", Facts: json.RawMessage(`{"class":"SHAMAN","race":"Orc","level":34,"map":1441,"spells":"1,2,3","quests":[{"id":1153,"title":"A New Ore Sample"}]}`)}
	prompt, err := digestKind.prompt(req, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pesterhide Snarler", "A New Ore Sample", "awards zero experience", "matched: zone Thousand Needles"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	for _, unwanted := range []string{"Fixed a druid thing", "Shadowmelded", "Kazdor", `"spells"`} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("the prompt should not hold %q", unwanted)
		}
	}
	_ = snapshots
}

func TestChangesFeedDownloadAndPageRefetchRule(t *testing.T) {
	var served, notModified atomic.Int32
	body := `{"built":"` + time.Now().UTC().Format(time.RFC3339) + `","hotfixes":{"url":"u","newest":"2026-11-19","changed_at":"2026-11-20T10:00:00Z"},"issues":{"changed_at":"2026-11-20T09:00:00Z"},"changes":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/world/changes.json" {
			http.NotFound(w, r)
			return
		}
		served.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	w := &worldWatcher{dir: worldDir(snapshots), http: http.DefaultClient, base: func() string { return srv.URL }, now: time.Now}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	hot := func(f *changesFile) string { return f.Hotfixes.ChangedAt }
	if !feedStale(now.Add(-4*time.Hour).Unix(), now, w.dir, hot) || feedStale(now.Add(-2*time.Hour).Unix(), now, w.dir, hot) {
		t.Fatal("without the feed: every 3 hours")
	}
	if err := w.refreshChanges(context.Background()); err != nil {
		t.Fatal(err)
	}
	stamp := changesStamp(w.dir)
	if err := w.refreshChanges(context.Background()); err != nil || !changesStamp(w.dir).Equal(stamp) || served.Load() != 2 || notModified.Load() != 1 {
		t.Fatal("an unchanged feed is asked with its ETag and not downloaded again", err, served.Load(), notModified.Load())
	}
	changed := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if !feedStale(changed.Add(-time.Minute).Unix(), now, w.dir, hot) {
		t.Fatal("a copy older than the page's last change is fetched again")
	}
	if feedStale(changed.Add(time.Minute).Unix(), now.Add(20*time.Hour), w.dir, hot) {
		t.Fatal("a copy newer than the last change is kept, even past 3 hours")
	}
	if !feedStale(changed.Add(time.Minute).Unix(), now.Add(25*time.Hour), w.dir, hot) {
		t.Fatal("and fetched once a day in case the feed stopped")
	}
	unknown := strings.Replace(body, `"changed_at":"2026-11-20T10:00:00Z"`, `"changed_at":null`, 1)
	os.WriteFile(changesPath(w.dir), []byte(unknown), 0o600)
	if !feedStale(changed.Add(time.Minute).Unix(), changed.Add(4*time.Hour), w.dir, hot) {
		t.Fatal("when the backend could not read the page, every 3 hours as without the feed")
	}
	old := strings.Replace(body, time.Now().UTC().Format(time.RFC3339)[:4], "2020", 1)
	os.WriteFile(changesPath(w.dir), []byte(old), 0o600)
	if f, _ := readChanges(w.dir); f != nil {
		t.Fatal("a feed older than 30 days is not used")
	}
}
