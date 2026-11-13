package main

import (
	"encoding/json"
	"strings"
)

// The existing project instructions may have been cached by a resumed writer.
// Carry the checking rule alongside each new game question, including follow-ups.
const gameEvidenceHint = "Before answering, verify game facts against the current WoW Forever client or a source for WoW Forever. A skill rank is not a learned recipe. For personal crafting advice use get_recipes, retry by confirmed recipe spell ID if the name is missing, and check materials. If the snapshot cannot support a claim, search the web for this version, read a relevant source, and cite its URL. If your first lookup is inconclusive, refine it; say what is still unknown if verification fails."
const evidenceOpen = "<wow_evidence version=\"1\">"
const evidenceClose = "</wow_evidence>\n\n"

func groundingPrompt(message string) string {
	if envelope := snapshotEnvelope.FindStringIndex(message); envelope != nil {
		const endTag = "</wow_snapshot>"
		if index := strings.Index(message[:envelope[1]], endTag); index >= 0 {
			return message[:index] + " " + gameEvidenceHint + message[index:]
		}
	}
	return evidenceOpen + gameEvidenceHint + evidenceClose + message
}

// Standalone web search may expose source records in app-server results.
// Older and hosted variants may expose only the attempted query or action URL.
// Keep a bit once decoded. Large response snippets don't belong in the polling
// cache or HUD, which are read more frequently than new turns arrive.
type webResults bool

func (r *webResults) UnmarshalJSON(raw []byte) error {
	var rows []struct {
		Type       string          `json:"type"`
		URL        string          `json:"url"`
		Title      string          `json:"title"`
		Snippet    string          `json:"snippet"`
		Error      json.RawMessage `json:"error"`
		IsError    bool            `json:"isError"`
		StatusCode int             `json:"status_code"`
	}
	*r = false
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	// This is the observed shape returned by the standalone tool for both
	// search and openPage. Unrecognized future shapes are "unconfirmed".
	for _, row := range rows {
		if row.Type != "text_result" || row.IsError || row.StatusCode >= 400 ||
			(len(row.Error) != 0 && string(row.Error) != "null") || safeWebURL(row.URL) == "" {
			continue
		}
		snippet := strings.ToLower(strings.TrimSpace(row.Snippet))
		if snippet == "" || strings.HasPrefix(strings.ToLower(row.Title), "internal error") ||
			strings.HasPrefix(snippet, "internal error") || strings.HasPrefix(snippet, "failed to fetch") ||
			strings.Contains(snippet, "is not accessible via this tool") {
			continue
		}
		*r = true
		break
	}
	return nil
}
