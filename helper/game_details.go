package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Codex starts the MCP server separately from the tray app. The mailbox holds only
// short-lived, read-only requests. The tray app publishes one at a time to an addon slot.
const detailTimeout = 42 * time.Second

type detailRequest struct {
	ID        string    `json:"id"`
	Session   string    `json:"session"`
	Refs      []gameRef `json:"refs"`
	Character string    `json:"character,omitempty"`
	Login     string    `json:"login,omitempty"`
	BagCounts bool      `json:"bagCounts,omitempty"`
	// Upgrade asks the add-on to judge auction candidates for one equipment slot instead of reading cards.
	Upgrade *upgradeQuery          `json:"upgrade,omitempty"`
	Addons  *addonInventoryRequest `json:"addons,omitempty"`
}

type detailEnvelope struct {
	detailRequest
	RunID   string `json:"runId"`
	Expires int64  `json:"expires"`
}

func detailDir(dir string) string { return filepath.Join(dir, "detail-queue") }

func bridgePresencePath(dir string) string { return filepath.Join(detailDir(dir), "online") }

// Mark the actual game session, as opposed to the process handling MCP requests.
// A normal game exit clears the marker; a stale timestamp also covers crashes.
func (s *session) pulseDetailBridge(dir string, now time.Time) {
	if now.Sub(s.lastDetailPulse) < 10*time.Second {
		return
	}
	path := bridgePresencePath(dir)
	if !s.lastDetailPulse.IsZero() && os.Chtimes(path, now, now) == nil {
		s.lastDetailPulse = now
		return
	}
	if os.MkdirAll(detailDir(dir), 0700) == nil && writeFileAtomic(path, []byte(s.runID)) == nil {
		s.lastDetailPulse = now
	}
}

func closeDetailBridge(dir, runID string) {
	path := bridgePresencePath(dir)
	if data, err := os.ReadFile(path); err == nil && string(data) == runID {
		os.Remove(path)
	}
}

func verifyDetailBridge(dir string, s gameSnapshot, now time.Time) error {
	at, err := time.Parse(time.RFC3339, s.Captured)
	if err != nil || now.Sub(at) >= time.Hour {
		return errors.New("this snapshot is too old for a live lookup; its captured entries remain available, or ask a fresh question in game")
	}
	path := bridgePresencePath(dir)
	runID, err := os.ReadFile(path)
	if err != nil || string(runID) != s.RunID {
		return errors.New("the WoW/Companion session that captured this snapshot has ended; read its captured entries or ask in the current game session")
	}
	info, err := os.Stat(path)
	if err != nil || now.Sub(info.ModTime()) > 35*time.Second {
		return errors.New("Looking for Guidance is not responding; the captured entries remain available")
	}
	return nil
}

func nextDetailRequest(dir, runID, current string, now time.Time) *detailRequest {
	files, _ := os.ReadDir(detailDir(dir))
	var oldest *detailEnvelope
	for _, f := range files {
		id, ok := strings.CutSuffix(f.Name(), ".req")
		if !ok || !snapshotIDPattern.MatchString(id) {
			continue
		}
		base := filepath.Join(detailDir(dir), id)
		raw, err := os.ReadFile(base + ".req")
		var env detailEnvelope
		if err != nil || json.Unmarshal(raw, &env) != nil || env.ID != id {
			continue
		}
		if env.Expires <= now.UnixMilli() {
			os.Remove(base + ".req")
			os.Remove(base + ".out")
			continue
		}
		if env.RunID != runID {
			continue
		}
		if _, err := os.Stat(base + ".out"); errors.Is(err, os.ErrNotExist) {
			if env.ID == current {
				return &env.detailRequest
			}
			if oldest == nil || env.Expires < oldest.Expires {
				oldest = &env
			}
		}
	}
	if oldest != nil {
		return &oldest.detailRequest
	}
	return nil
}

func (s *session) syncDetailRequest(dir string, now time.Time) {
	s.mu.Lock()
	var id string
	if s.detailRequest != nil {
		id = s.detailRequest.ID
	}
	s.mu.Unlock()
	next := nextDetailRequest(dir, s.runID, id, now)
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.detailRequest
	if (current == nil && next == nil) || (current != nil && next != nil && current.ID == next.ID) {
		return
	}
	s.detailRequest, s.dirty = next, true
}

