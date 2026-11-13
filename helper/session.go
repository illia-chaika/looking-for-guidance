package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	offlineNote      = "Открой Looking for Guidance на Mac."
	restartNote      = "Перезапусти WoW, чтобы загрузить аддон."
	claudeSignInNote = "Sign in to the Claude Code CLI on this computer."
)

var codexNotes = map[string]string{
	"missing":    "Не удалось найти Codex CLI. Проверь путь в настройках Looking for Guidance.",
	"signed-out": "Войди в ChatGPT через Codex CLI в игровом профиле.",
	"error":      "Подключаюсь к Codex…",
}

var claudeNotes = map[string]string{
	"missing":    "Не удалось найти Claude Code. Установи его или выбери Codex в меню Looking for Guidance.",
	"signed-out": "Войди в Claude Code: выполни claude auth login в терминале.",
	"error":      "Подключаюсь к Claude Code…",
}

type session struct {
	activeCharacter, activeLogin string
	owners                       map[string]string
	memory                       *memoryView
	insights                     *insightEngine
	memoryStamp                  time.Time
	memoryExpiry                 int64
	ctx                          context.Context
	runID                        string
	wowDir                       string
	projectDir                   string
	onTick                       func()
	origins                      map[string]string
	findAgent                    func(name string) string
	provider                     string

	mu              sync.Mutex
	codex           string
	models          []payloadModel
	threads         map[string]*threadState
	order           []string
	approvals       []*approval
	failed          []payloadThread
	acks            map[string][]int64
	detailRequest   *detailRequest
	indexes         snapshotIndexes
	hello           time.Time
	codexErr        string
	writeErr        string
	watchErr        string
	denied          bool
	cancelOnDenial  context.CancelFunc
	listWanted      bool
	accountWanted   bool
	mcpWanted       map[string]bool
	serverNames     map[string]string
	claude          map[string]*claudeThread
	claudePath      string
	claudeInstalled bool
	claudeConfig    string
	claudeFile      string
	claudeChecked   bool
	listed          bool
	dirty           bool
	closed          bool

	writeMu sync.Mutex
	writer  *payloadWriter

	// Only the run loop uses these fields, so they need no lock.
	client          *codexClient
	queue           []queued
	lastList        time.Time
	lastDetailPulse time.Time
	lastAccount     time.Time
	nextStart       time.Time
	backoff         time.Duration
	nextClaude      time.Time
	claudeCheck     chan struct{}
}

type queued struct {
	packet
	received time.Time
}

type sessionView struct {
	provider        string
	codex           string
	claude          bool
	claudeInstalled bool
	claudeChecked   bool
	hello           time.Time
	threads         int
	problem         string
}

func newSession(wowDir string, origins map[string]string, onTick func()) *session {
	return &session{
		runID: fmt.Sprint(time.Now().UnixNano()),
		ctx:   context.Background(), wowDir: wowDir, origins: origins, onTick: onTick, findAgent: findInstalled, codex: "error",
		threads: map[string]*threadState{}, claude: map[string]*claudeThread{},
		acks: map[string][]int64{}, dirty: true,
		mcpWanted: map[string]bool{}, serverNames: map[string]string{},
	}
}

func (s *session) view() sessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sessionView{provider: s.provider, codex: s.codex, claude: s.claudePath != "", claudeInstalled: s.claudeInstalled, claudeChecked: s.claudeChecked, hello: s.hello, threads: len(s.order) + len(s.claude), problem: firstOf(s.watchErr, s.writeErr, s.codexErr)}
}

func (s *session) accessDenied() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.denied
}

// A refusal applies to both directions of the WoW bridge. A fresh session,
// started by the player after granting access, can try the volume again.
func (s *session) stopOnDenial(err error) bool {
	if !errors.Is(err, os.ErrPermission) {
		return false
	}
	s.mu.Lock()
	if s.denied {
		s.mu.Unlock()
		return true
	}
	s.denied = true
	s.watchErr = "Нет доступа к файлам WoW. Связь с игрой приостановлена."
	cancel := s.cancelOnDenial
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	logf("Paused game file access: %v", err)
	s.onTick()
	return true
}

