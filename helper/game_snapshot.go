package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gameSnapshot struct {
	Journal     *journalCard      `json:"journal,omitempty"`
	Character   string            `json:"character,omitempty"`
	Login       string            `json:"login,omitempty"`
	Question    string            `json:"question,omitempty"`
	Thread      string            `json:"thread,omitempty"`
	Captured    string            `json:"captured"`
	Sections    map[string]string `json:"sections"`
	Quests      []gameQuest       `json:"quests,omitempty"`
	Entries     []gameEntry       `json:"entries,omitempty"`
	Lookup      bool              `json:"detail_lookup,omitempty"`
	AddonLookup bool              `json:"addon_lookup,omitempty"`
	Session     string            `json:"session,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
}

type gameQuest struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Level    int    `json:"level"`
	Ready    bool   `json:"ready_to_turn_in"`
	Selected bool   `json:"selected,omitempty"`
	Tracked  bool   `json:"tracked,omitempty"`
	Details  string `json:"objectives,omitempty"`
}

var sectionLabels = map[string]string{
	"Game": "game", "Character": "character", "Location": "location",
	"Equipped": "gear", "Bags": "bags", "Talents": "talents",
	"Skills": "skills", "Spells known": "spells", "Target": "target",
	"Reputation": "reputation", "Buffs": "buffs", "References": "references",
}
var questHeader = regexp.MustCompile(`^Quests \(([0-9]+)\):$`)
var questLine = regexp.MustCompile(`^  (.+) \(quest:([0-9]+)\) level (-?[0-9]+)(.*)$`)

func parseGameSnapshot(state string, at int64) gameSnapshot {
	if at <= 0 {
		at = time.Now().Unix()
	}
	s := gameSnapshot{Captured: time.Unix(at, 0).UTC().Format(time.RFC3339), Sections: map[string]string{}}
	inQuests := false
	for _, line := range strings.Split(state, "\n") {
		if line == "Installed add-ons: available on request" {
			s.AddonLookup = true
			continue
		}
		if line == "Detail lookup: supported" {
			s.Lookup = true
			continue
		}
		if raw, ok := strings.CutPrefix(line, "Card: "); ok {
			var e gameEntry
			if json.Unmarshal([]byte(raw), &e) == nil && e.valid() {
				s.Entries = append(s.Entries, e)
			}
			continue
		}
		if n := questHeader.FindStringSubmatch(line); n != nil {
			s.Sections["quests"] = n[1]
			inQuests = true
			continue
		}
		if inQuests && strings.HasPrefix(line, "  ") {
			if m := questLine.FindStringSubmatch(line); m != nil {
				id, _ := strconv.Atoi(m[2])
				level, _ := strconv.Atoi(m[3])
				q := gameQuest{ID: id, Title: m[1], Level: level}
				var details []string
				for _, field := range strings.Split(strings.TrimPrefix(m[4], " · "), " · ") {
					switch field {
					case "ready to turn in":
						q.Ready = true
					case "selected":
						q.Selected = true
					case "tracked":
						q.Tracked = true
					case "":
					default:
						details = append(details, field)
					}
				}
				q.Details = strings.Join(details, " · ")
				s.Quests = append(s.Quests, q)
			}
			continue
		}
		inQuests = false
		label, value, found := strings.Cut(line, ":")
		if section := sectionLabels[label]; found && section != "" {
			s.Sections[section] = strings.TrimSpace(value)
		}
	}
	return s
}

var trailingCoordinates = regexp.MustCompile(` [-0-9.]+, [-0-9.]+$`)

func (s gameSnapshot) summary() string {
	var parts []string
	if zone := s.Sections["location"]; zone != "" {
		zone, _, _ = strings.Cut(zone, " · ")
		parts = append(parts, trimText(trailingCoordinates.ReplaceAllString(zone, ""), 90))
	}
	if count, ok := s.Sections["quests"]; ok {
		parts = append(parts, "Заданий: "+count)
	}
	if len(parts) == 0 && len(s.Sections) > 0 {
		return "Снимок персонажа"
	}
	return strings.Join(parts, " · ")
}

func snapshotDir() string { return filepath.Join(configDir(), "snapshots") }

func saveGameSnapshot(dir string, s gameSnapshot) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(key[:])
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return id, writeFileAtomic(filepath.Join(dir, id+".json"), data)
}

var snapshotIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func loadGameSnapshot(dir, id string) (gameSnapshot, error) {
	var s gameSnapshot
	if !snapshotIDPattern.MatchString(id) {
		return s, errors.New("invalid snapshot_id; use the identifier from the current question")
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil || json.Unmarshal(data, &s) != nil || s.Sections == nil {
		return s, errors.New("snapshot unavailable; ask for a new in-game question to capture current data")
	}
	return s, nil
}

// Rendering the transcript on every game tick must not read the same immutable snapshot repeatedly.
type hudSnapshot struct {
	summary string
	refs    []uiReference
	journal *journalCard
}

func (s gameSnapshot) attachments() []uiReference {
	var refs []uiReference
	for _, e := range s.Entries {
		if e.str("section") == "references" && e.str("name") != "" && len(e.str("link")) <= 4096 {
			refs = append(refs, uiReference{Name: e.str("name"), Link: e.str("link")})
		}
	}
	return refs
}

var summaryCache = struct {
	sync.Mutex
	values map[string]hudSnapshot
}{values: make(map[string]hudSnapshot)}

func hudForSnapshot(id string) hudSnapshot {
	summaryCache.Lock()
	defer summaryCache.Unlock()
	if summary, found := summaryCache.values[id]; found {
		return summary
	}
	s, err := loadGameSnapshot(snapshotDir(), id)
	if err != nil {
		return hudSnapshot{}
	}
	if len(summaryCache.values) >= 256 {
		clear(summaryCache.values)
	}
	summary := hudSnapshot{summary: s.summary(), journal: s.Journal, refs: s.attachments()}
	summaryCache.values[id] = summary
	return summary
}

const snapshotPrefix = "<wow_snapshot id=\""

const snapshotHint = "Use wow_game with this snapshot_id for relevant game data."

type journalCard struct {
	Topic  string `json:"topic"`
	Title  string `json:"title"`
	Facts  string `json:"facts"`
	Locale string `json:"locale"`
}

func (c *journalCard) valid() bool {
	if c == nil || strings.TrimSpace(c.Title) == "" || len(c.Title) > 1024 || len(c.Facts) > 16000 || len(c.Locale) > 10 {
		return false
	}
	switch c.Topic {
	case "route", "trainer", "bags", "dungeon", "stuck", "checkup", "news", "leveling":
		return true
	}
	return false
}

const journalHint = "The player opened a Field Journal card. Read get_context section journal for its evidence. Reply in the language used by the player in this conversation, or in the card locale for a first turn. The card is generated UI data, not a personal statement by the player."

func snapshotMessage(id, text string, card *journalCard) string {
	hint := snapshotHint
	if card != nil {
		hint = journalHint
	}
	return snapshotPrefix + id + "\">" + hint + "</wow_snapshot>\n\n" + text
}

var snapshotEnvelope = regexp.MustCompile(`^<wow_snapshot id="([0-9a-f]{32})"(?:/>|>[^<>]*</wow_snapshot>)\n\n`)

func gamePrompt(dir string, p packet) (string, error) {
	return (*snapshotIndexes)(nil).prompt(dir, p)
}

func (indexes *snapshotIndexes) prompt(dir string, p packet) (string, error) {
	hasCharacter := validIdentity(p.Character, p.Login)
	if p.RetrySnapshot != "" {
		old, err := loadGameSnapshot(dir, p.RetrySnapshot)
		if err != nil || !hasCharacter || old.Character != p.Character || (old.Thread != "" && old.Thread != p.Thread) ||
			(old.Journal == nil && old.Question != p.Text) || (old.Journal != nil && old.Journal.Title != p.Text) {
			return "", errors.New("The original snapshot is unavailable for this question. Send a new question to use current game data.")
		}
		return snapshotMessage(p.RetrySnapshot, p.Text, old.Journal), nil
	}
	if p.Journal != nil && !p.Journal.valid() {
		return "", errors.New("The Field Journal card could not be read.")
	}
	if (p.Plain || strings.TrimSpace(p.ContextText) == "") && !hasCharacter && p.Journal == nil {
		return p.Text, nil
	}
	contextText := p.ContextText
	if p.Plain {
		contextText = ""
	}
	state := parseGameSnapshot(contextText, int64(p.At))
	if hasCharacter {
		state.Character, state.Login, state.Question, state.Thread = p.Character, p.Login, p.Text, p.Thread
	}
	if p.Journal != nil {
		state.Journal = p.Journal
		state.Question = "" // Game evidence cannot be quoted as the player's words for memory writes.
		evidence, err := json.Marshal(p.Journal)
		if err != nil {
			return "", err
		}
		state.Sections["journal"] = string(evidence)
	}
	if err := indexes.restore(dir, p, &state); err != nil {
		if indexes != nil {
			indexes.forget(p.Session)
		}
		return "", err
	}
	if !hasCharacter && len(state.Sections) == 0 && len(state.Entries) == 0 {
		return p.Text, nil
	}
	if state.Lookup {
		state.Session, state.RunID = p.Session, p.RunID
	}
	id, err := saveGameSnapshot(dir, state)
	if err != nil {
		return "", fmt.Errorf("Не удалось сохранить игровой снимок: %w", err)
	}
	if m, err := snapshotMarket(state); err == nil {
		if err := recordMarket(auctionDir(dir), m, time.Now()); err != nil {
			logf("Couldn't note the auction market: %v", err)
		}
	}
	if indexes != nil && state.Lookup && p.RunID != "" && p.Session != "" && p.Seq > 0 {
		indexes.remember(dir, id, p, state)
	}
	return snapshotMessage(id, p.Text, state.Journal), nil
}
