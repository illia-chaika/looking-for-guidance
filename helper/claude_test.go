package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type claudeHarness struct {
	t    *testing.T
	s    *session
	log  string
	file string
	seq  int
}

func newClaudeHarness(t *testing.T, dir string) *claudeHarness {
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := &claudeHarness{t: t, s: newSession("", map[string]string{}, func() {}), log: filepath.Join(dir, "claude.log"), file: filepath.Join(dir, "claude-threads.json")}
	t.Setenv("CLAUDE_LOG", h.log)
	t.Setenv("LOOKING_FOR_GUIDANCE_TEST_MODE", "claude")
	fixtures, err := filepath.Abs("testdata/claude")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOKING_FOR_GUIDANCE_TESTDATA", fixtures)
	h.s.claudePath = fake
	h.s.loadClaude(h.file)
	t.Cleanup(h.s.detachClaude)
	return h
}

func (h *claudeHarness) send(p packet) {
	h.seq++
	p.Session, p.Seq, p.At = "s1", float64(h.seq), float64(time.Now().Unix())
	h.s.receive(p)
	h.s.drain(time.Now())
}

func (h *claudeHarness) payload() payload {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.payload()
}

func (h *claudeHarness) wait(what string, ok func(payloadThread) bool) payloadThread {
	h.t.Helper()
	var th payloadThread
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		threads := h.payload().Threads
		if len(threads) == 0 {
			continue
		}
		if th = threads[0]; ok(th) {
			return th
		}
	}
	h.t.Fatalf("%s: got %+v", what, th)
	return th
}

// waitFor is wait for one chat among several.
func (h *claudeHarness) waitFor(what, id string, ok func(payloadThread) bool) payloadThread {
	h.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, th := range h.payload().Threads {
			if th.ID == id && ok(th) {
				return th
			}
		}
	}
	h.t.Fatalf("timed out waiting for %s:\n%s", what, h.logged())
	return payloadThread{}
}

func (h *claudeHarness) exited() bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	for _, t := range h.s.claude {
		if t.run != nil {
			return false
		}
	}
	return true
}

func (h *claudeHarness) idle(what string) payloadThread {
	h.t.Helper()
	return h.wait(what, func(th payloadThread) bool { return th.State == "idle" && h.exited() })
}

func (h *claudeHarness) logged() string {
	data, _ := os.ReadFile(h.log)
	return string(data)
}