func (s *session) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.ctx = ctx
	s.mu.Lock()
	s.cancelOnDenial = cancel
	s.mu.Unlock()
	if s.projectDir != "" {
		s.insights = newInsightEngine(filepath.Join(configDir(), "insights"), func() {
			s.mu.Lock()
			s.dirty = true
			s.mu.Unlock()
		}, func() (string, string) {
			s.mu.Lock()
			provider, claude := s.provider, s.claudePath
			s.mu.Unlock()
			if provider == "claude" {
				return provider, claude
			}
			return "codex", s.findAgent("codex")
		})
		s.insights.notes = bagNotes
		s.loadThreadOwners()
		s.pulseDetailBridge(snapshotDir(), time.Now())
		defer closeDetailBridge(snapshotDir(), s.runID)
	}
	raw, packets := make(chan packet, 64), make(chan packet, 64)
	s.writer = &payloadWriter{path: payloadPath(s.wowDir)}
	s.loadClaude(filepath.Join(configDir(), "claude-threads.json"))
	if !s.wantsClaude() {
		s.mu.Lock()
		s.claudeChecked = true
		s.mu.Unlock()
	}
	watched := make(chan struct{})
	go func() {
		if err := watchOutbox(ctx, filepath.Join(s.wowDir, "WTF", "Account"), raw); err != nil && !s.stopOnDenial(err) {
			s.mu.Lock()
			s.watchErr = "Couldn't watch WoW's files: " + oneLine(err.Error()) + ". Restart Looking for Guidance after checking folder access."
			s.mu.Unlock()
			logf("%s", s.view().problem)
			s.onTick()
		}
		close(watched)
	}()
	delivered := make(chan struct{})
	go func() {
		s.deliver(ctx, raw, packets)
		close(delivered)
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			<-watched
			<-delivered
			s.shutdown()
			return
		case p := <-packets:
			s.receive(p)
		case <-tick.C:
		}
		now := time.Now()
		s.superviseCodex(now)
		if s.wantsClaude() {
			s.superviseClaude(now)
		}
		s.drain(now)
		if s.insights != nil {
			ready := s.agentReady()
			s.insights.pump(&ready)
		}
		if s.projectDir != "" {
			s.pulseDetailBridge(snapshotDir(), now)
			s.syncDetailRequest(snapshotDir(), now)
			s.syncMemory(snapshotDir(), now, nil)
		}
		s.write(time.Now())
		s.refresh(now)
		s.write(now)
		s.onTick()
	}
}

