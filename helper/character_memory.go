package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var playerGUID = regexp.MustCompile(`^Player-[0-9]{1,12}-[A-Fa-f0-9]{1,16}$`)
var loginToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
var memoryKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func validIdentity(guid, login string) bool {
	return playerGUID.MatchString(guid) && loginToken.MatchString(login)
}

type noteSource struct {
	Quote  string `json:"quote"`
	Thread string `json:"threadId,omitempty"`
	Manual bool   `json:"manual,omitempty"`
}
type memoryNote struct {
	Topic    string      `json:"topic,omitempty"`
	ID       string      `json:"id"`
	Text     string      `json:"text,omitempty"`
	Scope    string      `json:"scope,omitempty"`
	Revision int         `json:"revision"`
	At       int64       `json:"at"`
	Source   *noteSource `json:"source,omitempty"`
	Expires  int64       `json:"expires,omitempty"`
	Login    string      `json:"login,omitempty"`
}
type memoryView struct {
	Character  string       `json:"character"`
	Login      string       `json:"login"`
	Notes      []memoryNote `json:"notes"`
	Error      string       `json:"error,omitempty"`
	RequestSeq int64        `json:"requestSeq,omitempty"`
	Session    string       `json:"session,omitempty"`
}
type memoryFile struct {
	ActiveLogin string                `json:"active_login,omitempty"`
	Generation  int                   `json:"generation"`
	Notes       map[string]memoryNote `json:"notes"`
	Receipts    []string              `json:"receipts,omitempty"`
}

func characterPath(dir, guid string) string {
	return filepath.Join(dir, "character-memory", guid+".json")
}
func readMemory(path string) (memoryFile, error) {
	db := memoryFile{Notes: map[string]memoryNote{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	if err = json.Unmarshal(data, &db); err != nil || db.Notes == nil {
		return db, errors.New("could not read character memory")
	}
	return db, nil
}
func (n memoryNote) active(login string, now time.Time) bool {
	return n.Text != "" && (n.Scope != "session" || n.Login == login) && (n.Expires == 0 || n.Expires > now.Unix())
}
func (db memoryFile) view(guid, login string, now time.Time) memoryView {
	v := memoryView{Character: guid, Login: login, Notes: []memoryNote{}}
	for _, n := range db.Notes {
		if n.active(login, now) && (n.Scope != "session" || db.ActiveLogin == "" || db.ActiveLogin == login) {
			n.Login = ""
			v.Notes = append(v.Notes, n)
		}
	}
	sort.Slice(v.Notes, func(i, j int) bool { return v.Notes[i].Revision > v.Notes[j].Revision })
	return v
}

type memoryMutation struct {
	Action, ID, Text, Scope, Quote, Operation, Thread, Topic string
	Revision                                                 int
	Captured                                                 int64
	Manual                                                   bool
}

func (m memoryMutation) validate() error {
	if !memoryKey.MatchString(m.ID) || m.Revision < 0 {
		return errors.New("invalid note ID or revision")
	}
	if m.Action == "forget" {
		if m.Revision == 0 {
			return errors.New("read the note revision before removing it")
		}
		return nil
	}
	if m.Action != "upsert" || strings.TrimSpace(m.Text) == "" || utf8.RuneCountInString(m.Text) > 400 || strings.ContainsFunc(m.Text, unicode.IsControl) {
		return errors.New("note must be one line of 1–400 characters")
	}
	if !slices.Contains([]string{"", "route", "trainer", "bags", "dungeon", "stuck"}, m.Topic) {
		return errors.New("invalid memory topic")
	}
	if !slices.Contains([]string{"character", "session", "day"}, m.Scope) {
		return errors.New("scope must be character, session or day")
	}
	return nil
}
func updateMemory(dir, guid, login string, m memoryMutation, now time.Time) (memoryView, error) {
	if !validIdentity(guid, login) {
		return memoryView{}, errors.New("player identity unavailable")
	}
	if err := m.validate(); err != nil {
		return memoryView{}, err
	}
	path := characterPath(dir, guid)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return memoryView{}, err
	}
	unlock, ok := lockFile(path+".lock", true)
	if !ok {
		return memoryView{}, errors.New("cannot lock character memory")
	}
	defer unlock()
	db, err := readMemory(path)
	if err != nil {
		return memoryView{}, err
	}
	if db.ActiveLogin != "" && db.ActiveLogin != login && (m.Scope == "session" || db.Notes[m.ID].Scope == "session") {
		return db.view(guid, login, now), errors.New("this login session ended")
	}
	// Serialize model processes and in-game edits on the same lock. Replays
	// return today's view, including any newer correction or removal.
	digest := sha256.Sum256([]byte(m.Operation))
	receipt := hex.EncodeToString(digest[:])
	if slices.Contains(db.Receipts, receipt) {
		return db.view(guid, login, now), nil
	}
	for id, n := range db.Notes {
		if n.Text == "" && now.Unix()-n.At > 3600 || n.Expires > 0 && now.Unix()-n.Expires > 3600 {
			delete(db.Notes, id)
		}
	}
	previous, exists := db.Notes[m.ID]
	visible := exists && previous.active(login, now)
	if visible && m.Revision != previous.Revision || !visible && (m.Revision != 0 || m.Action == "forget") {
		return db.view(guid, login, now), errors.New("note changed; read character memory again")
	}
	if exists && !m.Manual && m.Captured < previous.At || exists && !visible && m.Captured <= previous.At {
		return db.view(guid, login, now), errors.New("this message predates the latest edit or removal")
	}
	if !visible && len(db.view(guid, login, now).Notes) >= 48 {
		return db.view(guid, login, now), errors.New("character memory is full; merge or remove obsolete notes")
	}
	db.Generation++
	next := memoryNote{ID: m.ID, Revision: db.Generation, At: now.Unix()}
	if m.Action != "forget" {
		next.Text, next.Scope, next.Topic = strings.TrimSpace(m.Text), m.Scope, m.Topic
		if m.Manual {
			next.Topic = previous.Topic
		}
		next.Source = &noteSource{Quote: m.Quote, Thread: m.Thread, Manual: m.Manual}
		if m.Scope == "session" {
			next.Login = login
		}
		if m.Scope == "day" {
			day := time.Unix(m.Captured, 0).In(time.Local)
			next.Expires = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location()).Unix()
		}
	}
	// Removed entries retain their key and revision to prevent old snapshots
	// restoring them. The user's deleted text and its source are not retained.
	db.Notes[m.ID] = next
	db.Receipts = append(db.Receipts, receipt)
	if len(db.Receipts) > 256 {
		db.Receipts = db.Receipts[len(db.Receipts)-256:]
	}
	data, err := json.Marshal(db)
	if err == nil {
		err = writeFileAtomic(path, data)
	}
	if err != nil {
		actual, readErr := readMemory(path)
		if readErr != nil {
			return memoryView{}, err
		}
		return actual.view(guid, login, now), err
	}
	return db.view(guid, login, now), nil
}

