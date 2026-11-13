package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexReuseMaterializesOnlyRequestedSections(t *testing.T) {
	dir := t.TempDir()
	s := newSession("", nil, func() {})
	first := packet{Type: "reply", Session: "game", RunID: "companion", Seq: 8, At: 1000, Text: "один", ContextText: capturedJournal + "\n" + gearCards}
	prompt, err := s.indexes.prompt(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	id := snapshotEnvelope.FindStringSubmatch(prompt)[1]
	if ack := s.payload().SnapshotBases["game"]; ack.ID != id || ack.Seq != 8 {
		t.Fatal(ack)
	}
	next := first
	next.Seq, next.At, next.Text = 9, 2000, "два"
	next.ContextText = "Game: WoW Forever\nLocation: Undercity\nEquipped: Black staff\nCard: " + `{"section":"gear","kind":"item","id":44,"name":"Black staff"}` + "\nIndex base: " + id + "\nReuse: skills,quests\nDetail lookup: supported"
	text, err := s.indexes.prompt(dir, next)
	if err != nil || strings.Contains(text, "Black staff") {
		t.Fatal(text, err)
	}
	secondID := snapshotEnvelope.FindStringSubmatch(text)[1]
	second, err := loadGameSnapshot(dir, secondID)
	if err != nil || secondID == id || second.Captured == "1970-01-01T00:16:40Z" || second.Sections["location"] != "Undercity" || second.Sections["gear"] != "Black staff" || len(second.Quests) != 2 || !second.Quests[1].Tracked {
		t.Fatal(second, err)
	}
	if len(second.Entries) != 2 || second.Entries[0].id() != 44 || second.Entries[1].str("section") != "skills" || second.Entries[1].str("description") != "Владение мечом" {
		t.Fatal(second.Entries)
	}
	if _, leaked := second.Sections["references"]; leaked {
		t.Fatal("attachment from another question resurfaced")
	}
	if _, leaked := second.Sections["spells"]; leaked {
		t.Fatal("disabled section resurfaced")
	}
	if ack := s.payload().SnapshotBases["game"]; ack.ID != secondID || ack.Seq != 9 {
		t.Fatal(ack)
	}

	// Simulate an older message in flight while the player advances the base.
	// It still resolves from disk even outside the in-memory ring.
	next.ContextText = "Detail lookup: supported\nIndex base: " + id + "\nReuse: gear"
	if detached, err := gamePrompt(dir, next); err != nil || detached == "" {
		t.Fatal(detached, err)
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.indexes.prompt(dir, next); err != nil {
		t.Fatal("live acknowledged base lost", err)
	}
	if _, err := gamePrompt(dir, next); err == nil {
		t.Fatal("unknown base silently used")
	}
}

func TestIndexReuseRejectsForeignAndMixedSources(t *testing.T) {
	dir := t.TempDir()
	s := newSession("", nil, func() {})
	p := packet{Seq: 1, RunID: "one", Session: "alice", ContextText: gearCards}
	_, err := s.indexes.prompt(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	id := s.payload().SnapshotBases["alice"].ID
	for _, tc := range []struct{ run, session, body string }{
		{"two", "alice", "Reuse: gear"}, {"one", "bob", "Reuse: gear"}, {"one", "alice", "Reuse: references"},
		{"one", "alice", "Equipped: another item\nReuse: gear"}, {"one", "alice", "Reuse: gear,gear"}, {"one", "alice", "Reuse: bags"},
	} {
		q := p
		q.RunID, q.Session, q.ContextText = tc.run, tc.session, "Detail lookup: supported\nIndex base: "+id+"\n"+tc.body
		if _, err := s.indexes.prompt(dir, q); err == nil {
			t.Fatal("invalid index reference accepted", tc)
		}
	}
	if len(s.payload().SnapshotBases) != 0 {
		t.Fatal("missing base would be repeatedly advertised")
	}
	if _, err := s.indexes.prompt(filepath.Join(dir, id+".json"), p); err == nil || len(s.payload().SnapshotBases) != 0 {
		t.Fatal("failed save was advertised as a base")
	}
}
