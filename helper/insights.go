package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Journal cards ask for advice that needs research: which hotfixes concern this character, whether a quest is
// bugged, what a dungeon needs. Each request becomes one hidden, non-interactive Codex run with a fixed model,
// a JSON answer checked here, and a cache on disk. Nothing of it appears among the player's conversations.
const (
	insightModel      = "gpt-6.1-sol"
	insightClaude     = "opus"
	insightTimeout    = 5 * time.Minute
	insightRetryAfter = 10 * time.Minute
	insightDailyCap   = 60  // when the game names no cap of its own
	insightCapLimit   = 200 // the most a setting in the game can allow
	insightKeep       = 30 * 24 * time.Hour
	insightParallel   = 2
	maxInsightFacts   = 8 << 10
	maxInsightData    = 8 << 10
	maxInsightsShown  = 16
)

type insight struct {
	Kind  string          `json:"kind"`
	Key   string          `json:"key"`
	State string          `json:"state"` // pending, ready or failed
	At    int64           `json:"at,omitempty"`
	Since int64           `json:"since,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Note  string          `json:"note,omitempty"`
}

type insightRequest struct {
	Kind, Key, Locale, GameLocale, Character string
	Force                                    bool
	// Cap is the player's own daily limit of research runs, sent with every request from the game's settings.
	Cap    int
	Facts  json.RawMessage
	Market *marketFacts
}

// insightEnv is what a job may use besides the add-on's facts.
type insightEnv struct {
	Feed    *gameFeed
	Notes   []string
	Extra   string       // text of further pages the helper fetched itself
	Auction *auctionBook // the character's auction house, when the player set up auction prices
}

type insightKind struct {
	version      string
	effort       string
	search       bool
	ttl          time.Duration
	feed         bool // the answer depends on the current hotfix list
	issues       bool // the job also reads Blizzard's known-issues thread
	notes        bool // the answer depends on what the player asked the advisor to remember
	auction      bool // the job reads the character's auction house
	perCharacter bool
	schema       string
	prompt       func(insightRequest, insightEnv) (string, error)
	check        func(raw []byte, req insightRequest, env insightEnv) (any, error)
	// decorate adds current facts to a ready answer each time it is shown, cached or fresh.
	decorate func(data json.RawMessage, req insightRequest, env insightEnv) json.RawMessage
}

type insightRecord struct {
	State string          `json:"state"`
	At    int64           `json:"at"`
	Basis string          `json:"basis,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Note  string          `json:"note,omitempty"`
}

type insightJob struct {
	id   string
	req  insightRequest
	kind *insightKind
	env  insightEnv
}

type insightEngine struct {
	dir     string
	feedURL string
	notify  func()
	// agent names the adviser that runs research, "codex" or "claude", and the path of its CLI.
	agent func() (provider, path string)
	fetch func(context.Context, string) (string, error)
	notes func(character string) []string
	run   func(ctx context.Context, job *insightJob, prompt, schemaPath, outPath string) error
	// limits receives what Claude Code says about the allowance while it runs a job.
	limits func([]payloadLimit)
	now    func() time.Time

	mu        sync.Mutex
	character string
	shown     []insight
	active    map[string]bool
	queue     []*insightJob
	running   int
	ready     bool
}

func newInsightEngine(dir string, notify func(), agent func() (provider, path string)) *insightEngine {
	e := &insightEngine{dir: dir, feedURL: defaultHotfixURL, notify: notify, agent: agent, fetch: fetchPage, now: time.Now,
		active: map[string]bool{}}
	if url := safeWebURL(loadSettings().HotfixURL); url != "" {
		e.feedURL = url
	}
	e.run = e.exec
	e.prune()
	return e
}

// Keys may carry localized dungeon or item names, never line breaks or path characters.
var insightKeyPattern = regexp.MustCompile(`^[\p{L}\p{N} _:.,'’()-]{1,120}$`)

func (e *insightEngine) id(k *insightKind, req insightRequest) string {
	parts := []string{k.version, req.Kind, req.Key, req.Locale, req.GameLocale}
	if k.perCharacter {
		parts = append(parts, req.Character)
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:10])
}

func (e *insightEngine) recordPath(req insightRequest, id string) string {
	return filepath.Join(e.dir, req.Kind, id+".json")
}

func (e *insightEngine) outPath(id string) string { return filepath.Join(e.dir, "work", id+".out") }

