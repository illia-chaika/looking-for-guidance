package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJournalShowsObservedActionsAndSources(t *testing.T) {
	const f = `{"id":"t","status":"completed","items":[{"type":"mcpToolCall","id":"1","server":"wow_game","tool":"get_context","status":"completed","arguments":{"sections":["gear"],"snapshot_id":"dont_display_me"},"result":{"content":[{"text":"{\"captured\":\"now\",\"sections\":{\"gear\":\"long-private-inventory\"}}"}]}},{"type":"mcpToolCall","id":"2","server":"wow_game","tool":"get_entries","status":"completed","arguments":{"section":"skills"},"result":{"content":[{"text":"category unavailable"}]}},{"type":"webSearch","id":"3","query":"fallback","action":{"type":"search","queries":["WoW Forever patch","WoW Forever builds"]}},{"type":"webSearch","id":"4","action":{"type":"openPage","url":"https://worldofwarcraft.blizzard.com/news/123"}},{"type":"reasoning","content":["never show reasoning"]},{"type":"agentMessage","text":"Готово"}]}`
	var turn cxTurn
	if err := json.Unmarshal([]byte(f), &turn); err != nil {
		t.Fatal(err)
	}
	got := transcript([]cxTurn{turn}, "", nil)
	if len(got) != 2 {
		t.Fatal(got)
	}
	a := got[0]
	if !strings.Contains(a.Text, "Интернет: 2") || len(a.Events) != 4 || a.Events[0].Status != "ok" || a.Events[0].Label != "Экипировка" || a.Events[1].Status != "failed" || !strings.Contains(a.Events[2].Detail, "builds") || a.Events[3].URL != "https://worldofwarcraft.blizzard.com/news/123" {
		t.Fatalf("audit=%+v", a)
	}
	if got[1].Source != "Экипировка · Поиск без подтверждения" {
		t.Fatal(got[1])
	}
	b, _ := json.Marshal(got)
	for _, private := range []string{"dont_display_me", "long-private-inventory", "never show reasoning"} {
		if strings.Contains(string(b), private) {
			t.Fatal(private)
		}
	}
	turn.Items = []cxItem{{Type: "agentMessage", Text: "wowhead.com"}}
	got = transcript([]cxTurn{turn}, "", nil)
	if len(got) != 1 || got[0].Source != "Без веб-поиска" {
		t.Fatal(got)
	}
	for _, url := range []string{"file:///private", "https://user:pass@host/path", "https://example.org/|Haddon:foo"} {
		if safeWebURL(url) != "" {
			t.Fatal(url)
		}
	}
}
func TestJournalReplacesStartedWithFinished(t *testing.T) {
	s := newSession("", nil, func() {})
	s.upsert(cxThread{ID: "t1"})
	s.order = []string{"t1"}
	for _, stage := range []string{"started", "completed"} {
		status := "inProgress"
		if stage == "completed" {
			status = "failed"
		}
		s.notify("item/"+stage, json.RawMessage(`{"threadId":"t1","turnId":"u1","item":{"type":"mcpToolCall","id":"x","server":"wow_game","tool":"get_details","status":"`+status+`"}}`))
		got := s.payload().Threads[0].Turns
		if len(got) != 1 || len(got[0].Events) != 1 || got[0].Events[0].Status != map[string]string{"started": "running", "completed": "failed"}[stage] {
			t.Fatal(stage, got)
		}
	}
}

func TestJournalDoesNotRewindCallOnLaggingHistory(t *testing.T) {
	raw := cxItem{ID: "m", Type: "mcpToolCall", Server: "wow_game", Tool: "get_details", Status: "inProgress"}
	live := raw
	live.Status = "failed"
	s := threadState{turns: []cxTurn{{ID: "turn", Status: "inProgress", Items: []cxItem{{ID: "q", Type: "userMessage"}, raw}}}, live: &cxTurn{ID: "turn", Status: "inProgress", Items: []cxItem{live}}}
	shown := s.allTurns()
	if len(shown) != 1 || len(shown[0].Items) != 2 || shown[0].Items[1].Status != "failed" || s.turns[0].Items[1].Status != "inProgress" {
		t.Fatal(shown)
	}
}

func TestJournalShowsGameSectionsAcrossVersions(t *testing.T) {
	call := func(name, args, result string, failed bool) cxItem {
		env, err := json.Marshal(map[string]any{
			"type": "mcpToolCall", "server": "wow_game", "tool": name, "status": "completed",
			"arguments": json.RawMessage(args), "result": map[string]any{"isError": failed, "content": []map[string]string{{"text": result}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var it cxItem
		if err := json.Unmarshal(env, &it); err != nil {
			t.Fatal(err)
		}
		return it
	}
	items := []cxItem{
		call("get_entries", `{"section":"talents","limit":50}`, "invalid section, entry filter or page bounds", true),
		call("search_spellbook", `{"limit":60}`, "limit 60 is outside 1–30; omit limit for 12 or request up to 30, then use next_offset", true),
		call("get_equipment", `{"slot":16,"id":123}`, `{"captured":"t","total":1,"entries":[{"id":123,"name":"Short Staff"}]}`, false),
		call("get_talents", `{"view":"all","offset":30,"limit":30}`, `{"captured":"t","total":32,"entries":[{"id":42},{"id":43}]}`, false),
		call("search_spellbook", `{"query":"Summon","known":true}`, `{"captured":"t","total":0,"entries":[]}`, false),
		{Type: "agentMessage", Text: "Текст"},
	}
	out := transcript([]cxTurn{{Status: "completed", Items: items}}, "", nil)
	if len(out) != 2 || out[0].Text != "Таланты · Заклинания · Экипировка" || out[1].Source != "Экипировка · Таланты · Заклинания · Без веб-поиска" {
		t.Fatalf("turns: %+v", out)
	}
	events := out[0].Events
	want := []string{"лимит 50 · допустимо от 1 до 30 записей", "лимит 60 · допустимо от 1 до 30 записей", "правая рука · Short Staff · найдено: 1", "всё дерево · записи 31–32 из 32", "изученные · «Summon» · нет совпадений на этой странице"}
	for i, w := range want {
		if events[i].Detail != w {
			t.Errorf("event %d: %+v want %s", i, events[i], w)
		}
	}
	if describe(items[0], nil) != "Смотрю · Таланты" || describe(items[1], nil) != "Смотрю · Заклинания" {
		t.Fatal("indistinguishable live tool names")
	}
}

func TestJournalShowsLookupNamesAndRealFailureReasons(t *testing.T) {
	var it cxItem
	if err := json.Unmarshal([]byte(`{"type":"mcpToolCall","server":"wow_game","tool":"get_details","status":"completed","arguments":{"refs":[{"kind":"item","id":159},{"kind":"spell","id":686}]},"result":{"content":[{"text":"{\"captured\":\"now\",\"cards\":[{\"id\":159,\"name\":\"Water\",\"kind\":\"item\",\"status\":\"ready\"},{\"id\":686,\"name\":\"Shadow Bolt\",\"kind\":\"spell\",\"status\":\"partial\"}]}"}]}}`), &it); err != nil {
		t.Fatal(err)
	}
	e, _ := gameActivityEvent(it)
	if e.Label != "Карточки предметов и заклинаний" || e.Detail != "Water · Shadow Bolt · Полных карточек: 1 из 2" || e.Status != "partial" {
		t.Fatal(e)
	}
	it.Result.Content[0].Text = "client offline"
	it.Result.IsError = true
	e, _ = gameActivityEvent(it)
	if e.Status != "failed" || !strings.Contains(e.Detail, "client offline") {
		t.Fatal(e)
	}
}
