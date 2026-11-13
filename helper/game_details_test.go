package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const gearCards = `Detail lookup: supported
Equipped: 2 items
Card: {"section":"gear","kind":"item","id":100,"slot":16,"name":"Клинок","link":"item:100:11"}
Card: {"section":"gear","kind":"item","id":100,"slot":17,"name":"Клинок","link":"item:100:22"}
References: 1
Card: {"section":"references","kind":"item","id":100,"name":"Клинок","link":"item:100:22"}
Skills: 1
Card: {"section":"skills","kind":"skill","id":43,"name":"Мечи","rank":27,"maxRank":50,"description":"Владение мечом"}
Spells known: 0`

func newCatalogQuestion(t *testing.T, dir string) string {
	t.Helper()
	prompt, err := gamePrompt(dir, packet{Type: "reply", Session: "player", RunID: "app", ContextText: gearCards, Text: "Сравни оружие."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Клинок") || strings.Contains(prompt, "item:100") {
		t.Fatal("catalog leaked into prompt")
	}
	return snapshotEnvelope.FindStringSubmatch(prompt)[1]
}

func TestEntrySearchKeepsVariantsAndLegacyData(t *testing.T) {
	dir := t.TempDir()
	id := newCatalogQuestion(t, dir)
	list := func(section, filters string) mcpObject {
		t.Helper()
		out, err := queryGameSnapshot(dir, "get_entries", json.RawMessage(`{"snapshot_id":"`+id+`","section":"`+section+`"`+filters+`}`))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := list("gear", `,"query":"КЛИн","limit":1`)
	if out["total"] != 2 || out["next_offset"] != 1 || out["entries"].([]gameEntry)[0].str("link") != "item:100:11" {
		t.Fatal(out)
	}
	if row := list("gear", `,"id":100,"offset":1`)["entries"].([]gameEntry); len(row) != 1 || row[0].str("link") != "item:100:22" {
		t.Fatal(row)
	}
	if row := list("gear", `,"slot":17,"link":"item:100:22"`)["entries"].([]gameEntry); len(row) != 1 {
		t.Fatal(row)
	}
	if row := list("skills", `,"query":"меч"`)["entries"].([]gameEntry); len(row) != 1 || row[0].str("description") != "Владение мечом" {
		t.Fatal(row)
	}
	if row := list("references", "")["entries"].([]gameEntry); len(row) != 1 || row[0].str("link") != "item:100:22" {
		t.Fatal(row)
	}
	if _, err := queryGameSnapshot(dir, "get_entries", json.RawMessage(`{"snapshot_id":"`+id+`","section":"bags"}`)); err == nil {
		t.Fatal("disabled category appears empty")
	}
	s, err := loadGameSnapshot(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []gameRef{
		{Kind: "item", ID: 100, Link: "item:100:11", Section: "references"},
		{Kind: "item", ID: 100, Link: "item:200:22", Section: "gear"},
		{Kind: "item", ID: 100, Link: "|Hitem:100|h[x]|h", Section: "gear"},
		{Kind: "spell", ID: 43, Section: "bags"},
	} {
		if _, err := ref.resolve(s); err == nil {
			t.Fatalf("invalid ref accepted: %+v", ref)
		}
	}
	id, err = saveGameSnapshot(dir, parseGameSnapshot("Equipped: item:3270, item:11848", 0))
	if err != nil {
		t.Fatal(err)
	}
	if out := list("gear", ""); out["legacy_summary"] != "item:3270, item:11848" {
		t.Fatal(out)
	}
}

func TestMCPClientDetailRoundTripAndCache(t *testing.T) {
	dir := t.TempDir()
	id := newCatalogQuestion(t, dir)
	args := json.RawMessage(`{"snapshot_id":"` + id + `","refs":[{"kind":"item","id":100,"section":"gear","link":"item:100:22"},{"kind":"item","id":100,"section":"gear"},{"kind":"spell","id":42,"section":"spells"}]}`)
	lines := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_details","arguments":` + string(args) + `}}` + "\n"
	var wire bytes.Buffer
	done := make(chan error, 1)
	tray := newSession("", nil, func() {})
	tray.runID = "app"
	tray.pulseDetailBridge(dir, time.Now())
	go func() { done <- runGameMCP(strings.NewReader(lines), &wire, dir) }()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		tray.syncDetailRequest(dir, time.Now())
		if tray.detailRequest != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	req := tray.payload().DetailRequest
	if req == nil || req.Session != "player" || len(req.Refs) != 3 || req.Refs[0].Link != "item:100:22" {
		t.Fatalf("detail request missing: %+v", req)
	}
	if nextDetailRequest(dir, "other-run", "", time.Now()) != nil {
		t.Fatal("another run saw the request")
	}
	raw := `{"type":"details","requestId":"` + req.ID + `","session":"different","runId":"app","at":1790000000,"cards":[{"kind":"item","id":100,"link":"item:100:22","status":"ready","stats":{"intellect":5}},{"kind":"item","id":100,"status":"ready","stats":{"intellect":1}},{"kind":"spell","id":42,"status":"partial","name":"Пока без описания"}]}`
	var p packet
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if err := acceptGameDetails(dir, "app", p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(detailDir(dir), req.ID+".out")); !os.IsNotExist(err) {
		t.Fatal("reply from wrong player accepted")
	}
	p.Session = "player"
	if err := acceptGameDetails(dir, "app", p, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second * 3):
		t.Fatal("detail RPC blocked")
	}
	var response struct {
		Result struct {
			IsError bool
			Content []struct{ Text string }
		}
	}
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(wire.String()), "\n")[1]), &response); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Captured string
		Cards    []gameEntry
	}
	if response.Result.IsError || json.Unmarshal([]byte(response.Result.Content[0].Text), &out) != nil || len(out.Cards) != 3 {
		t.Fatal(wire.String())
	}
	if out.Cards[0]["stats"].(map[string]any)["intellect"] != float64(5) || out.Cards[1]["stats"].(map[string]any)["intellect"] != float64(1) || out.Cards[2].str("status") != "partial" || out.Cards[0].str("read_at") == "" {
		t.Fatal(out)
	}
	for _, idx := range []int{0, 1, 2} {
		_, err := os.Stat(cacheCardPath(dir, id, req.Refs[idx]))
		if (idx == 2) != os.IsNotExist(err) {
			t.Fatalf("cache ref %d: %v", idx, err)
		}
	}
	closeDetailBridge(dir, tray.runID)
	cached, err := queryGameSnapshot(dir, "get_details", json.RawMessage(`{"snapshot_id":"`+id+`","refs":[{"kind":"item","id":100,"section":"gear","link":"item:100:22"}]}`))
	if err != nil || cached["cards"].([]gameEntry)[0].str("status") != "ready" {
		t.Fatalf("cached lookup: %v %v", cached, err)
	}
	tray.syncDetailRequest(dir, time.Now())
	if tray.payload().DetailRequest != nil {
		t.Fatal("completed lookup republished to game")
	}
	mixed, err := queryGameSnapshot(dir, "get_details", args)
	if err != nil || mixed["cards"].([]gameEntry)[0].str("status") != "ready" || !strings.Contains(mixed["cards"].([]gameEntry)[2].str("reason"), "has ended") {
		t.Fatalf("offline partial lookup: %+v %v", mixed, err)
	}
}