// deliver runs apart from the frame reader, because the frame reader must never wait on a file write. The addon
// changes AddOns.txt every 40 ms, and a missed frame drops the packet.
func (s *session) deliver(ctx context.Context, raw <-chan packet, out chan<- packet) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-raw:
			// An older game client sends no runId. The local build binds a message
			// to our deduplication window, including retries after a helper restart.
			if p.RunID != "" && (p.RunID != s.runID || time.Since(time.Unix(int64(p.At), 0)) >= time.Hour) {
				continue
			}
			s.ack(p)
			s.write(time.Now())
			if p.Replay {
				continue
			}
			select {
			case out <- p:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *session) shutdown() {
	s.detachClaude()
	if s.client != nil {
		s.client.close()
		s.client = nil
		logf("Closed the Codex connection; Codex finishes on its own")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.accessDenied() {
		return
	}
	s.mu.Lock()
	p := payload{V: protocolVersion, Helper: version, Codex: "offline", Note: offlineNote, Acks: s.ackList()}
	s.mu.Unlock()
	if _, err := s.writer.write(p, time.Now()); err != nil && !s.stopOnDenial(err) {
		logf("Couldn't write the offline payload: %v", err)
	}
}

func (s *session) receive(p packet) {
	logf("Packet %s #%d (%s) from %s", p.Type, int64(p.Seq), p.Session, p.File)
	// Any packet shows the addon is running. The app can start after the addon's hello, which then reads as old.
	s.mu.Lock()
	s.hello = time.Now()
	s.mu.Unlock()
	if s.projectDir != "" && (p.Type == "memory" || p.Type == "create" || p.Type == "reply" || p.Type == "hello" || p.Type == "insight") {
		s.observeCharacter(snapshotDir(), p)
		if p.Type == "memory" {
			if validIdentity(p.Character, p.Login) {
				s.syncMemory(snapshotDir(), time.Now(), &p)
			}
			return
		}
	}
	if p.Type == "insight" {
		// Research runs beside the conversations and has its own cache, so it does not wait in the Codex queue.
		if s.insights != nil {
			s.insights.request(insightRequest{Kind: p.Kind, Key: p.Key, Locale: p.Locale, GameLocale: p.GameLocale,
				Character: s.activeCharacter, Force: p.Force, Facts: p.Facts})
		}
		return
	}
	if p.Type == "details" {
		if err := acceptGameDetails(snapshotDir(), s.runID, p, time.Now()); err != nil {
			logf("Game detail reply: %v", err)
		}
		return
	}
	s.queue = append(s.queue, queued{p, time.Now()})
}

func (s *session) drain(now time.Time) {
	pending := s.queue[:0]
	for _, q := range s.queue {
		if q.Type != "hello" && q.Type != "read" && !s.forClaude(q.packet) && !s.ready() {
			if now.Sub(q.received) < 10*time.Minute {
				pending = append(pending, q)
			} else {
				logf("Dropped %s #%d: Codex wasn't ready for ten minutes", q.Type, int64(q.Seq))
			}
			continue
		}
		s.handle(q.packet)
	}
	clear(s.queue[len(pending):])
	s.queue = pending
}

// Keep a list of each session's newest seqs, not only the highest. Then a packet lost between two delivered packets
// does not read as delivered.
func (s *session) ack(p packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, list := int64(p.Seq), s.acks[p.Session]
	if slices.Contains(list, seq) {
		return
	}
	list = append(list, seq)
	s.acks[p.Session] = list[max(0, len(list)-maxAcks):]
	s.dirty = true
	for len(s.acks) > 16 {
		delete(s.acks, slices.Min(slices.Collect(maps.Keys(s.acks))))
	}
}

func (s *session) ackList() map[string][]int64 {
	out := make(map[string][]int64, len(s.acks))
	for session, seqs := range s.acks {
		out[session] = slices.Clone(seqs)
	}
	return out
}

func (s *session) handle(p packet) {
	// A game packet can refer only to a conversation already known in the gaming project.
	if s.projectDir != "" && p.Type != "create" && p.Type != "hello" {
		s.mu.Lock()
		t, c := s.threads[p.Thread], s.claude[p.Thread]
		known := t != nil && t.info.Cwd == s.projectDir || c != nil && c.Cwd == s.projectDir
		allowed := known && (s.owners[p.Thread] == "" || s.owners[p.Thread] == s.activeCharacter)
		s.mu.Unlock()
		if !allowed {
			return
		}
	}
	if p.Type == "reply" && (p.Model != "" || p.Effort != "") {
		s.mu.Lock()
		t, c := s.threads[p.Thread], s.claude[p.Thread]
		offered := s.models
		if c != nil {
			offered = claudeGameModels()
		}
		allowed := (t != nil || c != nil) && strings.HasPrefix(p.Model, claudePrefix) == (c != nil) && slices.ContainsFunc(offered, func(m payloadModel) bool {
			return m.ID == p.Model && (p.Effort == "" || slices.Contains(m.Efforts, p.Effort))
		})
		switch {
		case allowed && c != nil:
			// A Claude Code chat takes the choice with its next message; a Codex chat passes it to turn/start.
			c.Model, c.Effort = p.Model, firstOf(p.Effort, c.Effort)
		case !allowed && t != nil:
			t.err = "Choose an available model and reasoning level."
			s.dirty = true
		case !allowed && c != nil:
			c.err = "Choose an available model and reasoning level."
			s.dirty = true
		}
		s.mu.Unlock()
		if !allowed {
			return
		}
	}
	if p.Type == "create" {
		if why := s.refuse(p); why != "" {
			logf("Didn't start the task from %s #%d: %s", p.Session, int64(p.Seq), why)
			s.failTask(p, agentFor(p.Model), "Looking for Guidance didn't start this task: "+why+".")
			return
		}
	}
	if s.forClaude(p) {
		s.handleClaude(p)
		return
	}
	switch p.Type {
	case "hello":
		logf("Addon %s is running", p.Addon)
	case "read":
		s.mu.Lock()
		if t := s.threads[p.Thread]; t != nil {
			t.readAt = t.info.UpdatedAt
			s.dirty = true
		}
		s.mu.Unlock()
	case "create":
		s.create(p)
	case "reply":
		s.reply(p)
	case "approve", "deny":
		s.answer(p, p.Type == "approve")
	case "stop":
		s.stopTurn(p)
	case "rename":
		s.renameThread(p)
	default:
		logf("Ignored unknown packet type %q", p.Type)
	}
}

// wantsClaude says whether Claude Code is worth watching: it is the chosen adviser, or older chats belong to it.
func (s *session) wantsClaude() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectDir == "" || s.provider == "claude" || len(s.claude) > 0
}

