package main

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
)

type cxThread struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Preview         string   `json:"preview"`
	Cwd             string   `json:"cwd"`
	Model           string   `json:"model"`
	ReasoningEffort string   `json:"reasoningEffort"`
	UpdatedAt       int64    `json:"updatedAt"`
	Status          cxStatus `json:"status"`
}

type cxStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags,omitempty"`
}

type cxTurn struct {
	ID          string   `json:"id"`
	Items       []cxItem `json:"items"`
	Status      string   `json:"status"`
	StartedAt   int64    `json:"startedAt"`
	CompletedAt int64    `json:"completedAt"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type cxItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	Content        json.RawMessage `json:"content"`
	Text           string          `json:"text"`
	Command        string          `json:"command"`
	CommandActions []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Name    string `json:"name"`
		Path    string `json:"path"`
		Query   string `json:"query"`
	} `json:"commandActions"`
	Changes []struct {
		Path string `json:"path"`
		Kind struct {
			Type string `json:"type"`
		} `json:"kind"`
	} `json:"changes"`
	Server      string          `json:"server"`
	Tool        string          `json:"tool"`
	Status      string          `json:"status"`
	StartedOnly bool            `json:"-"`
	WebResults  webResults      `json:"results,omitempty"`
	Arguments   json.RawMessage `json:"arguments"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
	Action struct {
		Type    string   `json:"type"`
		URL     string   `json:"url"`
		Pattern string   `json:"pattern"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
	} `json:"action"`
	Result *struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Query string `json:"query"`
}

type threadState struct {
	info                           cxThread
	turns                          []cxTurn
	live                           *cxTurn
	fetched                        int64
	fetchedAt                      int64
	stale                          bool
	activeTurn                     string
	since                          int64
	activity                       string
	fileChanges                    map[string]string
	origin                         string
	readAt                         int64
	err                            string
	failedQuestion                 *payloadTurn
	usage                          *contextUsage
	compactionTurn, compactionItem string
	serverNames                    map[string]string
}

type approval struct {
	id     string
	rpcID  json.RawMessage
	client *codexClient
	thread string
	kind   string
	text   string
	reason string
}

func stateFor(s cxStatus) string {
	switch s.Type {
	case "active":
		for _, flag := range s.ActiveFlags {
			if flag == "waitingOnApproval" || flag == "waitingOnUserInput" {
				return "needs_you"
			}
		}
		return "active"
	case "idle":
		return "idle"
	case "systemError":
		return "error"
	default:
		return "history"
	}
}

// The same as Codex's percent_of_context_window_remaining, with its BASELINE_TOKENS (prompts, tools and room to
// compact). Source: codex-rs/protocol/src/protocol.rs.
func contextLeft(used, window int64) int {
	const baseline = 12000
	if window <= baseline {
		return 0
	}
	effective := window - baseline
	remaining := max(effective-max(used-baseline, 0), 0)
	return int(math.Round(float64(remaining) / float64(effective) * 100))
}

// Keep a matched measurement. A partial update must not mix a new model's
// token count with a previous model's window.
type contextUsage struct {
	Used   int64
	Window int64
}

func (u contextUsage) valid() bool {
	return u.Used >= 0 && u.Window > 0
}

func (t *threadState) updateModel(model string) {
	if model == "" {
		return
	}
	if t.info.Model != "" && t.info.Model != model {
		t.usage = nil
	}
	t.info.Model = model
}

func threadTitle(t cxThread, turns []payloadTurn) string {
	title := strings.TrimSpace(t.Name)
	if title == "" {
		preview, _ := stripContext(t.Preview)
		title = oneLine(preview)
	}
	if i := slices.IndexFunc(turns, func(e payloadTurn) bool { return e.Role == "user" }); title == "" && i >= 0 {
		title = oneLine(turns[i].Text)
	}
	if title == "" {
		return "Untitled thread"
	}
	return trimText(title, 120)
}

func (t *threadState) payload(a *approval) payloadThread {
	turns := transcript(t.allTurns(), t.info.Cwd, t.serverNames)
	p := payloadThread{
		ID:         t.info.ID,
		Agent:      "codex",
		Title:      threadTitle(t.info, turns),
		State:      stateFor(t.info.Status),
		Project:    filepath.Base(t.info.Cwd),
		Cwd:        t.info.Cwd,
		Model:      t.info.Model,
		Effort:     t.info.ReasoningEffort,
		Compacting: t.compactionItem != "",
		Updated:    t.info.UpdatedAt,
		Unread:     t.info.UpdatedAt > t.readAt,
		Origin:     t.origin,
		Turns:      turns,
	}
	if u := t.usage; u != nil {
		left := contextLeft(u.Used, u.Window)
		p.ContextLeft, p.ContextUsed, p.ContextWindow = &left, &u.Used, &u.Window
	}
	if p.State == "idle" && t.live != nil && t.live.Status == "inProgress" {
		p.State = "active"
	}
	if a != nil {
		p.State = "needs_you"
		p.Approval = &payloadApproval{ID: a.id, Kind: a.kind, Text: a.text, Reason: a.reason}
	}
	if p.State == "active" || p.State == "needs_you" {
		p.Activity, p.Since = t.activity, t.since
	}
	switch {
	case t.err != "":
		p.State, p.Error = "error", t.err
		if t.failedQuestion != nil {
			p.Turns = append(p.Turns, *t.failedQuestion)
		}
	case p.State == "error":
		p.Error = "Codex reported an error in this thread."
	}
	return p
}

func (t *threadState) allTurns() []cxTurn {
	if t.live == nil {
		return t.turns
	}
	turns := slices.Clone(t.turns)
	for i, turn := range turns {
		if turn.ID == t.live.ID {
			// Polling can return the same number of items a moment before an
			// item/completed notification. Merge by ID so the HUD updates a
			// single running call instead of hiding its completion or its user.
			turn.Items = slices.Clone(turn.Items)
			for _, live := range t.live.Items {
				j := slices.IndexFunc(turn.Items, func(i cxItem) bool { return i.ID == live.ID })
				if j < 0 {
					turn.Items = append(turn.Items, live)
				} else if !live.StartedOnly || turn.Items[j].Status == "inProgress" {
					if live.Type == "webSearch" && turn.Items[j].Type == live.Type {
						live.WebResults = live.WebResults || turn.Items[j].WebResults
					}
					turn.Items[j] = live
				}
			}
			if t.live.Status != "inProgress" {
				turn.Status = t.live.Status
				turn.CompletedAt = t.live.CompletedAt
				turn.Error = t.live.Error
			}
			if turn.StartedAt == 0 {
				turn.StartedAt = t.live.StartedAt
			}
			turns[i] = turn
			return turns
		}
	}
	return append(turns, *t.live)
}

func (t *threadState) addLive(turnID string, it cxItem) {
	if t.live == nil || t.live.ID != turnID {
		t.live = &cxTurn{ID: turnID, Status: "inProgress"}
	}
	for i := range t.live.Items {
		if t.live.Items[i].ID == it.ID {
			t.live.Items[i] = it
			return
		}
	}
	t.live.Items = append(t.live.Items, it)
}

func (t *threadState) setTurns(turns []cxTurn) {
	t.turns = turns
	if t.compactionItem != "" {
		for _, turn := range turns {
			if turn.ID == t.compactionTurn && (turn.Status != "inProgress" || slices.ContainsFunc(turn.Items, func(it cxItem) bool {
				return it.Type == "contextCompaction" && it.ID == t.compactionItem
			})) {
				t.compactionTurn, t.compactionItem = "", ""
				break
			}
		}
	}
	if t.live == nil || t.live.Status == "inProgress" {
		return
	}
	for _, turn := range turns {
		if turn.ID != t.live.ID || turn.Status == "inProgress" || len(turn.Items) < len(t.live.Items) {
			continue
		}
		// A poll with plenty of other items can still lag the completed
		// compaction. Keep the live marker until the durable history has it.
		for _, item := range t.live.Items {
			if item.Type == "contextCompaction" && !slices.ContainsFunc(turn.Items, func(i cxItem) bool {
				return i.Type == item.Type && i.ID == item.ID
			}) {
				return
			}
		}
		t.live = nil
		return
	}
}

func transcript(turns []cxTurn, cwd string, serverNames map[string]string) []payloadTurn {
	var out []payloadTurn
	for _, turn := range turns {
		act := activity{serverNames: serverNames}
		var answer string
		var sources []string
		var events []turnEvent
		var gameCalls, webCalls int
		// A compact can run before the answer or after it. Preserve its
		// position relative to the last agent message displayed by this UI.
		lastAnswer := -1
		for i := len(turn.Items) - 1; i >= 0; i-- {
			if turn.Items[i].Type == "agentMessage" {
				lastAnswer = i
				break
			}
		}
		var afterAnswer []payloadTurn
		for i, it := range turn.Items {
			switch it.Type {
			case "userMessage":
				raw := userText(it.Content)
				text, hadContext := stripContext(raw)
				var summary string
				var refs []uiReference
				var card *journalCard
				var snapshotID string
				if ref := snapshotEnvelope.FindStringSubmatch(strings.TrimLeft(raw, " \t\r\n")); ref != nil {
					hud := hudForSnapshot(ref[1])
					summary, refs = hud.summary, hud.refs
					snapshotID, card = ref[1], hud.journal
					if card == nil && strings.Contains(ref[0], journalHint) {
						card = &journalCard{Title: text}
					}
				} else if hadContext {
					// Also display the state supplied inline by the original addon.
					state, _, _ := strings.Cut(raw, contextClose)
					if snapshot := parseGameSnapshot(state, turn.StartedAt); len(snapshot.Sections) > 0 {
						summary = snapshot.summary()
					}
				}
				out = append(out, payloadTurn{Role: "user", Text: text, At: turn.StartedAt, Context: hadContext, SnapshotSummary: summary, References: refs, Journal: card, SnapshotID: snapshotID})
			case "contextCompaction":
				// The history projection materializes only completed compactions.
				// A live item/started is tracked separately and never added here.
				marker := payloadTurn{Role: "compaction", Text: "Контекст сжат"}
				if lastAnswer >= 0 && i > lastAnswer {
					afterAnswer = append(afterAnswer, marker)
				} else {
					out = append(out, marker)
				}
			case "agentMessage":
				answer = it.Text
			default:
				act.add(it, cwd)
				if e, ok := gameActivityEvent(it); ok {
					if e.Kind == "web" {
						webCalls++
					} else {
						gameCalls++
					}
					if len(events) < 64 {
						events = append(events, e)
					}
					if source := eventSource(it, e); source != "" && !slices.Contains(sources, source) {
						sources = append(sources, source)
					}
				}
			}
		}
		if summary := act.summary(); summary != "" {
			if len(events) > 0 && act.reads+act.lists+act.searches+act.commands+act.edits == 0 {
				summary = activitySummary(events, gameCalls, webCalls)
				out = append(out, payloadTurn{Role: "activity", Text: summary, Events: events})
			} else {
				out = append(out, payloadTurn{Role: "activity", Text: summary, Items: act.details})
			}
		}
		if answer != "" {
			at := turn.CompletedAt
			if at == 0 {
				at = turn.StartedAt
			}
			if turn.Status == "completed" && !slices.Contains(sources, "Веб-поиск") {
				if webCalls > 0 {
					sources = append(sources, "Поиск без подтверждения")
				} else {
					sources = append(sources, "Без веб-поиска")
				}
			}
			out = append(out, payloadTurn{Role: "assistant", Text: answer, At: at, Source: strings.Join(sources, " · ")})
		}
		out = append(out, afterAnswer...)
		if turn.Status == "failed" && turn.Error != nil && turn.Error.Message != "" {
			out = append(out, payloadTurn{Role: "error", Text: turn.Error.Message, At: turn.CompletedAt})
		}
	}
	return out
}

func eventSource(it cxItem, e turnEvent) string {
	if it.Type == "webSearch" && e.Status == "ok" {
		return "Веб-поиск"
	}
	// Resolving a source revision gives the adviser an address. The subsequent page read supplies the evidence.
	if it.Server == "wow_game" && it.Tool != "get_client_api_source" && !memoryTool(it.Tool) && it.Status == "completed" && it.Result != nil && (e.Status == "ok" || e.Status == "partial") {
		return e.Label
	}
	return ""
}

func userText(content json.RawMessage) string {
	var inputs []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(content, &inputs)
	var parts []string
	for _, in := range inputs {
		if in.Type == "text" && in.Text != "" {
			parts = append(parts, in.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type activity struct {
	reads, lists, searches, commands, edits, tools, web int
	details                                             []string
	serverNames                                         map[string]string
}

func (a *activity) add(it cxItem, cwd string) {
	switch it.Type {
	case "commandExecution":
		if len(it.CommandActions) == 0 {
			a.commands++
			a.detail("Ran " + it.Command)
		}
		for _, c := range it.CommandActions {
			switch c.Type {
			case "read":
				a.reads++
				a.detail("Read " + relPath(firstOf(c.Path, c.Name), cwd))
			case "listFiles":
				a.lists++
				a.detail("Listed " + relPath(firstOf(c.Path, c.Command), cwd))
			case "search":
				a.searches++
				a.detail("Searched for " + firstOf(c.Query, c.Command))
			default:
				a.commands++
				a.detail("Ran " + firstOf(c.Command, it.Command))
			}
		}
	case "fileChange":
		for _, c := range it.Changes {
			a.edits++
			verb := map[string]string{"add": "Created", "delete": "Deleted"}[c.Kind.Type]
			if verb == "" {
				verb = "Edited"
			}
			a.detail(verb + " " + relPath(c.Path, cwd))
		}
	case "mcpToolCall":
		a.tools++
		line := toolLine(firstOf(a.serverNames[it.Server], it.Server), it.Tool)
		if it.Server == "wow_game" {
			var args activityArgs
			_ = json.Unmarshal(it.Arguments, &args)
			line = firstOf(args.label(it.Tool), line)
		}
		a.detail(line)
	case "dynamicToolCall":
		a.tools++
		a.detail("Used " + it.Tool)
	case "webSearch":
		a.web++
		a.detail("Searched the web for " + it.Query)
	}
}

func (a *activity) detail(line string) {
	line, _, _ = strings.Cut(strings.TrimSpace(line), "\n")
	if len(a.details) < maxDetailItems {
		a.details = append(a.details, trimText(line, 160))
	}
}

func (a *activity) summary() string {
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf(many, n))
		}
	}
	add(a.reads, "read 1 file", "read %d files")
	add(a.lists, "listed 1 folder", "listed %d folders")
	add(a.searches, "searched 1 time", "searched %d times")
	add(a.commands, "ran 1 command", "ran %d commands")
	add(a.edits, "changed 1 file", "changed %d files")
	add(a.tools, "used 1 tool", "used %d tools")
	add(a.web, "searched the web", "searched the web %d times")
	s := strings.Join(parts, ", ")
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func describe(it cxItem, serverNames map[string]string) string {
	switch it.Type {
	case "commandExecution":
		cmd, _, _ := strings.Cut(it.Command, "\n")
		return trimText("Running "+cmd, 80)
	case "fileChange":
		if len(it.Changes) > 0 {
			return "Editing " + filepath.Base(it.Changes[0].Path)
		}
		return "Editing files"
	case "mcpToolCall":
		if it.Server == "wow_game" {
			var a activityArgs
			_ = json.Unmarshal(it.Arguments, &a)
			if label := a.label(it.Tool); label != "" {
				return "Смотрю · " + label
			}
		}
		return toolLine(firstOf(serverNames[it.Server], it.Server), it.Tool)
	case "dynamicToolCall":
		return "Using " + it.Tool
	case "webSearch":
		return "Searching the web"
	case "reasoning":
		return "Thinking"
	case "agentMessage":
		return "Writing"
	}
	return ""
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func relPath(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return path
}