func TestClaudeConversationFromTheGame(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	h := newClaudeHarness(t, dir)

	models := h.payload().Models
	if len(models) != 2 || models[0].ID != "claude:opus" || !models[0].Default || models[1].ID != "claude:sonnet" || models[1].Effort != "high" || len(models[1].Efforts) != 5 {
		t.Fatalf("models with Codex missing: %+v", models)
	}
	h.s.codex = "missing"
	if p := h.payload(); p.Note != "" || len(p.Projects) != 1 {
		t.Fatalf("Codex missing but Claude works: note %q, projects %+v", p.Note, p.Projects)
	}

	h.send(packet{Type: "create", Text: "What should I train?\nThanks", Cwd: work, Model: "claude:opus", Effort: "high", ContextText: "Character: level 12 Night Elf Druid"})
	th := h.idle("first reply")
	if th.Agent != "claude" || th.Title != "What should I train?" || th.Model != "claude:opus" || th.Origin != "s1:1" || th.Cwd != work || th.Project != filepath.Base(work) {
		t.Fatalf("new thread: %+v", th)
	}
	if len(th.Turns) != 3 || th.Turns[0].Text != "What should I train?\nThanks" || !th.Turns[0].Context || th.Turns[1].Text != "Ran 1 command" || th.Turns[2].Role != "assistant" || th.Turns[2].Text != "done" {
		t.Fatalf("first turns: %+v", th.Turns)
	}
	if th.ContextLeft == nil || *th.ContextLeft != 96 {
		t.Fatalf("context left: %v", th.ContextLeft)
	}
	log := h.logged()
	for _, want := range []string{"start " + work + " -p ", "--model opus", "--effort high", "--permission-prompt-tool stdio", "--session-id " + th.ID, "wow_character_state captured=", "Character: level 12 Night Elf Druid", "What should I train?"} {
		if !strings.Contains(log, want) {
			t.Fatalf("claude was not given %q:\n%s", want, log)
		}
	}
	id := th.ID

	h.send(packet{Type: "reply", Thread: id, Text: "Write my notes"})
	th = h.wait("write approval", func(th payloadThread) bool { return th.State == "needs_you" })
	if a := th.Approval; a == nil || a.Kind != "file" || a.Text != "notes.txt" || a.ID != "a9c35e60-b2f8-4b21-a1c2-6b3fd0aa985b" {
		t.Fatalf("file approval: %+v", th.Approval)
	}
	if !strings.Contains(h.logged(), "--resume "+id) {
		t.Fatal("a reply didn't resume the session")
	}
	h.send(packet{Type: "deny", Thread: id, Approval: th.Approval.ID})
	th = h.idle("after deny")
	if !strings.Contains(h.logged(), `"behavior":"deny"`) || th.Turns[len(th.Turns)-2].Text != "Write my notes" || th.Turns[len(th.Turns)-1].Text != "I couldn't create notes.txt because the Write call was denied, so no file was written." {
		t.Fatalf("deny: %+v\n%s", th.Turns, h.logged())
	}

	h.send(packet{Type: "reply", Thread: id, Text: "Run ping"})
	th = h.wait("command approval", func(th payloadThread) bool { return th.State == "needs_you" })
	if a := th.Approval; a == nil || a.Kind != "command" || a.Text != "ping -c 40 127.0.0.1" || a.Reason != "Ping localhost 40 times" {
		t.Fatalf("command approval: %+v", th.Approval)
	}
	h.send(packet{Type: "approve", Thread: id, Approval: th.Approval.ID})
	th = h.wait("allowed", func(th payloadThread) bool {
		return th.State == "active" && th.Approval == nil && strings.Contains(h.logged(), `"behavior":"allow","updatedInput":{"command":"ping -c 40 127.0.0.1"`)
	})
	if th.Activity != "Running ping -c 40 127.0.0.1" || th.Since == 0 {
		t.Fatalf("allow: %+v", th)
	}
	h.send(packet{Type: "reply", Thread: id, Text: "And after that?"})
	h.send(packet{Type: "stop", Thread: id})
	th = h.idle("after stop and the queued reply")
	if th.Error != "" || th.Turns[len(th.Turns)-3].Text != "And after that?" || th.Turns[len(th.Turns)-1].Text != "done" {
		t.Fatalf("stop then queued reply: %+v", th)
	}
	if strings.Count(h.logged(), "start "+work+" ") != 3 {
		t.Fatalf("the queued reply should reuse the running process:\n%s", h.logged())
	}

	h.send(packet{Type: "reply", Thread: id, Text: "crash"})
	th = h.wait("crash", func(th payloadThread) bool { return th.State == "error" })
	if th.Error != "Claude stopped: exit status 3: boom" {
		t.Fatalf("crash: %q", th.Error)
	}

	h.send(packet{Type: "read", Thread: id})
	again := newClaudeHarness(t, dir)
	again.s.codex = "missing"
	th = again.payload().Threads[0]
	if th.ID != id || th.State != "idle" || th.Unread || len(th.Turns) != 10 || th.ContextLeft == nil {
		t.Fatalf("after a helper restart: %+v", th)
	}
	again.send(packet{Type: "reply", Thread: id, Text: "Still there?"})
	again.idle("after restart")
	if !strings.Contains(again.logged(), "--resume "+id) {
		t.Fatal("a restarted helper didn't resume the session")
	}
}