// request never blocks: the session loop calls it for every insight packet.
func (e *insightEngine) request(req insightRequest) {
	kind := insightKinds[req.Kind]
	if kind == nil || !insightKeyPattern.MatchString(req.Key) || len(req.Facts) > maxInsightFacts || (kind.perCharacter && req.Character == "") {
		logf("Ignored a journal request of kind %q", req.Kind)
		return
	}
	id := e.id(kind, req)
	e.mu.Lock()
	if req.Character != e.character {
		e.character, e.shown = req.Character, nil
	}
	if e.active[id] {
		e.mu.Unlock()
		return
	}
	e.active[id] = true
	e.showLocked(insight{Kind: req.Kind, Key: req.Key, State: "pending", Since: e.now().Unix()})
	e.mu.Unlock()
	e.notify()
	go e.resolve(&insightJob{id: id, req: req, kind: kind})
}

func (e *insightEngine) showLocked(in insight) {
	kept := []insight{in}
	for _, old := range e.shown {
		if old.Kind == in.Kind && old.Key == in.Key {
			if in.State == "pending" && in.Since == 0 {
				kept[0].Since = old.Since
			}
			continue
		}
		kept = append(kept, old)
	}
	if len(kept) > maxInsightsShown {
		kept = kept[:maxInsightsShown]
	}
	e.shown = kept
}

func (e *insightEngine) view() []insight {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]insight(nil), e.shown...)
}

// finish publishes a result for the character who asked. A result that arrives after a character change stays on disk only.
func (e *insightEngine) finish(job *insightJob, rec insightRecord) {
	if rec.State == "ready" && job.kind.decorate != nil {
		if data := job.kind.decorate(rec.Data, job.req, job.env); len(data) <= maxInsightData {
			rec.Data = data
		}
	}
	e.mu.Lock()
	delete(e.active, job.id)
	if job.req.Character == e.character {
		e.showLocked(insight{Kind: job.req.Kind, Key: job.req.Key, State: rec.State, At: rec.At, Data: rec.Data, Note: rec.Note})
	}
	e.mu.Unlock()
	e.notify()
}

// basis names what an answer was built on besides the request. A cached answer is reused only on the same basis.
func (e *insightEngine) basis(job *insightJob) string {
	basis := ""
	if job.env.Feed != nil {
		basis = job.env.Feed.Hash
	}
	if job.kind.notes && len(job.env.Notes) > 0 {
		sum := sha1.Sum([]byte(strings.Join(job.env.Notes, "\n")))
		basis += "+" + hex.EncodeToString(sum[:6])
	}
	return basis
}

// resolve answers from the cache, adopts a result left by an earlier run of the helper, or queues a Codex run.
func (e *insightEngine) resolve(job *insightJob) {
	if job.kind.feed {
		feed, err := loadFeed(context.Background(), e.dir, e.feedURL, e.now(), e.fetch)
		if err != nil {
			e.finish(job, insightRecord{State: "failed", At: e.now().Unix(), Note: "hotfix list unavailable: " + oneLine(err.Error())})
			return
		}
		job.env.Feed = feed
	}
	if job.kind.issues {
		job.env.Extra = loadIssues(context.Background(), e.dir, defaultIssuesURL, e.now(), e.fetch)
	}
	if e.notes != nil && job.kind.notes {
		job.env.Notes = e.notes(job.req.Character)
	}
	if job.kind.auction && job.req.Market != nil {
		if m, err := job.req.Market.market(); err == nil {
			job.env.Auction, _ = loadAuctionBook(filepath.Join(filepath.Dir(e.dir), "auctions", m.key()+".json"))
		}
	}
	now := e.now()
	var rec insightRecord
	if data, err := os.ReadFile(e.recordPath(job.req, job.id)); err == nil && json.Unmarshal(data, &rec) == nil && !job.req.Force && rec.Basis == e.basis(job) {
		age := now.Sub(time.Unix(rec.At, 0))
		if (rec.State == "ready" && age < job.kind.ttl) || (rec.State == "failed" && age < insightRetryAfter) {
			e.finish(job, rec)
			return
		}
	}
	if raw, err := os.ReadFile(e.outPath(job.id)); err == nil && !job.req.Force {
		os.Remove(e.outPath(job.id))
		if rec, err := e.accept(job, raw); err == nil {
			logf("Journal %s: adopted a result finished while the helper was away", job.req.Kind)
			e.finish(job, rec)
			return
		}
	}
	e.mu.Lock()
	e.queue = append(e.queue, job)
	e.mu.Unlock()
	e.pump(nil)
}

