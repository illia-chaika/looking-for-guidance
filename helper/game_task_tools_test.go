package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func taskCall(t *testing.T, dir, id, name string, args mcpObject) (mcpObject, error) {
	t.Helper()
	if args == nil {
		args = mcpObject{}
	}
	args["snapshot_id"] = id
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return queryGameSnapshot(dir, name, data)
}

// Exercise the wire shape that Codex discovers; existing sessions can still replay old calls.
func TestTaskToolDiscoveryAndLegacyDispatch(t *testing.T) {
	dir := t.TempDir()
	id := newCatalogQuestion(t, dir)
	in := fmt.Sprintf(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-11-25"}}
{"jsonrpc":"2.0","method":"tools/list","id":2}
{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"get_equipment","arguments":{"snapshot_id":%q,"slot":17}}}
{"jsonrpc":"2.0","method":"tools/call","id":4,"params":{"name":"get_entries","arguments":{"snapshot_id":%q,"section":"gear","slot":17}}}
`, id, id)
	var out bytes.Buffer
	if err := runGameMCP(strings.NewReader(in), &out, dir); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatal(out.String())
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name, Description string
				InputSchema       struct {
					Properties map[string]struct {
						Description string
						Maximum     int
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Result.Tools {
		names = append(names, tool.Name)
		props := tool.InputSchema.Properties
		if _, ok := props["snapshot_id"]; ok == (tool.Name == "search_game_data" || tool.Name == "get_game_data") {
			t.Fatalf("snapshot_id belongs to the snapshot tools only: %s", tool.Name)
		}
		if limit, ok := props["limit"]; ok {
			if limit.Maximum != 30 || !strings.Contains(limit.Description, "1–30") || !strings.Contains(tool.Description, "next_offset") {
				t.Fatal(tool)
			}
			if _, ok := props["section"]; ok {
				t.Fatalf("new API exposes irrelevant section: %s", tool.Name)
			}
		}
	}
	want := []string{"get_context", "get_quests", "get_details", "get_talents", "get_equipment", "search_spellbook", "search_bags", "get_skills", "get_recipes", "get_buffs", "get_reputation", "get_attached_links", "get_auction_prices", "find_auction_upgrades", "search_game_data", "get_changes", "get_game_data", "get_memory", "upsert_memory", "forget_memory"}
	want = append(want, "get_installed_addons", "get_client_api_source")
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatal(names)
	}
	var results [2]struct {
		Result struct {
			Content []struct{ Text string }
			IsError bool
		}
	}
	for i := range results {
		if err := json.Unmarshal([]byte(lines[i+2]), &results[i]); err != nil {
			t.Fatal(err)
		}
		res := results[i].Result
		if res.IsError || len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, "item:100:22") {
			t.Fatal(res)
		}
	}
	if results[0].Result.Content[0].Text != results[1].Result.Content[0].Text {
		t.Fatal("legacy callers returned a different item")
	}
}

func TestTalentChoicesUseCapturedRanksAndCurrency(t *testing.T) {
	dir := t.TempDir()
	s := gameSnapshot{Lookup: true, Sections: map[string]string{"talents": "tree 91 currency 17: 1 unspent, 3 spent, Active (Rank 2)"}}
	for i := 1; i <= 40; i++ {
		row := gameEntry{"section": "talents", "kind": "spell", "id": i, "name": fmt.Sprintf("Talent %d", i), "rank": 0, "purchasedRank": 0, "maxRank": 3, "available": true, "canPurchase": i <= 18}
		s.Entries = append(s.Entries, row)
	}
	s.Entries[0]["rank"], s.Entries[0]["purchasedRank"] = 2, 2
	s.Entries[1]["rank"] = 1 // granted rank, zero spent
	s.Entries[2]["rank"] = 1 // older capture with no separate purchasedRank, retain and label
	delete(s.Entries[2], "purchasedRank")
	s.Entries[3]["available"] = false
	s.Entries[4]["rank"] = 3 // capped entry on a choice node
	delete(s.Entries[5], "available")
	s.Entries[6]["canAfford"] = false
	id, err := saveGameSnapshot(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	c, err := taskCall(t, dir, id, "get_talents", nil)
	if err != nil {
		t.Fatal(err)
	}
	curr := c["points"].([]map[string]int)
	if len(curr) != 1 || curr[0]["currency_id"] != 17 || curr[0]["unspent"] != 1 || curr[0]["spent"] != 3 {
		t.Fatal(curr)
	}
	inv := c["invested"].(mcpObject)
	if inv["total"] != 2 || c["rank_note"] == nil {
		t.Fatal(c)
	}
	p := c["purchasable"].(mcpObject)
	if p["total"] != 14 || len(p["entries"].([]gameEntry)) != 12 || p["next_offset"] != 12 {
		t.Fatal(p)
	}
	next, err := taskCall(t, dir, id, "get_talents", mcpObject{"view": "purchasable", "offset": p["next_offset"]})
	if err != nil || next["total"] != 14 || len(next["entries"].([]gameEntry)) != 2 || next["next_offset"] != nil {
		t.Fatal(next, err)
	}
	found, err := taskCall(t, dir, id, "get_talents", mcpObject{"id": 35})
	if err != nil || found["view"] != "all" || found["total"] != 1 {
		t.Fatal(found, err)
	}
	for _, n := range []int{50, 60} {
		_, err := taskCall(t, dir, id, "get_talents", mcpObject{"limit": n})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("limit %d", n)) || !strings.Contains(err.Error(), "1–30") || !strings.Contains(err.Error(), "next_offset") {
			t.Fatal(err)
		}
	}
}

func TestTaskSpecificFiltersAndMissingData(t *testing.T) {
	dir := t.TempDir()
	s := gameSnapshot{Sections: map[string]string{"talents": "talent tree unavailable", "spells": "spellbook", "references": "1"}, Entries: []gameEntry{
		{"section": "spells", "kind": "spell", "id": 1, "name": "Bolt", "known": true},
		{"section": "spells", "kind": "spell", "id": 2, "name": "Bolt", "known": true, "pet": true},
		{"section": "spells", "kind": "spell", "id": 3, "name": "Bolt", "known": false},
		{"section": "references", "kind": "quest", "id": 90, "name": "Journal"},
	}}
	id, err := saveGameSnapshot(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	result, err := taskCall(t, dir, id, "get_talents", nil)
	if err != nil || result["entries_available"] != false || result["points"] != nil || result["invested"] != nil || result["legacy_summary"] != "talent tree unavailable" {
		t.Fatal(result, err)
	}
	for _, c := range []struct {
		name string
		args mcpObject
		want int
	}{
		{"search_spellbook", mcpObject{"query": "OLT", "known": true, "pet": false}, 1},
		{"search_spellbook", mcpObject{"known": false}, 3},
		{"search_spellbook", mcpObject{"pet": true}, 2},
		{"get_attached_links", mcpObject{"kind": "quest"}, 90},
	} {
		page, err := taskCall(t, dir, id, c.name, c.args)
		if err != nil || page["total"] != 1 || page["entries"].([]gameEntry)[0].id() != c.want {
			t.Fatal(page, err)
		}
	}
	for _, c := range []struct {
		name string
		args mcpObject
		want string
	}{
		{"get_equipment", mcpObject{"section": "spells"}, "does not accept"},
		{"search_bags", mcpObject{"known": true}, "does not accept"},
		{"search_spellbook", mcpObject{"known": "false"}, "known"},
		{"get_skills", nil, "unavailable"},
		{"get_attached_links", mcpObject{"kind": "guild|bad"}, "kind"},
		{"get_talents", mcpObject{"view": "summary", "offset": 3}, "summary"},
	} {
		_, err := taskCall(t, dir, id, c.name, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func TestAttachedGameLinksPreserveOpaqueIDs(t *testing.T) {
	dir := t.TempDir()
	link := "trade:Player-123-FFFF:2259:AA=="
	prompt, err := gamePrompt(dir, packet{Text: "Check this", ContextText: "References: 1\nCard: " + string(marshalPlain(gameEntry{"section": "references", "kind": "trade", "name": "Alchemy", "link": link}))})
	if err != nil {
		t.Fatal(err)
	}
	id := snapshotEnvelope.FindStringSubmatch(prompt)[1]
	result, err := taskCall(t, dir, id, "get_attached_links", mcpObject{"kind": "trade"})
	if err != nil {
		t.Fatal(err)
	}
	entries := result["entries"].([]gameEntry)
	if len(entries) != 1 || entries[0].str("link") != link || entries[0].id() != 0 {
		t.Fatal(result)
	}
}
