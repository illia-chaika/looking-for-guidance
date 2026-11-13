package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryAcrossChatsAndConcurrentEdits(t *testing.T) {
	dir := t.TempDir()
	const player = "Player-17-0ABCDEFA"
	const login = "s1000-first"
	const quote = "Wool goes to my tailor."
	now := time.Now()
	snap := func(guid, token, msg string) string {
		prompt, err := gamePrompt(dir, packet{Character: guid, Login: token, Thread: "original", Text: msg, Plain: true, At: float64(now.Unix())})
		if err != nil {
			t.Fatal(err)
		}
		match := snapshotEnvelope.FindStringSubmatch(prompt)
		if match == nil {
			t.Fatal("missing identity-bound snapshot", prompt)
		}
		return match[1]
	}
	id := snap(player, login, quote)
	args := mcpObject{"snapshot_id": id, "id": "wool", "text": "Reserve wool for tailoring.", "scope": "character", "revision": 0, "quote": quote}
	call := func(name string, a mcpObject) (mcpObject, error) {
		raw, _ := json.Marshal(a)
		return queryGameSnapshot(dir, name, raw)
	}
	list := func(s string) []memoryNote {
		r, err := call("get_memory", mcpObject{"snapshot_id": s, "include_sources": true})
		if err != nil {
			t.Fatal(err)
		}
		return r["notes"].([]memoryNote)
	}
	if _, err := call("upsert_memory", args); err != nil {
		t.Fatal(err)
	}
	if _, err := call("upsert_memory", args); err != nil {
		t.Fatal("retry", err)
	}
	notes := list(snap(player, "s2000-next", "What can I sell?"))
	if len(notes) != 1 || notes[0].Revision != 1 || notes[0].Source.Quote != quote || notes[0].Source.Thread != "original" {
		t.Fatal(notes)
	}
	if got := list(snap("Player-17-00123456", login, "Alt")); len(got) != 0 {
		t.Fatal("foreign memory", got)
	}
	args["quote"] = "Invented source"
	if _, err := call("upsert_memory", args); err == nil {
		t.Fatal("accepted invented quote")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, v := range []string{"Give wool to my friend", "Use wool for bandages"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := updateMemory(dir, player, login, memoryMutation{Action: "upsert", ID: "wool", Text: text, Scope: "character", Manual: true, Revision: 1, Captured: now.Unix(), Quote: text, Operation: text}, now)
			results <- err
		}(v)
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatal("simultaneous edits committed", ok)
	}
	updated := list(id)
	if len(updated) != 1 || updated[0].Revision != 2 || !updated[0].Source.Manual {
		t.Fatal(updated)
	}
	forget := snap(player, login, "We are done with wool.")
	_, err := call("forget_memory", mcpObject{"snapshot_id": forget, "id": "wool", "revision": 2, "quote": "done with wool"})
	if err != nil {
		t.Fatal(err)
	}
	args["quote"] = quote
	if _, err = call("upsert_memory", args); err != nil {
		t.Fatal("original retry", err)
	}
	if len(list(id)) != 0 {
		t.Fatal("resurrected deleted fact")
	}
	data, err := readMemory(characterPath(dir, player))
	if err != nil {
		t.Fatal(err)
	}
	serialized, _ := json.Marshal(data)
	if strings.Contains(string(serialized), quote) {
		t.Fatal("source retained after deletion")
	}
}

func TestMemoryScopesAndUIReceipt(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	const guid = "Player-1-000001AB"
	const login = "s123-first"
	add := func(key, scope string) {
		_, err := updateMemory(dir, guid, login, memoryMutation{Action: "upsert", ID: key, Text: "Play in a group", Scope: scope, Operation: key, Captured: now.Unix()}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	add("tonight", "day")
	add("outing", "session")
	db, err := readMemory(characterPath(dir, guid))
	if err != nil {
		t.Fatal(err)
	}
	if got := db.view(guid, "s124-relog", now); len(got.Notes) != 1 || got.Notes[0].ID != "tonight" {
		t.Fatal(got)
	}
	if got := db.view(guid, login, now.Add(25*time.Hour)); len(got.Notes) != 1 || got.Notes[0].ID != "outing" {
		t.Fatal(got)
	}
	s := newSession("", map[string]string{}, func() {})
	edit := packet{Type: "memory", Session: "ui", Seq: 20, Character: guid, Login: login, MemoryAction: "edit", NoteID: "outing", Revision: 2, Scope: "session", NoteText: "Pair with a mage"}
	s.observeCharacter(dir, edit)
	s.syncMemory(dir, now, &edit)
	s.syncMemory(dir, now, nil)
	if s.memory == nil || s.memory.RequestSeq != 20 || s.memory.Session != "ui" || s.memory.Error != "" || s.memory.Notes[0].Text != edit.NoteText {
		t.Fatal(s.memory)
	}
	oldPath := characterPath(dir, guid)
	if filepath.Dir(oldPath) != filepath.Join(dir, "character-memory") {
		t.Fatal(oldPath)
	}
}
