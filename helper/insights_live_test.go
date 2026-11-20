package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live check against the real Codex CLI and the real hotfix page. It spends the player's Codex allowance, so it
// runs only on request:
//
//	FOREVER_LIVE=digest go test -run TestLiveInsight -v ./helper
//
// FOREVER_LIVE names the kind; FOREVER_FACTS and FOREVER_KEY override the sample character. FOREVER_MARKET
// ("EU PvE Horde") first downloads that auction house with the keychain's Battle.net client, for the bags job.
func TestLiveInsight(t *testing.T) {
	kind := os.Getenv("FOREVER_LIVE")
	if kind == "" {
		t.Skip("set FOREVER_LIVE to a job kind to run it against the real Codex")
	}
	if err := useGameProfile(); err != nil {
		t.Fatal(err)
	}
	facts := os.Getenv("FOREVER_FACTS")
	if facts == "" {
		facts = liveFacts[kind]
	}
	key := os.Getenv("FOREVER_KEY")
	if key == "" {
		key = "live"
	}
	dir := t.TempDir()
	var market *marketFacts
	if fields := strings.Fields(os.Getenv("FOREVER_MARKET")); len(fields) == 3 {
		market = &marketFacts{Region: fields[0], Ruleset: fields[1], Faction: fields[2]}
		m, err := market.market()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newAuctionWatcher(filepath.Join(filepath.Dir(dir), "auctions")).update(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	e := newInsightEngine(dir, func() {}, func() (string, string) {
		// FOREVER_LIVE_AGENT=claude runs the same job through Claude Code.
		if os.Getenv("FOREVER_LIVE_AGENT") == "claude" {
			return "claude", findInstalled("claude")
		}
		return "codex", findInstalled("codex")
	})
	e.notes = func(string) []string {
		return []string{"Не продавай Linen Cloth: коплю на бинты."}
	}
	ready := true
	e.pump(&ready)
	started := time.Now()
	e.request(insightRequest{Kind: kind, Key: key, Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A", Facts: json.RawMessage(facts), Market: market})
	for time.Since(started) < insightTimeout+time.Minute {
		for _, in := range e.view() {
			if in.State == "pending" {
				continue
			}
			var pretty any
			json.Unmarshal(in.Data, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			t.Logf("%s %s in %s, %d bytes\nnote: %s\n%s", in.Kind, in.State, time.Since(started).Round(time.Second), len(in.Data), in.Note, out)
			if in.State != "ready" {
				t.Fail()
			}
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("no answer")
}

// A level 10 Undead Warlock like the one this build was made for.
var liveFacts = map[string]string{
	"leveling": `{"faction":"Horde","class":"WARLOCK","race":"Scourge","level":10,"from":10,"to":14,"zone":"Tirisfal Glades"}`,
	"bags":     `{"class":"WARLOCK","level":10,"professions":[{"name":"Alchemy","rank":21,"max":75},{"name":"Herbalism","rank":50,"max":75},{"name":"Cooking","rank":1,"max":75},{"name":"First Aid","rank":20,"max":75}],"items":[{"id":2589,"name":"Linen Cloth","q":1,"type":"Trade Goods","sub":"Cloth","sell":13,"n":26,"tip":"Crafting Reagent"},{"id":3834,"name":"Sturdy Cloth Trousers","q":1,"type":"Armor","sub":"Cloth","req":9,"sell":120,"n":1,"slot":"INVTYPE_LEGS","tip":"Legs; Cloth; 13 Armor; Requires Level 9","worn":"Patchwing Pants: Legs; Cloth; 18 Armor; +2 Intellect"},{"id":247755,"name":"Elixir of Minor Force","q":1,"type":"Consumable","sub":"Elixir","sell":5,"n":7,"tip":"Use: Increases attack power by 8 for 30 min."},{"id":765,"name":"Silverleaf","q":1,"type":"Trade Goods","sub":"Herb","sell":10,"n":34,"tip":"Crafting Reagent"},{"id":12223,"name":"Meaty Bat Wing","q":1,"type":"Trade Goods","sub":"Meat","sell":3,"n":9,"tip":"Crafting Reagent"},{"id":2678,"name":"Mild Spices","q":1,"type":"Trade Goods","sub":"Other","sell":1,"n":1,"tip":"Crafting Reagent"},{"id":4604,"name":"Forest Mushroom Cap","q":1,"type":"Consumable","sub":"Food & Drink","sell":1,"n":1,"tip":"Use: Restores 58 health over 18 sec. Must remain seated while eating."},{"id":247754,"name":"Minor Arcane Elixir","q":1,"type":"Consumable","sub":"Elixir","sell":5,"n":2,"tip":"Use: Increases spell damage by up to 5 for 30 min."},{"id":4562,"name":"Severing Axe of the Monkey","q":2,"type":"Weapon","sub":"Two-Handed Axes","req":9,"sell":310,"n":1,"slot":"INVTYPE_2HWEAPON","tip":"Two-Hand; Axe; 19 - 29 Damage; +2 Agility; +2 Stamina","worn":"Apprentice's Staff: Two-Hand; Staff; 9 - 14 Damage"}]}`,
	"dungeon":  `{"name":"Ragefire Chasm","faction":"Horde","class":"WARLOCK","plevel":13}`,
	"brief":    `{"class":"WARLOCK","race":"Scourge","faction":"Horde","level":10,"from":10,"to":19,"professions":[{"name":"Alchemy","rank":21,"max":75},{"name":"Herbalism","rank":50,"max":75}]}`,
	"quest":    `{"id":1234,"title":"The Great Outdoors","level":8,"category":"Tirisfal Glades","objectives":[{"text":"Sit by a campfire: 0/1","done":0,"need":1}],"goal":"Build a campfire and sit by it.","zone":"Tirisfal Glades","class":"WARLOCK","race":"Scourge","faction":"Horde","plevel":10}`,
	"digest":   `{"class":"WARLOCK","race":"Scourge","faction":"Horde","level":10,"zone":"Tirisfal Glades","professions":[{"name":"Alchemy","rank":21,"max":75},{"name":"Herbalism","rank":50,"max":75},{"name":"Cooking","rank":1,"max":75},{"name":"First Aid","rank":20,"max":75}],"quests":[{"id":408,"title":"The Family Crypt","level":11},{"id":405,"title":"The Prodigal Lich","level":8},{"id":1478,"title":"Halgar's Summons","level":10}]}`,
}
