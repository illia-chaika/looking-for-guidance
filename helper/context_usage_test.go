package main

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestContextMeasurementIsPerThreadAndModel(t *testing.T) {
	s := newSession("", nil, func() {})
	a := s.upsert(cxThread{ID: "a", Model: "model-a"})
	b := s.upsert(cxThread{ID: "b", Model: "model-a"})
	emit := func(raw string) { s.notify("thread/tokenUsage/updated", json.RawMessage(raw)) }
	valid := `{"threadId":"a","turnId":"t","tokenUsage":{"last":{"totalTokens":53020},"total":{"totalTokens":750000},"modelContextWindow":475000}}`
	emit(valid)
	check := func() {
		t.Helper()
		v := a.payload(nil)
		if v.ContextLeft == nil || *v.ContextLeft != 91 || v.ContextUsed == nil || *v.ContextUsed != 53020 || v.ContextWindow == nil || *v.ContextWindow != 475000 {
			t.Fatalf("token meter should use the last complete Codex observation: %+v", v)
		}
		if v := b.payload(nil); v.ContextUsed != nil || v.ContextWindow != nil || v.ContextLeft != nil {
			t.Fatal("a second thread without token usage inherited the meter")
		}
	}
	check()
	emit(`{"threadId":"a","turnId":"t","tokenUsage":{"last":{"totalTokens":100000},"modelContextWindow":null}}`)
	emit(`{"threadId":"a","turnId":"t","tokenUsage":{"last":{},"modelContextWindow":1000}}`)
	emit(`{"threadId":"a","turnId":"t","tokenUsage":{"last":{"totalTokens":-8},"modelContextWindow":475000}}`)
	check()
	// Thread listing often omits model metadata; that alone shouldn't
	// change or clear the last measurement.
	s.upsert(cxThread{ID: "a"})
	check()
	s.upsert(cxThread{ID: "a", Model: "different-model"})
	v := a.payload(nil)
	if v.ContextUsed != nil || v.ContextWindow != nil || v.ContextLeft != nil {
		t.Fatal("a new model displayed the previous model's token budget")
	}
	emit(`{"threadId":"a","turnId":"t","tokenUsage":{"last":{"totalTokens":0},"modelContextWindow":125000}}`)
	v = a.payload(nil)
	if v.ContextUsed == nil || *v.ContextUsed != 0 || v.ContextLeft == nil || *v.ContextLeft != 100 {
		t.Fatal("a reported zero is a known measurement")
	}
}

func TestCompactionStartCompleteAndRestore(t *testing.T) {
	s := newSession("", nil, func() {})
	thread := s.upsert(cxThread{ID: "first", Status: cxStatus{Type: "active"}})
	other := s.upsert(cxThread{ID: "other", Status: cxStatus{Type: "idle"}})
	s.notify("turn/started", json.RawMessage(`{"threadId":"first","turn":{"id":"t","status":"inProgress"}}`))
	item := json.RawMessage(`{"threadId":"first","turnId":"t","item":{"id":"comp1","type":"contextCompaction"}}`)
	count := func(p payloadThread) int {
		n := 0
		for _, row := range p.Turns {
			if row.Role == "compaction" {
				n++
			}
		}
		return n
	}
	s.notify("item/started", item)
	if !thread.payload(nil).Compacting || count(thread.payload(nil)) != 0 || other.payload(nil).Compacting {
		t.Fatal("start should show progress on its own thread only")
	}
	// Receiving both events before a payload write is common for a short compact.
	s.notify("item/completed", item)
	s.notify("item/completed", item)
	if thread.payload(nil).Compacting || count(thread.payload(nil)) != 1 {
		t.Fatal("completion should publish one durable transcript marker")
	}
	s.notify("turn/completed", json.RawMessage(`{"threadId":"first","turn":{"id":"t","status":"completed"}}`))
	turn := cxTurn{ID: "t", Status: "completed", Items: []cxItem{{ID: "different", Type: "reasoning"}}}
	thread.setTurns([]cxTurn{turn})
	if count(thread.payload(nil)) != 1 {
		t.Fatal("equal-size stale poll wiped out a completed compaction")
	}
	turn.Items = append(turn.Items, cxItem{ID: "comp1", Type: "contextCompaction"})
	thread.setTurns([]cxTurn{turn})
	if thread.live != nil || count(thread.payload(nil)) != 1 {
		t.Fatal("server history should replace the live copy, without duplication")
	}
	// A later helper session restores the marker from the actual stored turn,
	// even when no token data or notifications are available on reconnect.
	fresh := &threadState{}
	fresh.setTurns([]cxTurn{turn})
	if fresh.payload(nil).ContextLeft != nil || fresh.payload(nil).Compacting || count(fresh.payload(nil)) != 1 {
		t.Fatal("reopened thread lost the marker or fabricated live progress/usage")
	}
}

func TestInterruptedCompactionDoesNotBecomeACompletion(t *testing.T) {
	s := newSession("", nil, func() {})
	p := s.upsert(cxThread{ID: "first", Status: cxStatus{Type: "active"}})
	s.notify("item/started", json.RawMessage(`{"threadId":"first","turnId":"interrupted","item":{"id":"c1","type":"contextCompaction"}}`))
	s.notify("turn/completed", json.RawMessage(`{"threadId":"first","turn":{"id":"interrupted","status":"interrupted"}}`))
	p.setTurns([]cxTurn{{ID: "interrupted", Status: "interrupted"}})
	if got := p.payload(nil); got.Compacting || slices.ContainsFunc(got.Turns, func(row payloadTurn) bool { return row.Role == "compaction" }) {
		t.Fatal("an interrupted compaction was reported as still running or successful")
	}
}

func TestCompactionKeepsPositionAroundAnswer(t *testing.T) {
	rows := transcript([]cxTurn{{Status: "completed", Items: []cxItem{
		{Type: "userMessage", Content: json.RawMessage(`[{"type":"text","text":"Вопрос"}]`)},
		{Type: "contextCompaction", ID: "before"}, {Type: "agentMessage", Text: "Ответ"}, {Type: "contextCompaction", ID: "after"},
	}}}, "", nil)
	var roles []string
	for _, row := range rows {
		roles = append(roles, row.Role)
	}
	if !slices.Equal(roles, []string{"user", "compaction", "assistant", "compaction"}) {
		t.Fatalf("compaction order: %v", roles)
	}
}
