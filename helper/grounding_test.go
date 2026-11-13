package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvidenceGuidanceStaysOutOfVisiblePlayerQuestion(t *testing.T) {
	id := strings.Repeat("7", 32)
	question := "А готовка в принципе нужна? Для чего?"
	journal := &journalCard{Topic: "bags", Title: "Backpack"}
	for _, card := range []*journalCard{nil, journal} {
		raw := groundingPrompt(snapshotMessage(id, question, card))
		if !strings.Contains(raw, gameEvidenceHint) || (card != nil && !strings.Contains(raw, journalHint)) {
			t.Fatal("game grounding or journal instructions disappeared")
		}
		if parsed, context := stripContext(raw); parsed != question || !context {
			t.Fatal("grounding guidance leaked into the chat")
		}
		if m := snapshotEnvelope.FindStringSubmatch(raw); m == nil || m[1] != id {
			t.Fatal("lost immutable snapshot reference")
		}
	}
	raw := groundingPrompt(question)
	if got, context := stripContext(raw); got != question || context {
		t.Fatalf("plain question = %q, context=%v", got, context)
	}
	if got, context := stripContext(evidenceOpen + "Earlier wording." + evidenceClose + question); got != question || context {
		t.Fatal("older saved hint appeared in history")
	}
}

func TestWebProvenanceRequiresReturnedSourceData(t *testing.T) {
	const action = `"type":"webSearch","action":{"type":"openPage","url":"https://example.com/forever"}`
	const result = `"results":[{"type":"text_result","title":"Cooking","url":"https://example.com/forever","snippet":"The recipe lists cooked fish."}]`
	for _, test := range []struct {
		note    string
		item    string
		started bool
		want    string
	}{
		{"observed web result", "{" + action + "," + result + "}", false, "Веб-поиск"},
		{"old API exposes action only", "{" + action + "}", false, "Поиск без подтверждения"},
		{"started only", "{" + action + "," + result + "}", true, "Поиск без подтверждения"},
		{"source name without retrieved data", "{" + action + `,"results":[{"title":"Cooking","url":"https://example.com/forever"}]}`, false, "Поиск без подтверждения"},
		{"failed page", "{" + action + `,"results":[{"type":"error","url":"https://example.com/forever","text":"unavailable"}]}`, false, "Поиск без подтверждения"},
		{"http error", "{" + action + `,"results":[{"type":"text_result","url":"https://example.com/forever","snippet":"Not Found","status_code":404}]}`, false, "Поиск без подтверждения"},
		{"tool error page", "{" + action + `,"results":[{"type":"text_result","url":"https://example.com/forever","snippet":"Internal Error: page is not accessible via this tool"}]}`, false, "Поиск без подтверждения"},
		{"no results", "{" + action + `,"results":[]}`, false, "Поиск без подтверждения"},
		{"memory is not world evidence", `{"type":"mcpToolCall","server":"wow_game","tool":"get_memory","status":"completed","result":{"content":[{"text":"{\"captured\":\"now\",\"notes\":[]}"}]}}`, false, "Без веб-поиска"},
	} {
		t.Run(test.note, func(t *testing.T) {
			var it cxItem
			if err := json.Unmarshal([]byte(test.item), &it); err != nil {
				t.Fatal(err)
			}
			it.StartedOnly = test.started
			rows := transcript([]cxTurn{{Status: "completed", Items: []cxItem{it, {Type: "agentMessage", Text: "Ответ игроку"}}}}, "", nil)
			got := rows[len(rows)-1]
			if got.Role != "assistant" || got.Text != "Ответ игроку" || got.Source != test.want {
				t.Fatal(got)
			}
		})
	}
}