// pump starts queued runs while Codex is signed in. The session loop reports readiness; a finished run passes nil.
func (e *insightEngine) pump(ready *bool) {
	e.mu.Lock()
	if ready != nil {
		e.ready = *ready
	}
	var start []*insightJob
	for e.ready && e.running < insightParallel && len(e.queue) > 0 {
		start = append(start, e.queue[0])
		e.queue = e.queue[1:]
		e.running++
	}
	e.mu.Unlock()
	for _, job := range start {
		go e.execute(job)
	}
}

func (e *insightEngine) accept(job *insightJob, raw []byte) (insightRecord, error) {
	value, err := job.kind.check(stripFence(raw), job.req, job.env)
	if err != nil {
		return insightRecord{}, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return insightRecord{}, err
	}
	if len(data) > maxInsightData {
		return insightRecord{}, fmt.Errorf("answer of %d bytes is too large for the game", len(data))
	}
	rec := insightRecord{State: "ready", At: e.now().Unix(), Basis: e.basis(job), Data: data}
	return rec, e.store(job, rec)
}

func (e *insightEngine) store(job *insightJob, rec insightRecord) error {
	path := e.recordPath(job.req, job.id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(rec)
	return writeFileAtomic(path, data)
}

func (e *insightEngine) execute(job *insightJob) {
	rec, err := e.research(job)
	if err != nil {
		logf("Journal %s failed: %v", job.req.Kind, err)
		rec = insightRecord{State: "failed", At: e.now().Unix(), Basis: e.basis(job), Note: oneLine(err.Error())}
		e.store(job, rec)
	}
	e.mu.Lock()
	e.running--
	e.mu.Unlock()
	e.finish(job, rec)
	e.pump(nil)
}

func (e *insightEngine) research(job *insightJob) (insightRecord, error) {
	if !e.spend(job.req.Cap) {
		return insightRecord{}, errors.New("daily limit of background checks reached")
	}
	prompt, err := job.kind.prompt(job.req, job.env)
	if err != nil {
		return insightRecord{}, err
	}
	work := filepath.Join(e.dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return insightRecord{}, err
	}
	schemaPath := filepath.Join(work, job.req.Kind+".schema.json")
	if err := os.WriteFile(schemaPath, []byte(job.kind.schema), 0o600); err != nil {
		return insightRecord{}, err
	}
	out := e.outPath(job.id)
	started := e.now()
	var last error
	// The schema steers the model; the check below decides. One more try covers a malformed or unsupported answer.
	for attempt := 1; attempt <= 2; attempt++ {
		os.Remove(out)
		ctx, cancel := context.WithTimeout(context.Background(), insightTimeout)
		err := e.run(ctx, job, prompt, schemaPath, out)
		cancel()
		if err != nil {
			return insightRecord{}, err
		}
		raw, err := os.ReadFile(out)
		os.Remove(out)
		if err != nil {
			return insightRecord{}, errors.New("the adviser returned no answer")
		}
		rec, err := e.accept(job, raw)
		if err == nil {
			logf("Journal %s ready in %s (attempt %d)", job.req.Kind, e.now().Sub(started).Round(time.Second), attempt)
			return rec, nil
		}
		last = err
	}
	return insightRecord{}, fmt.Errorf("answer rejected: %w", last)
}

// exec hands the job to the adviser the player chose. Both leave the answer as JSON in outPath.
func (e *insightEngine) exec(ctx context.Context, job *insightJob, prompt, schemaPath, outPath string) error {
	provider, path := e.agent()
	if provider == "claude" {
		if path == "" {
			return errors.New("Claude Code is not signed in")
		}
		return execClaude(ctx, path, job, prompt, schemaPath, outPath, e.limits)
	}
	if path == "" {
		return errors.New("Codex CLI not found")
	}
	return execCodex(ctx, path, job, prompt, schemaPath, outPath)
}

// execClaude runs one Claude Code turn with no tools but web search and page reading, none of the user's own setup,
// and no saved session. The answer must match the job's schema; the check in accept decides either way.
func execClaude(ctx context.Context, path string, job *insightJob, prompt, schemaPath, outPath string, onLimits func([]payloadLimit)) error {
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	tools := ""
	if job.kind.search {
		tools = "WebSearch,WebFetch"
	}
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--model", insightClaude, "--effort", job.kind.effort, "--setting-sources", "", "--strict-mcp-config",
		"--tools", tools, "--allowedTools", tools, "--permission-mode", "dontAsk", "--no-session-persistence",
		"--json-schema", string(schema)}
	answer := make(chan claudeMessage, 1)
	var run agentProcess
	err = run.start(path, filepath.Dir(outPath), args, func(line []byte) {
		var m claudeMessage
		if json.Unmarshal(line, &m) != nil {
			return
		}
		if m.Type == "rate_limit_event" && onLimits != nil {
			onLimits(m.RateLimit.windows())
		}
		if m.Type == "result" {
			select {
			case answer <- m:
			default:
			}
		}
	}, nil)
	if err != nil {
		return err
	}
	defer run.close()
	if err := run.send(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": prompt}}); err != nil {
		return err
	}
	select {
	case m := <-answer:
		data, err := claudeAnswerJSON(m)
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0o600)
	case <-run.done:
		return fmt.Errorf("Claude Code stopped: %s", oneLine(run.exitReason()))
	case <-ctx.Done():
		killAgent(run.cmd)
		<-run.done
		return errors.New("Claude Code took too long")
	}
}