// The caller holds s.mu.
func (s *session) claudeState() string {
	switch {
	case s.claudePath != "":
		return "ready"
	case !s.claudeChecked:
		return "error"
	case s.claudeInstalled:
		return "signed-out"
	}
	return "missing"
}

// agentReady says whether the chosen adviser can take a journal research job.
func (s *session) agentReady() bool {
	s.mu.Lock()
	claude, path := s.projectDir != "" && s.provider == "claude", s.claudePath
	s.mu.Unlock()
	if claude {
		return path != ""
	}
	return s.ready()
}

// setProvider switches the adviser while the game runs: new chats and journal research go to it from now on.
func (s *session) setProvider(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != name {
		s.provider, s.nextClaude, s.dirty = name, time.Time{}, true
		s.claudeChecked = s.claudeChecked && name != "claude"
	}
}

func (s *session) claudeThreads() []*claudeThread {
	return slices.SortedFunc(maps.Values(s.claude), func(a, b *claudeThread) int { return cmp.Compare(b.Updated, a.Updated) })
}

func (s *session) offeredModels() []payloadModel {
	if s.projectDir != "" {
		// In the game one agent answers everything; new chats go to it. Older chats keep the agent they began with,
		// so its models stay listed, none of them as the default.
		var claude []payloadModel
		if s.claudePath != "" && (s.provider == "claude" || len(s.claude) > 0) {
			claude = claudeGameModels()
		}
		mine, other := s.models, claude
		if s.provider == "claude" {
			mine, other = claude, s.models
		}
		out := slices.Clone(mine)
		for _, m := range other {
			m.Default = false
			out = append(out, m)
		}
		return out
	}
	if s.claudePath == "" {
		return s.models
	}
	return append(slices.Clone(s.models), claudeModels(slices.ContainsFunc(s.models, func(m payloadModel) bool { return m.Default }))...)
}

func (s *session) offeredProjects() []payloadProject {
	if s.projectDir != "" {
		return []payloadProject{{Label: filepath.Base(s.projectDir), Cwd: s.projectDir}}
	}
	var projects []payloadProject
	seen := map[string]bool{}
	add := func(cwd string) {
		if cwd != "" && !seen[cwd] {
			seen[cwd] = true
			projects = append(projects, payloadProject{Label: filepath.Base(cwd), Cwd: cwd})
		}
	}
	for _, id := range s.order {
		add(s.threads[id].info.Cwd)
	}
	for _, t := range s.claudeThreads() {
		add(t.Cwd)
	}
	if home, err := os.UserHomeDir(); err == nil && s.claudePath != "" && len(projects) == 0 {
		add(home)
	}
	return projects
}