// Each result must correspond to the exact reference requested. A late or foreign
// reply can never enrich another character's snapshot.
func acceptGameDetails(dir, runID string, p packet, now time.Time) error {
	if !snapshotIDPattern.MatchString(p.RequestID) || p.RunID == "" || p.RunID != runID || len(p.Cards) > maxDetailRefs {
		return errors.New("invalid detail response")
	}
	base := filepath.Join(detailDir(dir), p.RequestID)
	raw, err := os.ReadFile(base + ".req")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} // expired or already consumed
	if err != nil {
		return err
	}
	var env detailEnvelope
	if json.Unmarshal(raw, &env) != nil || env.ID != p.RequestID || env.RunID != runID || env.Session != p.Session || env.Expires <= now.UnixMilli() {
		return nil
	}
	if env.Character != "" && (env.Character != p.Character || env.Login != p.Login) {
		return nil
	}
	if _, err := os.Stat(base + ".out"); err == nil {
		return nil
	}
	at := time.Unix(int64(p.At), 0)
	if p.At <= 0 {
		at = now
	}
	if env.Addons != nil {
		return acceptAddonPage(base, p.Addons, at)
	}
	if env.Upgrade != nil {
		var answer map[string]json.RawMessage
		if json.Unmarshal(p.Upgrades, &answer) != nil || answer == nil || len(p.Upgrades) > 32<<10 {
			return errors.New("invalid upgrade response")
		}
		answer["read_at"], _ = json.Marshal(at.UTC().Format(time.RFC3339))
		body, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		return writeFileAtomic(base+".out", body)
	}
	result := make([]gameEntry, 0, len(env.Refs))
	for i, ref := range env.Refs {
		card := gameEntry{"kind": ref.Kind, "id": ref.ID, "status": "unavailable", "reason": "The game client did not return this card."}
		if ref.Link != "" {
			card["link"] = ref.Link
		}
		if i < len(p.Cards) {
			e := p.Cards[i]
			if e.id() == ref.ID && e.str("kind") == ref.Kind && (ref.Link == "" || e.str("link") == ref.Link) && slices.Contains([]string{"ready", "partial", "unavailable"}, e.str("status")) {
				if data, err := json.Marshal(e); err == nil && len(data) <= 20<<10 {
					card = e
				}
			}
		}
		card["read_at"] = at.UTC().Format(time.RFC3339)
		result = append(result, card)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return writeFileAtomic(base+".out", body)
}

func cacheCardPath(dir, id string, ref gameRef) string {
	// Spell effects can depend on player level. Reuse a reading only within
	// the originating question, including the precise item variant.
	encoded, _ := json.Marshal(ref)
	sum := sha256.Sum256(append([]byte(id+"\n"), encoded...))
	return filepath.Join(dir, "detail-cache", hex.EncodeToString(sum[:])+".json")
}

// Without an explicit section a lookup must identify a captured entry. With a
// section, existing callers can still request a base item or an outside spell.
func (r gameRef) resolve(s gameSnapshot) (gameRef, error) {
	if err := r.validateIdentity(); err != nil {
		return r, err
	}
	if r.Section == "" {
		var match gameRef
		for _, e := range s.Entries {
			if e.id() != r.ID || e.str("kind") != r.Kind || (r.Link != "" && e.str("link") != r.Link) {
				continue
			}
			candidate := gameRef{Kind: r.Kind, ID: r.ID, Section: e.str("section"), Link: e.str("link")}
			if candidate.checkSection(s) != nil || candidate.validateIdentity() != nil {
				continue
			}
			if match.Section != "" && candidate.Link != match.Link {
				return r, fmt.Errorf("multiple variants of %s:%d were captured; pass the exact link from the desired entry", r.Kind, r.ID)
			}
			if match.Section == "" || candidate.Section == "references" {
				match = candidate
			}
		}
		if match.Section == "" {
			section := "spells"
			if r.Kind == "item" {
				section = "gear or bags"
			}
			return r, fmt.Errorf("no matching %s:%d in the captured entries; for an outside candidate specify an enabled section (%s)", r.Kind, r.ID, section)
		}
		return match, nil
	}
	if err := r.checkSection(s); err != nil {
		return r, err
	}
	exact, err := r.attachedVariant(s)
	if err != nil {
		return r, err
	}
	return exact, exact.validateIdentity()
}