func memoryTool(name string) bool {
	return name == "get_memory" || name == "upsert_memory" || name == "forget_memory"
}

func queryMemory(dir, name string, raw json.RawMessage) (mcpObject, error) {
	var a struct {
		Topic    string `json:"topic"`
		Snapshot string `json:"snapshot_id"`
		ID       string `json:"id"`
		Text     string `json:"text"`
		Scope    string `json:"scope"`
		Quote    string `json:"quote"`
		Revision *int   `json:"revision"`
		Sources  bool   `json:"include_sources"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	state, err := loadGameSnapshot(dir, a.Snapshot)
	if err != nil {
		return nil, err
	}
	if !validIdentity(state.Character, state.Login) {
		return nil, errors.New("this question has no bound character; send a fresh in-game question")
	}
	now := time.Now()
	if name == "get_memory" {
		db, err := readMemory(characterPath(dir, state.Character))
		if err != nil {
			return nil, err
		}
		notes := db.view(state.Character, state.Login, now).Notes
		if !a.Sources {
			for i := range notes {
				notes[i].Source = nil
			}
		}
		return mcpObject{"captured": now.UTC().Format(time.RFC3339), "notes": notes}, nil
	}
	captured, err := time.Parse(time.RFC3339, state.Captured)
	if err != nil || now.Sub(captured) > time.Hour || captured.After(now.Add(time.Minute)) {
		return nil, errors.New("memory changes require a current in-game message")
	}
	quote := strings.TrimSpace(a.Quote)
	if quote == "" || utf8.RuneCountInString(quote) > 400 || !strings.Contains(state.Question, quote) {
		return nil, errors.New("quote an exact passage from the current player message (up to 400 characters)")
	}
	if a.Revision == nil {
		return nil, errors.New("supply revision from get_memory, or zero for a new note")
	}
	action := map[string]string{"upsert_memory": "upsert", "forget_memory": "forget"}[name]
	opFields := []any{a.Snapshot, action, a.ID, *a.Revision, a.Text, a.Scope, a.Topic, quote}
	op, _ := json.Marshal(opFields)
	_, err = updateMemory(dir, state.Character, state.Login, memoryMutation{Action: action, ID: a.ID, Text: a.Text, Scope: a.Scope, Topic: a.Topic, Revision: *a.Revision, Quote: quote, Operation: string(op), Thread: state.Thread, Captured: captured.Unix()}, now)
	if err != nil {
		return nil, err
	}
	return mcpObject{"captured": now.UTC().Format(time.RFC3339), "id": a.ID, "status": action}, nil
}

// Only a packet from the running game rotates the login. A late model call
// holding an older immutable snapshot cannot reactivate an expired session.
func enterMemoryLogin(dir, guid, login string) error {
	path := characterPath(dir, guid)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	unlock, ok := lockFile(path+".lock", true)
	if !ok {
		return errors.New("cannot lock character memory")
	}
	defer unlock()
	db, err := readMemory(path)
	if err != nil {
		return err
	}
	if db.ActiveLogin == login {
		return nil
	}
	db.ActiveLogin = login
	for id, n := range db.Notes {
		if n.Scope == "session" && n.Login != login {
			delete(db.Notes, id)
		}
	}
	data, err := json.Marshal(db)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func (s *session) observeCharacter(dir string, p packet) {
	if !validIdentity(p.Character, p.Login) {
		return
	}
	if s.activeCharacter != p.Character || s.activeLogin != p.Login {
		if err := enterMemoryLogin(dir, p.Character, p.Login); err != nil {
			logf("Character memory session: %v", err)
		}
		s.mu.Lock()
		s.activeCharacter, s.activeLogin = p.Character, p.Login
		s.memoryStamp = time.Time{}
		s.memoryExpiry = 0
		s.memory = nil
		s.dirty = true
		s.mu.Unlock()
	}
}
func (s *session) publishMemory(view memoryView) {
	s.memoryExpiry = 0
	for _, n := range view.Notes {
		if n.Expires > 0 && (s.memoryExpiry == 0 || n.Expires < s.memoryExpiry) {
			s.memoryExpiry = n.Expires
		}
	}
	s.mu.Lock()
	s.memory = &view
	s.dirty = true
	s.mu.Unlock()
}
func (s *session) syncMemory(dir string, now time.Time, request *packet) {
	if !validIdentity(s.activeCharacter, s.activeLogin) {
		return
	}
	path := characterPath(dir, s.activeCharacter)
	var stamp time.Time
	st, statErr := os.Stat(path)
	if statErr == nil {
		stamp = st.ModTime()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return
	}
	s.mu.Lock()
	loaded := s.memory != nil
	s.mu.Unlock()
	if request == nil && loaded && stamp.Equal(s.memoryStamp) && (s.memoryExpiry == 0 || now.Unix() < s.memoryExpiry) {
		return
	}
	db, err := readMemory(path)
	view := db.view(s.activeCharacter, s.activeLogin, now)
	if request != nil {
		p := request
		if err == nil && p.MemoryAction != "list" {
			action := p.MemoryAction
			if action == "edit" {
				action = "upsert"
				if p.Revision < 1 {
					err = errors.New("edit requires the current revision")
				}
			} else if action != "forget" {
				err = errors.New("unknown memory action")
			}
			if err == nil {
				var result memoryView
				result, err = updateMemory(dir, p.Character, p.Login, memoryMutation{Action: action, ID: p.NoteID, Text: p.NoteText, Scope: p.Scope, Revision: p.Revision, Quote: p.NoteText, Manual: true, Captured: now.Unix(), Operation: fmt.Sprintf("ui:%s:%d", p.Session, int64(p.Seq))}, now)
				if result.Character != "" {
					view = result
				}
			}
		}
		view.RequestSeq, view.Session = int64(p.Seq), p.Session
	}
	if err != nil {
		view.Error = err.Error()
	}
	// Keep the acknowledgement in the next slot even if an MCP write lands
	// before the game's next read. A transport ACK only confirms delivery.
	if request == nil && s.memory != nil {
		view.RequestSeq = s.memory.RequestSeq
		view.Session = s.memory.Session
		if err == nil {
			view.Error = s.memory.Error
		}
	}
	s.memoryStamp = stamp
	s.publishMemory(view)
}

func (s *session) loadThreadOwners() {
	data, err := os.ReadFile(filepath.Join(configDir(), "character-chats.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	owned := map[string]string{}
	if err != nil || json.Unmarshal(data, &owned) != nil {
		logf("Could not read character-chat associations")
		return
	}
	s.mu.Lock()
	s.owners = owned
	s.mu.Unlock()
}
func (s *session) bindThread(id, character string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner := s.owners[id]; owner != "" {
		if owner != character {
			return errors.New("this chat belongs to another character")
		}
		return nil
	}
	if s.owners == nil {
		s.owners = map[string]string{}
	}
	s.owners[id] = character
	data, err := json.Marshal(s.owners)
	if err == nil {
		err = writeFileAtomic(filepath.Join(configDir(), "character-chats.json"), data)
	}
	if err != nil {
		delete(s.owners, id)
	}
	return err
}

// bagNotes returns what the player asked to keep or how to treat items, for advice prepared outside a conversation.
func bagNotes(guid string) []string {
	if !playerGUID.MatchString(guid) {
		return nil
	}
	db, err := readMemory(characterPath(snapshotDir(), guid))
	if err != nil {
		return nil
	}
	var notes []string
	now := time.Now().Unix()
	for _, n := range db.Notes {
		if n.Text != "" && n.Scope == "character" && (n.Expires == 0 || n.Expires > now) {
			notes = append(notes, n.Text)
		}
	}
	sort.Strings(notes)
	return notes
}