// The game stream is shaped after a real session recorded on 13 November 2026 (thinking blocks, results of the game
// data server as text blocks without is_error, a rate-limit event); its contents are made up. The journal and
// signed-out streams follow the documented result message and the CLI's own sign-in error.
func TestClaudeAnswersAGameplayQuestion(t *testing.T) {
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	h := newClaudeHarness(t, t.TempDir())
	game := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(game); err == nil {
		game = resolved
	}
	h.s.projectDir, h.s.provider, h.s.codex = game, "claude", "missing"

	p := h.payload()
	if len(p.Models) != 2 || p.Models[0].ID != "claude:opus" || !p.Models[0].Default || p.Models[0].Effort != "medium" || p.Models[1].ID != "claude:sonnet" || p.Models[1].Default || p.Codex != "ready" || p.Note != "" || p.Agent != "claude" {
		t.Fatalf("Claude Code is the adviser, signed in: codex=%q note=%q models=%+v", p.Codex, p.Note, p.Models)
	}

	h.send(packet{Type: "create", Text: "Which talents next?", Cwd: game, Model: "claude:sonnet", Effort: "medium",
		Character: "Player-1-ABC", Login: "s123-abc", ContextText: "Character: level 12 Undead Warlock"})
	th := h.idle("the adviser's answer")
	if th.Agent != "claude" || th.Cwd != game || th.Title != "Which talents next?" {
		t.Fatalf("new chat: %+v", th)
	}
	if len(th.Turns) != 3 || th.Turns[0].Role != "user" || th.Turns[0].Text != "Which talents next?" || th.Turns[0].SnapshotID == "" {
		t.Fatalf("the question keeps its snapshot: %+v", th.Turns)
	}
	checked, answer := th.Turns[1], th.Turns[2]
	if checked.Role != "activity" || len(checked.Events) != 3 || checked.Events[0].Label != "Экипировка" || checked.Events[0].Status != "ok" ||
		checked.Events[1].Kind != "web" || checked.Events[1].Detail != "WoW Forever warlock talents" || checked.Events[2].URL != "https://worldofwarcraft.blizzard.com/news/123" {
		t.Fatalf("what the adviser checked: %+v", checked)
	}
	if answer.Role != "assistant" || answer.Text != "Take Improved Corruption next." || answer.Source != "Экипировка · Веб-поиск" {
		t.Fatalf("the answer and its sources: %+v", answer)
	}
	if shown, _ := json.Marshal(th); strings.Contains(string(shown), "dont_display_me") || strings.Contains(string(shown), "long-private-inventory") {
		t.Fatal("tool arguments or results leaked into the game")
	}
	if h.s.owners[th.ID] != "Player-1-ABC" {
		t.Fatalf("the chat belongs to the character who asked: %v", h.s.owners)
	}
	// What Claude Code said about the allowance during the turn reaches the game: 30% and 17% used.
	if want := []payloadLimit{{Mins: 300, Left: 70, Resets: 1794578400}, {Mins: 10080, Left: 83, Resets: 1794830400}}; !slices.Equal(h.payload().Limits, want) {
		t.Fatalf("the allowance shown in the game: %+v", h.payload().Limits)
	}

	log := h.logged()
	for _, want := range []string{"start " + game + " -p ", "--model sonnet", "--effort medium", "--setting-sources  --strict-mcp-config",
		`--game-mcp`, "--tools WebSearch,WebFetch", "mcp__wow_game__*", "--permission-mode dontAsk", "--append-system-prompt ",
		"--session-id " + th.ID, `wow_snapshot id=`, "Which talents next?"} {
		if !strings.Contains(log, want) {
			t.Fatalf("Claude Code was not given %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "--permission-prompt-tool") {
		t.Fatal("the adviser must not be able to ask for approvals")
	}

	// The chat continues with the model chosen in its footer, and stays with Claude Code after the player picks Codex.
	h.s.setProvider("")
	if p := h.payload(); len(p.Limits) != 0 {
		t.Fatalf("Claude Code's allowance is shown for Codex: %+v", p.Limits)
	}
	if p := h.payload(); p.Agent != "codex" || len(p.Models) != 2 || p.Models[0].Default || p.Models[1].Default {
		t.Fatalf("Codex is the adviser again; Claude's models stay for its chat, none as the default: %s %+v", p.Agent, p.Models)
	}
	h.send(packet{Type: "create", Text: "A new chat about talents", Cwd: game, Model: "claude:sonnet", Effort: "medium",
		Character: "Player-1-ABC", Login: "s123-abc"})
	if failed := h.payload().Threads[0]; failed.State != "error" || !strings.Contains(failed.Error, "not chosen") {
		t.Fatalf("a new chat must go to the chosen adviser: %+v", failed)
	}
	h.send(packet{Type: "reply", Thread: th.ID, Text: "And the talents after that?", Model: "claude:opus", Effort: "high",
		Character: "Player-1-ABC", Login: "s123-abc"})
	th = h.waitFor("the second answer", th.ID, func(th payloadThread) bool { return th.State == "idle" && len(th.Turns) == 6 })
	if th.Model != "claude:opus" || !strings.Contains(h.logged(), "--model opus --setting-sources") || !strings.Contains(h.logged(), "--effort high --resume "+th.ID) {
		t.Fatalf("the continued chat: model %s\n%s", th.Model, h.logged())
	}

	// Another character does not see the chat.
	h.s.mu.Lock()
	h.s.activeCharacter = "Player-1-DEF"
	h.s.mu.Unlock()
	for _, shown := range h.payload().Threads {
		if shown.ID == th.ID {
			t.Fatalf("a chat shown to another character: %+v", shown)
		}
	}
}

func TestJournalResearchThroughClaudeCode(t *testing.T) {
	work := t.TempDir()
	t.Setenv("HOME", work)
	h := newClaudeHarness(t, t.TempDir())
	schema, out := filepath.Join(work, "digest.schema.json"), filepath.Join(work, "answer.out")
	os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job := &insightJob{kind: &insightKind{search: true, effort: "low"}}
	var limits []payloadLimit
	if err := execClaude(ctx, h.s.claudePath, job, "journal-probe", schema, out, func(l []payloadLimit) { limits = l }); err != nil {
		t.Fatal(err)
	}
	if want := []payloadLimit{{Mins: 300, Left: 58, Resets: 1794578400}, {Mins: 10080, Left: 80, Resets: 1794830400}}; !slices.Equal(limits, want) {
		t.Fatalf("the allowance a journal job reports: %+v", limits)
	}
	if answer, _ := os.ReadFile(out); string(answer) != `{"headline":"Nothing new for you","items":[]}` {
		t.Fatalf("the structured answer: %s", answer)
	}
	log := h.logged()
	for _, want := range []string{"--model opus --effort low", "--setting-sources  --strict-mcp-config", "--tools WebSearch,WebFetch",
		"--permission-mode dontAsk", "--no-session-persistence", `--json-schema {"type":"object"}`} {
		if !strings.Contains(log, want) {
			t.Fatalf("Claude Code was not given %q:\n%s", want, log)
		}
	}

	// A job that needs no search gets no tools at all.
	job.kind = &insightKind{effort: "low"}
	if err := execClaude(ctx, h.s.claudePath, job, "journal-probe", schema, out, nil); err != nil || !strings.Contains(h.logged(), "--tools  --allowedTools  --permission-mode") {
		t.Fatalf("no tools without search: %v\n%s", err, h.logged())
	}

	os.Remove(out)
	err := execClaude(ctx, h.s.claudePath, job, "expired-probe", schema, out, nil)
	if err == nil || !strings.Contains(err.Error(), "OAuth session expired") {
		t.Fatalf("an expired sign-in must be reported by its message: %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("a failed turn left an answer")
	}
}