func (s *session) payload() payload {
	p := payload{V: protocolVersion, Helper: version, Codex: s.codex, Note: codexNotes[s.codex], Acks: s.ackList(), Models: s.offeredModels(), Projects: s.offeredProjects()}
	byClaude := s.projectDir != "" && s.provider == "claude"
	if s.projectDir != "" {
		p.Agent = firstOf(s.provider, "codex")
	}
	if byClaude {
		// The add-on reads this field as "is the adviser ready"; with Claude Code chosen it speaks for Claude Code.
		p.Codex = s.claudeState()
		p.Note = claudeNotes[p.Codex]
	}
	p.RunID = s.runID
	p.DetailRequest = s.detailRequest
	p.Memory = s.memory
	p.Character = s.activeCharacter
	if s.insights != nil {
		p.Insights = s.insights.view()
	}
	p.SnapshotBases = s.indexes.confirmed()
	p.Starting = !s.claudeChecked || !s.listed && s.codex != "missing" && s.codex != "signed-out" && s.codexErr == ""
	if byClaude {
		p.Starting = !s.claudeChecked
	} else if s.projectDir != "" {
	} else if s.claudePath != "" {
		if s.codex == "missing" {
			p.Note = ""
		}
	} else if s.codex == "missing" && s.claudeInstalled {
		p.Note = claudeSignInNote
	}
	pending := map[string]*approval{}
	for _, a := range slices.Backward(s.approvals) {
		pending[a.thread] = a
	}
	var threads []payloadThread
	for _, id := range s.order {
		if owner := s.owners[id]; owner != "" && owner != s.activeCharacter {
			continue
		}
		threads = append(threads, s.threads[id].payload(pending[id]))
	}
	for _, t := range s.claudeThreads() {
		if owner := s.owners[t.ID]; owner != "" && owner != s.activeCharacter {
			continue
		}
		threads = append(threads, t.payload())
	}
	slices.SortStableFunc(threads, func(a, b payloadThread) int { return cmp.Compare(b.Updated, a.Updated) })
	p.Threads = append(slices.Clone(s.failed), threads...)
	return p
}

// Write the file under writeMu, outside s.mu. Then the acker and the loop do not hold the shared state while the disk
// or a Windows reader is slow.
func (s *session) write(now time.Time) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if !s.dirty || s.closed || s.denied {
		s.mu.Unlock()
		return
	}
	p := s.payload()
	s.dirty = false
	s.mu.Unlock()
	_, err := s.writer.write(p, now)
	if s.stopOnDenial(err) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.writeErr = ""
		return
	}
	s.dirty = true
	msg := "Couldn't write the payload: " + err.Error()
	if msg != s.writeErr {
		logf("%s", msg)
	}
	s.writeErr = msg
}

// The addon adopts the failed thread by its origin.
func (s *session) failTask(p packet, agent, why string) {
	origin := fmt.Sprintf("%s:%d", p.Session, int64(p.Seq))
	title, _, _ := strings.Cut(strings.TrimSpace(p.Text), "\n")
	failed := payloadThread{
		ID: origin, Agent: agent, Origin: origin, Title: trimText(title, 120), State: "error",
		Project: filepath.Base(p.Cwd), Cwd: p.Cwd, Model: p.Model, Effort: p.Effort,
		Updated: time.Now().Unix(), Error: oneLine(why),
		Turns: []payloadTurn{failedQuestion(p, "")},
	}
	s.mu.Lock()
	s.failed = append([]payloadThread{failed}, s.failed[:min(len(s.failed), 4)]...)
	s.dirty = true
	s.mu.Unlock()
}

func agentFor(model string) string {
	if strings.HasPrefix(model, claudePrefix) {
		return "claude"
	}
	return "codex"
}

// Anything that runs in WoW can write a packet. So start a task only in a folder, with a model and an effort, that
// the app offered.
func (s *session) refuse(p packet) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.offeredProjects(), func(project payloadProject) bool { return project.Cwd == p.Cwd }) {
		return "its folder isn't one the app offered"
	}
	models := s.offeredModels()
	i := slices.IndexFunc(models, func(m payloadModel) bool { return m.ID == p.Model })
	if i < 0 {
		return "its model isn't one the app offered"
	}
	if s.projectDir != "" && agentFor(p.Model) != firstOf(s.provider, "codex") {
		return "its model belongs to the adviser that is not chosen"
	}
	if p.Effort != "" && !slices.Contains(models[i].Efforts, p.Effort) {
		return "its effort isn't one the app offered"
	}
	return ""
}

func oneLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return trimText(line, 200)
}