func TestReferencesOnlyUsesExactAttachment(t *testing.T) {
	dir := t.TempDir()
	prompt, err := gamePrompt(dir, packet{Text: "А это?", Session: "s", RunID: "r", ContextText: `Game: WoW Forever
References: 1
Card: {"section":"references","kind":"item","id":7,"name":"Посох","link":"item:7:112"}
Detail lookup: supported`})
	if err != nil {
		t.Fatal(err)
	}
	s, err := loadGameSnapshot(dir, snapshotEnvelope.FindStringSubmatch(prompt)[1])
	if err != nil || len(s.Sections) != 2 || len(s.Entries) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	r := gameRef{Kind: "item", ID: 7, Section: "references"}
	exact, err := r.resolve(s)
	if err != nil || exact.Link != "item:7:112" {
		t.Fatal(exact, err)
	}
	r.Section = "gear"
	if _, err := r.resolve(s); err == nil {
		t.Fatal("ambient section leaked from an attachment-only question")
	}
	s.Entries = append(s.Entries, gameEntry{"section": "references", "kind": "item", "id": float64(7), "link": "item:7:555"})
	r.Section = "references"
	if _, err := r.attachedVariant(s); err == nil {
		t.Fatal("ambiguous attached variant chosen silently")
	}
	s.Captured = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if err := verifyDetailBridge(dir, s, time.Now()); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatal(err)
	}
}

func TestDetailQueueHoldsCurrentAndPurgesExpired(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(detailDir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, c := range []string{"0", "1", "2"} {
		id := strings.Repeat(c, 32)
		r := detailEnvelope{detailRequest: detailRequest{ID: id, Session: "p"}, RunID: "r", Expires: now.Add(time.Duration(i-1)*time.Minute + 30*time.Second).UnixMilli()}
		body, _ := json.Marshal(r)
		if err := writeFileAtomic(filepath.Join(detailDir(dir), id+".req"), body); err != nil {
			t.Fatal(err)
		}
	}
	current := strings.Repeat("2", 32)
	if r := nextDetailRequest(dir, "r", current, now); r == nil || r.ID != current {
		t.Fatal("current request preempted", r)
	}
	if r := nextDetailRequest(dir, "r", "", now); r == nil || r.ID != strings.Repeat("1", 32) {
		t.Fatal("oldest waiting request skipped", r)
	}
	if _, err := os.Stat(filepath.Join(detailDir(dir), strings.Repeat("0", 32)+".req")); !os.IsNotExist(err) {
		t.Fatal("stale request retained")
	}
}