// claudeAnswerJSON takes the structured answer, or the text when the model wrote the JSON there. A failed turn,
// such as an expired sign-in, is an error by its own message.
func claudeAnswerJSON(m claudeMessage) ([]byte, error) {
	if m.IsError {
		return nil, fmt.Errorf("Claude Code: %s", oneLine(firstOf(m.Result, strings.Join(m.Errors, "; "), "the turn failed")))
	}
	if len(m.Structured) > 0 && string(m.Structured) != "null" {
		return m.Structured, nil
	}
	if strings.TrimSpace(m.Result) == "" {
		return nil, errors.New("Claude Code returned no answer")
	}
	return []byte(m.Result), nil
}

// execCodex runs one non-interactive Codex turn in an empty folder of its own. The process is the helper's own
// job, so unlike a player's conversation it is stopped when it runs too long.
func execCodex(ctx context.Context, path string, job *insightJob, prompt, schemaPath, outPath string) error {
	search := "disabled"
	if job.kind.search {
		search = "live"
	}
	work := filepath.Dir(outPath)
	cmd := agentCommand(path, "exec", "--ephemeral", "--skip-git-repo-check", "-C", work, "-s", "read-only",
		"-m", insightModel, "-c", "model_reasoning_effort="+job.kind.effort, "-c", "web_search="+search,
		"-c", "mcp_servers.wow_game.enabled=false", "--output-schema", schemaPath, "-o", outPath, "-")
	cmd.Dir = work
	cmd.Env = agentEnv(path)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = io.Discard
	var tail tailBuffer
	cmd.Stderr = &tail
	hideWindow(cmd)
	if err := startAgent(cmd); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Codex stopped: %v: %s", err, oneLine(lastLine(tail.String())))
		}
		return nil
	case <-ctx.Done():
		killAgent(cmd)
		<-done
		return errors.New("Codex took too long")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// dailyCap is the player's limit within what the helper allows: a packet from the game cannot lift it past that.
func dailyCap(asked int) int {
	if asked <= 0 {
		return insightDailyCap
	}
	return min(asked, insightCapLimit)
}

// spend counts runs per day. A card that kept asking by mistake must not drain the player's allowance.
func (e *insightEngine) spend(limit int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	path := filepath.Join(e.dir, "usage.json")
	var usage struct {
		Day   string `json:"day"`
		Count int    `json:"count"`
	}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &usage)
	}
	today := e.now().Format("2006-01-02")
	if usage.Day != today {
		usage.Day, usage.Count = today, 0
	}
	if usage.Count >= dailyCap(limit) {
		return false
	}
	usage.Count++
	data, _ := json.Marshal(usage)
	os.MkdirAll(e.dir, 0o700)
	return writeFileAtomic(path, data) == nil
}

func (e *insightEngine) prune() {
	cutoff := e.now().Add(-insightKeep)
	filepath.WalkDir(e.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) && (strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".out")) {
			os.Remove(path)
		}
		return nil
	})
}

func stripFence(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		if _, rest, ok := strings.Cut(s, "\n"); ok {
			s = strings.TrimSuffix(strings.TrimSpace(rest), "```")
		}
	}
	return []byte(strings.TrimSpace(s))
}