func queryDetails(dir string, raw json.RawMessage) (mcpObject, error) {
	var a struct {
		Snapshot string    `json:"snapshot_id"`
		Refs     []gameRef `json:"refs"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	if len(a.Refs) < 1 || len(a.Refs) > maxDetailRefs {
		return nil, fmt.Errorf("refs must contain 1 to %d item or spell references per call (got %d); split larger requests into batches", maxDetailRefs, len(a.Refs))
	}
	s, err := loadGameSnapshot(dir, a.Snapshot)
	if err != nil {
		return nil, err
	}
	if !s.Lookup || s.RunID == "" || s.Session == "" {
		return nil, errors.New("this snapshot has no live lookup; use the captured equipment, spellbook, talents or references from this question")
	}
	result := make([]gameEntry, len(a.Refs))
	var missing []gameRef
	for i, ref := range a.Refs {
		ref, err = ref.resolve(s)
		if err != nil {
			return nil, fmt.Errorf("refs[%d]: %w", i, err)
		}
		a.Refs[i] = ref
		data, _ := os.ReadFile(cacheCardPath(dir, a.Snapshot, ref))
		var e gameEntry
		if json.Unmarshal(data, &e) == nil && e.str("status") == "ready" && e.id() == ref.ID && e.str("kind") == ref.Kind && (ref.Link == "" || e.str("link") == ref.Link) {
			result[i] = e
		} else if !slices.Contains(missing, ref) {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		fresh, err := askGameForDetails(dir, s, missing)
		if err != nil {
			if !slices.ContainsFunc(result, func(e gameEntry) bool { return e != nil }) {
				return nil, err
			}
			for i, e := range result {
				if e == nil {
					ref := a.Refs[i]
					result[i] = gameEntry{"kind": ref.Kind, "id": ref.ID, "status": "unavailable", "reason": err.Error()}
					if ref.Link != "" {
						result[i]["link"] = ref.Link
					}
				}
			}
			return mcpObject{"captured": s.Captured, "cards": result}, nil
		}
		for i, e := range fresh {
			ref := missing[i]
			if e.str("status") == "ready" {
				path := cacheCardPath(dir, a.Snapshot, ref)
				if os.MkdirAll(filepath.Dir(path), 0700) == nil {
					if body, err := json.Marshal(e); err == nil {
						_ = writeFileAtomic(path, body)
					}
				}
			}
			for j, r := range a.Refs {
				if r == ref {
					result[j] = e
				}
			}
		}
	}
	return mcpObject{"captured": s.Captured, "cards": result}, nil
}

func askGameForDetails(dir string, s gameSnapshot, refs []gameRef) ([]gameEntry, error) {
	if err := verifyDetailBridge(dir, s, time.Now()); err != nil {
		return nil, err
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	req := detailRequest{ID: hex.EncodeToString(key[:]), Session: s.Session, Refs: refs}
	if slices.ContainsFunc(refs, func(ref gameRef) bool { return ref.Kind == "recipe" }) {
		if !validIdentity(s.Character, s.Login) {
			return nil, errors.New("recipe lookup requires the player identity from the question")
		}
		req.Character, req.Login = s.Character, s.Login
		_, req.BagCounts = s.Sections["bags"]
	}
	data, err := askGame(dir, s, req, detailTimeout)
	if err != nil {
		return nil, err
	}
	var cards []gameEntry
	if json.Unmarshal(data, &cards) != nil || len(cards) != len(refs) {
		return nil, errors.New("invalid detail reply from companion")
	}
	return cards, nil
}

// askGame queues one request for the tray to publish and waits for the game's answer.
func askGame(dir string, s gameSnapshot, req detailRequest, wait time.Duration) ([]byte, error) {
	data, err := json.Marshal(detailEnvelope{detailRequest: req, RunID: s.RunID, Expires: time.Now().Add(wait).UnixMilli()})
	if err != nil {
		return nil, err
	}
	base := filepath.Join(detailDir(dir), req.ID)
	if err := writeFileAtomic(base+".req", data); err != nil {
		return nil, err
	}
	defer os.Remove(base + ".req")
	defer os.Remove(base + ".out")
	timeout := time.NewTimer(wait)
	defer timeout.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	lastCheck := time.Now()
	for {
		select {
		case <-timeout.C:
			return nil, fmt.Errorf("the game client did not answer within %d seconds (offline, UI reloaded or transport busy); use the captured game data or a source for this version of WoW", int(wait.Seconds()))
		case now := <-tick.C:
			data, err := os.ReadFile(base + ".out")
			if errors.Is(err, os.ErrNotExist) {
				if now.Sub(lastCheck) >= time.Second {
					if err := verifyDetailBridge(dir, s, now); err != nil {
						return nil, err
					}
					lastCheck = now
				}
				continue
			}
			return data, err
		}
	}
}
