package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestRecipesDistinguishLearnedFromCraftableAndMissing(t *testing.T) {
	dir := t.TempDir()
	raw := `Skills: Cooking 1/75
Card: {"section":"skills","kind":"skill","id":185,"name":"Cooking","rank":1,"maxRank":75}
Card: {"section":"skills","kind":"recipe_book","id":185,"name":"Cooking","rank":1,"maxRank":75,"observedAt":1770000000,"freshness":"open","coverage":"filtered","recipes":[[7001,"Charred Wolf Meat",1],[7002,"An unlearned recipe",0]],"selected":{"recipeID":7001,"learned":true,"name":"Charred Wolf Meat","requirements":[{"name":"Cooking Fire","met":false}],"outputItemID":8001}}
`
	state := parseGameSnapshot(raw, 1770000001)
	query := func(state gameSnapshot, tool, extra string) mcpObject {
		t.Helper()
		id, err := saveGameSnapshot(dir, state)
		if err != nil {
			t.Fatal(err)
		}
		body := json.RawMessage(fmt.Sprintf(`{"snapshot_id":%q%s}`, id, extra))
		result, err := queryGameSnapshot(dir, tool, body)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	out := query(state, "get_recipes", `,"query":"WOLF","profession":"Cooking","known":true`)
	row := out["entries"].([]mcpObject)[0]
	if out["status"] != "partial" || row["recipe_id"] != 7001 || row["learned"] != true || row["outputItemID"] != float64(8001) || row["requirements"].([]any)[0].(map[string]any)["met"] != false {
		t.Fatal(out)
	}
	out = query(state, "get_recipes", `,"id":8001`)
	if out["total"] != 0 || out["match"] != "unknown" {
		t.Fatal("product matched recipe", out)
	}
	out = query(state, "get_recipes", `,"query":"Crispy Bat Wing"`)
	if out["match"] != "unknown" {
		t.Fatal("concluded missing was unlearned", out)
	}
	out = query(state, "get_recipes", `,"known":false`)
	if out["total"] != 1 || out["entries"].([]mcpObject)[0]["learned"] != false {
		t.Fatal(out)
	}
	out = query(state, "get_skills", "")
	if out["total"] != 1 || out["entries"].([]gameEntry)[0].str("kind") != "skill" {
		t.Fatal("recipe book leaked into skill results", out)
	}
	for _, freshness := range []string{"last_seen", "stale"} {
		state.Entries[1]["freshness"] = freshness
		out = query(state, "get_recipes", `,"id":7001`)
		row = out["entries"].([]mcpObject)[0]
		if row["learned"] != nil || row["learned_at_observation"] != true || row["requirements"] != nil || row["selected"] != nil {
			t.Fatal("historical facts presented as current", out)
		}
	}
	out = query(parseGameSnapshot("Skills: Cooking 1/75", 1770000002), "get_recipes", "")
	if out["status"] != "unavailable" || out["total"] != 0 {
		t.Fatal(out)
	}
	out = query(parseGameSnapshot("Location: Brill", 1770000003), "get_recipes", "")
	if out["status"] != "unavailable" || out["reason"] != "Skills were disabled or unavailable for this question." {
		t.Fatal(out)
	}
}

func TestRecipeProbeThroughScopedBridge(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := parseGameSnapshot("Skills: Cooking 1/75\nDetail lookup: supported\nCard: {\"section\":\"skills\",\"kind\":\"recipe_lookup\",\"name\":\"own_recipe_by_id\"}", now.Unix())
	s.Character, s.Login, s.Session, s.RunID = "Player-1-A1", "login", "addon-session", "run"
	session := newSession("", nil, func() {})
	session.runID = s.RunID
	session.pulseDetailBridge(dir, now)
	id, err := saveGameSnapshot(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	type reply struct {
		result mcpObject
		err    error
	}
	done := make(chan reply, 1)
	go func() {
		out, err := queryGameSnapshot(dir, "get_recipes", json.RawMessage(fmt.Sprintf(`{"snapshot_id":%q,"id":7002,"profession_id":185,"known":false}`, id)))
		done <- reply{out, err}
	}()
	var req *detailRequest
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond * 5) {
		req = nextDetailRequest(dir, s.RunID, "", time.Now())
		if req != nil {
			break
		}
	}
	if req == nil {
		t.Fatal("no live recipe request")
	}
	if req.Character != s.Character || req.Login != s.Login || req.Refs[0].Kind != "recipe" || req.Refs[0].Section != "skills" {
		t.Fatal(req)
	}
	p := packet{Type: "details", RequestID: req.ID, RunID: s.RunID, Session: s.Session, Character: "Player-2-B2", Login: s.Login,
		Cards: []gameEntry{{"id": float64(7002), "kind": "recipe", "status": "ready", "learned": false, "professionID": 185, "professionName": "Cooking", "name": "Crispy Bat Wing"}}}
	if err := acceptGameDetails(dir, s.RunID, p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if nextDetailRequest(dir, s.RunID, req.ID, time.Now()) == nil {
		t.Fatal("foreign identity completed player's lookup")
	}
	p.Character = s.Character
	if err := acceptGameDetails(dir, s.RunID, p, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.result["match"] != "exact" || r.result["total"] != 1 {
			t.Fatal(r)
		}
		entry := r.result["entries"].([]mcpObject)[0]
		if entry["learned"] != false || entry["freshness"] != "live_lookup" || entry["read_at"] == "" {
			t.Fatal(entry)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no confirmed recipe result")
	}
}
