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

const capturedJournal = "Game: WoW Forever 1.60.1\nCharacter: level 10 Undead Warlock\nLocation: Brill, Tirisfal Glades 61.2, 52.9\nQuests (2):\n  The Prodigal Lich (quest:405) level 10 · ready to turn in · 1/1 Talk to Bethor · selected\n  Новое задание (quest:407) level 11 · tracked · 2/3 остаток\nTalents: Test · Rank 1"

func TestJournalActionHasEvidenceButIsNotPlayerSpeech(t *testing.T) {
	dir := t.TempDir()
	card := journalCard{Topic: "bags", Title: "One free slot", Facts: "Keep my cloth for tailoring", Locale: "enUS"}
	p := packet{Character: "Player-1-ABC", Login: "s123-abc", Thread: "brill", ContextText: capturedJournal, Text: card.Title, Journal: &card}
	message, err := gamePrompt(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	ref := snapshotEnvelope.FindStringSubmatch(message)
	if len(ref) != 2 || !strings.Contains(message, journalHint) {
		t.Fatalf("missing journal envelope: %q", message)
	}
	old, err := loadGameSnapshot(dir, ref[1])
	if err != nil || old.Question != "" || old.Character != p.Character {
		t.Fatalf("wrong evidence owner: %+v %v", old, err)
	}
	section, err := queryGameSnapshot(dir, "get_context", json.RawMessage(`{"snapshot_id":"`+ref[1]+`","sections":["journal"]}`))
	if err != nil || !strings.Contains(section["sections"].(map[string]string)["journal"], card.Facts) {
		t.Fatal(section, err)
	}
	p.ContextText, p.RetrySnapshot = "Location: Undercity", ref[1]
	if replay, err := gamePrompt(dir, p); err != nil || replay != message {
		t.Fatal("retry recaptured context", err)
	}
	p.Thread = "other"
	if _, err := gamePrompt(dir, p); err == nil {
		t.Fatal("accepted retry in another chat")
	}
	p.Thread, p.Character = "brill", "Player-1-DEF"
	if _, err := gamePrompt(dir, p); err == nil {
		t.Fatal("accepted another character's snapshot")
	}
}

func TestQuestionUsesPrivateImmutableSnapshot(t *testing.T) {
	dir := t.TempDir()
	p := packet{Text: "Куда дальше?", ContextText: capturedJournal, At: 1790000000}
	prompt, err := gamePrompt(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	ref := snapshotEnvelope.FindStringSubmatch(prompt)
	if len(ref) != 2 || strings.Contains(prompt, "Brill") || strings.Contains(prompt, "quest:405") || !strings.HasSuffix(prompt, p.Text) {
		t.Fatalf("unexpected envelope: %q", prompt)
	}
	id := ref[1]
	data, err := queryGameSnapshot(dir, "get_context", json.RawMessage(`{"snapshot_id":"`+id+`","sections":["character","gear"]}`))
	if err != nil || len(data["sections"].(map[string]string)) != 1 || data["unavailable"].([]string)[0] != "gear" {
		t.Fatalf("context: %#v %v", data, err)
	}
	for _, tc := range []struct {
		opts    string
		n       int
		details bool
		title   string
	}{
		{`"scope":"list"`, 2, false, "The Prodigal Lich"},
		{`"scope":"selected"`, 1, true, "The Prodigal Lich"},
		{`"scope":"tracked"`, 1, true, "Новое задание"},
		{`"quest_id":407`, 1, true, "Новое задание"},
	} {
		r, err := queryGameSnapshot(dir, "get_quests", json.RawMessage(`{"snapshot_id":"`+id+`",`+tc.opts+`}`))
		if err != nil {
			t.Fatal(err)
		}
		qs := r["quests"].([]gameQuest)
		if len(qs) != tc.n || qs[0].Title != tc.title || (qs[0].Details != "") != tc.details {
			t.Fatalf("%s: %#v", tc.opts, qs)
		}
	}
	p.ContextText = "Character: level 11"
	fresh, err := gamePrompt(dir, p)
	if err != nil || strings.Contains(fresh, id) {
		t.Fatal("new question reused an old snapshot")
	}
	old, err := loadGameSnapshot(dir, id)
	if err != nil || old.Sections["character"] != "level 10 Undead Warlock" || old.summary() != "Brill, Tirisfal Glades · Заданий: 2" {
		t.Fatalf("snapshot mutated: %+v, %v", old, err)
	}
	if _, err = loadGameSnapshot(dir, "../settings"); err == nil {
		t.Fatal("accepted invalid snapshot ID")
	}
	fi, _ := os.Stat(filepath.Join(dir, id+".json"))
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("snapshot permissions: %v", fi.Mode())
	}
	p.Plain = true
	if text, err := gamePrompt(dir, p); err != nil || text != p.Text {
		t.Fatal("disabled game context shared data")
	}
}

func TestLocalMCPProtocolAndSourceLabels(t *testing.T) {
	dir := t.TempDir()
	prompt, err := gamePrompt(dir, packet{ContextText: capturedJournal, Text: "проверка"})
	if err != nil {
		t.Fatal(err)
	}
	id := snapshotEnvelope.FindStringSubmatch(prompt)[1]
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"tools/list","id":2}`,
		`{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"get_quests","arguments":{"snapshot_id":"` + id + `","scope":"tracked"}}}`,
		`{"jsonrpc":"2.0","method":"tools/call","id":4,"params":{"name":"get_quests","arguments":{"snapshot_id":"` + strings.Repeat("0", 32) + `"}}}`,
	}, "\n")
	var out bytes.Buffer
	if err := runGameMCP(strings.NewReader(in), &out, dir); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatal(out.String())
	}
	if !strings.Contains(lines[1], "get_context") || !strings.Contains(lines[2], "Новое задание") || !strings.Contains(lines[3], `"isError":true`) {
		t.Fatal(out.String())
	}
	var registry struct {
		Result struct {
			Tools []struct {
				Name string
				Meta struct {
					MaxChars int `json:"anthropic/maxResultSizeChars"`
				} `json:"_meta"`
			}
		}
	}
	if err := json.Unmarshal([]byte(lines[1]), &registry); err != nil {
		t.Fatal(err)
	}
	var expanded []string
	for _, tool := range registry.Result.Tools {
		if limit := tool.Meta.MaxChars; limit != 0 {
			if limit != 200_000 {
				t.Fatalf("%s declares unexpected Claude output limit %d", tool.Name, limit)
			}
			expanded = append(expanded, tool.Name)
		}
	}
	if len(expanded) != 1 || expanded[0] != "get_installed_addons" {
		t.Fatalf("unexpected tools with expanded Claude output: %v", expanded)
	}
	for i, want := range []string{"Журнал заданий", ""} {
		var res struct {
			Result json.RawMessage `json:"result"`
		}
		json.Unmarshal([]byte(lines[i+2]), &res)
		var item cxItem
		err := json.Unmarshal([]byte(`{"type":"mcpToolCall","server":"wow_game","tool":"get_quests","status":"completed","result":`+string(res.Result)+`}`), &item)
		e, _ := gameActivityEvent(item)
		if got := eventSource(item, e); err != nil || got != want {
			t.Fatalf("source=%s wanted %s (%v)", got, want, err)
		}
	}
}

func TestGameSessionFiltersThreadsAndRejectsUnknownTargets(t *testing.T) {
	s := newSession("", nil, func() {})
	s.projectDir = "/game"
	s.models = []payloadModel{{ID: "m"}}
	s.codex = "ready"
	if why := s.refuse(packet{Cwd: "/work", Model: "m"}); why == "" {
		t.Fatal("work folder offered to game")
	}
	var methods []string
	s.client = rpcClientFor(t, func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		methods = append(methods, method)
		switch method {
		case "thread/list":
			if !strings.Contains(string(params), `"cwd":"/game"`) {
				t.Fatal(string(params))
			}
			return json.RawMessage(`{"data":[{"id":"good","cwd":"/game"},{"id":"other","cwd":"/work"}]}`), nil
		default:
			t.Fatalf("unexpected method %q", method)
			return nil, nil
		}
	})
	s.refresh(time.Now())
	s.handle(packet{Type: "reply", Thread: "other", Text: "test"})
	s.handle(packet{Type: "rename", Thread: "other", Title: "test"})
	s.notify("thread/started", json.RawMessage(`{"thread":{"id":"other","cwd":"/work"}}`))
	if s.threads["other"] != nil || len(methods) != 1 || s.threads["good"] == nil {
		t.Fatal("cross-project conversation accepted")
	}
}
