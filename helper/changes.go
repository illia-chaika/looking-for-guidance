package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// The changes feed tells the player what changed in the game that concerns their character. Everything is found by
// game IDs on this computer, without the adviser: Blizzard's hotfix notes (read from Blizzard's page by the companion
// itself) and the data changes the project's backend finds in the game client's tables. The adviser only writes a
// short summary when the player asks for one (digestKind).

const (
	changesFileName = "changes.json"
	changesMaxBytes = 8 << 20
	changesKeep     = 30 * 24 * time.Hour // Blizzard's API terms: its values are kept at most 30 days
	changesDays     = 14                  // as many days of data changes as of hotfix notes (feedDays)
	changesShown    = 24                  // items per group in the card
	changesTextMax  = 300
)

// changesFile is world/changes.json from the backend: when Blizzard's pages last changed and the data changes of
// the last 30 days. It holds no Blizzard text.
type changesFile struct {
	Built    string `json:"built"`
	Build    string `json:"build"`
	Hotfixes struct {
		URL       string `json:"url"`
		Newest    string `json:"newest"`
		ChangedAt string `json:"changed_at"`
	} `json:"hotfixes"`
	Issues struct {
		URL       string `json:"url"`
		ChangedAt string `json:"changed_at"`
	} `json:"issues"`
	Changes []dataChange `json:"changes"`
}

type dataChange struct {
	Date   string                       `json:"date"`
	Table  string                       `json:"table"`
	Row    int64                        `json:"row"`
	Kind   string                       `json:"kind"`
	ID     int64                        `json:"id"`
	Change string                       `json:"change"`
	Fields map[string][]json.RawMessage `json:"fields"`
	Before map[string]json.RawMessage   `json:"before"`
	After  map[string]json.RawMessage   `json:"after"`
}

func changesPath(worldDir string) string { return filepath.Join(worldDir, changesFileName) }

// readChanges returns the stored feed, or nil when there is none or it is too old to use.
func readChanges(worldDir string) (*changesFile, error) {
	body, err := os.ReadFile(changesPath(worldDir))
	if err != nil {
		return nil, err
	}
	var f changesFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, err
	}
	if built, err := time.Parse(time.RFC3339, f.Built); err != nil || time.Since(built) > changesKeep {
		return nil, errors.New("the stored changes feed is too old")
	}
	return &f, nil
}

// changesStamp is the modification time of the stored feed; it moves only when the backend published a new one.
func changesStamp(worldDir string) time.Time {
	info, err := os.Stat(changesPath(worldDir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// refreshChanges downloads the feed. The tray calls it every hour; an unchanged feed is not written again.
func (w *worldWatcher) refreshChanges(ctx context.Context) error {
	path := changesPath(w.dir)
	if f, err := readChanges(w.dir); err != nil && !errors.Is(err, os.ErrNotExist) || f == nil {
		os.Remove(path) // unreadable or older than 30 days
	}
	base := strings.TrimSuffix(w.base(), "/")
	if base == "" {
		return nil
	}
	// The feed grows to a few megabytes over 30 days: ask with the last ETag and download it only when it changed.
	etagPath := path + ".etag"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/world/"+changesFileName, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	if tag, err := os.ReadFile(etagPath); err == nil && len(tag) > 0 && changesStamp(w.dir) != (time.Time{}) {
		req.Header.Set("If-None-Match", string(tag))
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", req.URL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, changesMaxBytes+1))
	if err != nil {
		return err
	}
	if len(body) > changesMaxBytes {
		return errors.New("the changes feed is larger than expected")
	}
	defer func() {
		if tag := resp.Header.Get("ETag"); tag != "" {
			os.WriteFile(etagPath, []byte(tag), 0o600)
		}
	}()
	var f changesFile
	if err := json.Unmarshal(body, &f); err != nil || f.Built == "" {
		return errors.New("the changes feed is unreadable")
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, body) {
		return nil
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, body)
}

// feedStale says whether a cached copy of a Blizzard page should be fetched again. When the backend's feed says when
// the page last changed, it is fetched only after that (and once a day in case the feed stopped); when the feed is
// missing or the backend could not read the page, every 3 hours.
func feedStale(fetched int64, now time.Time, worldDir string, changedAt func(*changesFile) string) bool {
	age := now.Sub(time.Unix(fetched, 0))
	f, err := readChanges(worldDir)
	if err != nil || f == nil {
		return age >= feedMaxAge
	}
	at, err := time.Parse(time.RFC3339, changedAt(f))
	if err != nil {
		return age >= feedMaxAge
	}
	return at.Unix() > fetched || age >= 24*time.Hour
}

// --- hotfix notes with their structure ---------------------------------------------------------------------------

// hotfixNote is one line of Blizzard's notes with the headings above it: the category (Classes, Quests…), then
// headings such as a class, a specialization, a dungeon or a profession, and parent notes of a nested list.
type hotfixNote struct {
	Date     string
	Category string
	Path     []string
	Text     string
	Dev      string // the developers' notes that follow it
}

func parseNotes(days []feedDay) []hotfixNote {
	var notes []hotfixNote
	type open struct {
		depth int
		text  string
	}
	for _, day := range days {
		lines := strings.Split(day.Text, "\n")
		category, first := "", len(notes)
		var stack []open
		for i, line := range lines {
			trimmed := strings.TrimLeft(line, " ")
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			depth := (len(line) - len(trimmed)) / 2
			if !strings.HasPrefix(trimmed, "- ") {
				if depth == 0 {
					category, stack = strings.TrimSpace(trimmed), nil
				}
				continue
			}
			text := strings.TrimSpace(trimmed[2:])
			for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
				stack = stack[:len(stack)-1]
			}
			if strings.HasPrefix(strings.ToLower(text), "developers") {
				if len(notes) > first {
					n := &notes[len(notes)-1]
					n.Dev = strings.TrimSpace(n.Dev + " " + text)
				}
				continue
			}
			children := false
			for _, next := range lines[i+1:] {
				t := strings.TrimLeft(next, " ")
				if strings.TrimSpace(t) == "" {
					continue
				}
				children = strings.HasPrefix(t, "- ") && (len(next)-len(t))/2 > depth
				break
			}
			path := make([]string, 0, len(stack))
			for _, s := range stack {
				path = append(path, s.text)
			}
			stack = append(stack, open{depth, text})
			if children && len(text) < 45 && !strings.HasSuffix(text, ".") {
				continue // a heading: a class, a specialization, a dungeon, a profession
			}
			notes = append(notes, hotfixNote{Date: day.Date, Category: category, Path: path, Text: text})
		}
	}
	return notes
}

// --- names in the notes, resolved to the world dataset ------------------------------------------------------------

// nameIndex finds the English names of quests, items, class spells, zones and dungeons in a note.
type nameIndex struct {
	byName map[string][]*worldEntry
	places map[string]bool // folded names of zones and dungeons
}

var nameIndexCache struct {
	sync.Mutex
	world *worldData
	index *nameIndex
}

func indexFor(d *worldData) *nameIndex {
	if d == nil {
		return nil
	}
	nameIndexCache.Lock()
	defer nameIndexCache.Unlock()
	if nameIndexCache.world == d {
		return nameIndexCache.index
	}
	x := &nameIndex{byName: map[string][]*worldEntry{}, places: map[string]bool{}}
	for _, e := range d.entries {
		key := foldName(e.en)
		if utf8.RuneCountInString(key) < 4 {
			continue
		}
		switch e.kind {
		case "places":
			if !strings.HasPrefix(e.id, "zone-") && !strings.HasPrefix(e.id, "dungeon-") {
				continue
			}
			x.places[key] = true
		case "spells":
			if _, ok := e.brief["class"]; !ok {
				continue // recipes and item effects are found by their items' names
			}
		case "items", "quests":
		default:
			continue
		}
		x.byName[key] = append(x.byName[key], e)
	}
	nameIndexCache.world, nameIndexCache.index = d, x
	return x
}

type noteRef struct {
	e          *worldEntry
	possessive bool // "Vitrix's Poison Shock": a creature's ability, not the character's spell
}

var (
	wordPattern  = regexp.MustCompile(`[\p{L}\p{N}]+`)
	quotePattern = regexp.MustCompile(`["“][^"“”]+["”]`)
)

// scan returns the records named in a note, longest names first and without overlaps. A name counts only where it
// starts with a capital letter, the way the notes write names; a one-word name never counts at a sentence's start,
// unless the text is a heading (a dungeon or zone above a group of notes).
func (x *nameIndex) scan(text string, heading bool) []noteRef {
	if x == nil {
		return nil
	}
	spans := wordPattern.FindAllStringIndex(text, -1)
	words := make([]string, len(spans))
	for i, s := range spans {
		words[i] = strings.ToLower(text[s[0]:s[1]])
	}
	var refs []noteRef
	for i := 0; i < len(words); {
		first, _ := utf8.DecodeRuneInString(text[spans[i][0]:])
		matched := 0
		if unicode.IsUpper(first) {
			for n := min(6, len(words)-i); n >= 1 && matched == 0; n-- {
				if n == 1 && i == 0 && !heading {
					break
				}
				key := strings.Join(words[i:i+n], " ")
				found := x.byName[key]
				if len(found) == 0 && strings.HasSuffix(key, "s") && len(words[i+n-1]) > 3 {
					found = x.byName[strings.TrimSuffix(key, "s")]
				}
				if len(found) == 0 {
					continue
				}
				possessive := i >= 1 && words[i-1] == "s" && spans[i-1][0] > 0 && strings.ContainsAny(text[spans[i-1][0]-1:spans[i-1][0]], "'’") ||
					i >= 1 && strings.HasSuffix(text[:spans[i][0]], "'s ")
				for _, e := range found {
					refs = append(refs, noteRef{e: e, possessive: possessive})
				}
				matched = n
			}
		}
		i += max(matched, 1)
	}
	return refs
}

// --- matching a character -------------------------------------------------------------------------------------

// changeFacts is what the card sends: the profile plus game IDs. ID lists come as comma-joined strings so the
// add-on's packet trimming cannot cut them.
type changeFacts struct {
	Class       string `json:"class"`
	Race        string `json:"race"`
	Faction     string `json:"faction"`
	Level       int    `json:"level"`
	Zone        string `json:"zone"`
	Map         int    `json:"map"`
	Professions []struct {
		Name string `json:"name"`
		Rank int    `json:"rank"`
		Max  int    `json:"max"`
	} `json:"professions"`
	Quests []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Level int    `json:"level"`
	} `json:"quests"`
	Gear   string `json:"gear"`
	Bags   string `json:"bags"`
	Spells string `json:"spells"`
}

var (
	classNames = map[string]string{"WARRIOR": "Warrior", "PALADIN": "Paladin", "HUNTER": "Hunter", "ROGUE": "Rogue",
		"PRIEST": "Priest", "SHAMAN": "Shaman", "MAGE": "Mage", "WARLOCK": "Warlock", "DRUID": "Druid"}
	// raceWords maps a race token to how the notes name the race; races the client may add are named by their token.
	raceWords = map[string][]string{"Human": {"Human", "Humans"}, "Orc": {"Orc", "Orcs"}, "Dwarf": {"Dwarf", "Dwarves"},
		"NightElf": {"Night Elf", "Night Elves"}, "Scourge": {"Undead", "Forsaken"}, "Tauren": {"Tauren"},
		"Gnome": {"Gnome", "Gnomes"}, "Troll": {"Troll", "Trolls"}, "Skyborne": {"Skyborne"}}
	// professionWords: the profession, its practitioners, its recipe prefix, its tracking spell. Standard game words.
	professionWords = map[string][]string{
		"Alchemy":        {"Alchemy", "Alchemist", "Alchemists"},
		"Blacksmithing":  {"Blacksmithing", "Blacksmith", "Blacksmiths", "Plans:", "Swordsmith", "Axesmith", "Hammersmith", "Weaponsmith", "Armorsmith"},
		"Enchanting":     {"Enchanting", "Enchanter", "Enchanters", "Formula:"},
		"Engineering":    {"Engineering", "Engineer", "Engineers", "Schematic:"},
		"Herbalism":      {"Herbalism", "Herbalist", "Herbalists", "Find Herbs"},
		"Leatherworking": {"Leatherworking", "Leatherworker", "Leatherworkers", "Pattern:"},
		"Mining":         {"Mining", "Miner", "Miners", "Find Minerals"},
		"Skinning":       {"Skinning", "Skinner", "Skinners"},
		"Tailoring":      {"Tailoring", "Tailor", "Tailors", "Pattern:"},
		"Cooking":        {"Cooking", "Recipe:"},
		"Fishing":        {"Fishing"},
		"First Aid":      {"First Aid"},
	}
)

func wordIn(word, text string) bool {
	lw, lt := strings.ToLower(word), strings.ToLower(text)
	if strings.HasSuffix(lw, ":") {
		return strings.Contains(lt, lw)
	}
	for from := 0; ; {
		i := strings.Index(lt[from:], lw)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(lw)
		before, _ := utf8.DecodeLastRuneInString(lt[:start])
		after, _ := utf8.DecodeRuneInString(lt[end:])
		if (start == 0 || !unicode.IsLetter(before)) && (end == len(lt) || !unicode.IsLetter(after) || (after == 's' && !nextLetter(lt, end+1))) {
			return true
		}
		from = start + 1
	}
}

func nextLetter(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsLetter(r)
}

func ids(list string) map[int64]bool {
	out := map[int64]bool{}
	for _, part := range strings.Split(list, ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && n > 0 {
			out[n] = true
		}
	}
	return out
}

type changeMatcher struct {
	f                  changeFacts
	world              *worldData
	index              *nameIndex
	class              string
	races              []string
	profs              []string
	quests             map[int64]string
	gear, bags, spells map[int64]bool
	zone               string
}

func newChangeMatcher(f changeFacts, d *worldData) *changeMatcher {
	m := &changeMatcher{f: f, world: d, index: indexFor(d), class: classNames[f.Class], quests: map[int64]string{},
		gear: ids(f.Gear), bags: ids(f.Bags), spells: ids(f.Spells), zone: f.Zone}
	m.races = raceWords[f.Race]
	if m.races == nil && f.Race != "" {
		m.races = []string{f.Race}
	}
	for _, p := range f.Professions {
		m.profs = append(m.profs, p.Name)
	}
	for _, q := range f.Quests {
		m.quests[int64(q.ID)] = q.Title
	}
	if d != nil && f.Map > 0 {
		if e := d.byKey["places:zone-"+strconv.Itoa(f.Map)]; e != nil {
			m.zone = e.en
		}
	}
	return m
}

type changeItem struct {
	Date     string      `json:"date"`
	Category string      `json:"category,omitempty"`
	Text     string      `json:"text,omitempty"`
	Kind     string      `json:"kind"`   // note or data
	Reason   string      `json:"reason"` // quest, zone, item, spell, profession, class or race
	Name     string      `json:"name,omitempty"`
	ID       int64       `json:"id,omitempty"`
	Fields   [][3]string `json:"fields,omitempty"` // data changes: what, was, now
	tier     string
	dev      string
}

const (
	tierMine    = "mine"    // names something the character has: notified
	tierClass   = "class"   // the character's class or race: listed
	tierGeneral = "general" // names no character at all: offered to the summary only
)

func pathHas(path []string, word string) bool {
	for _, p := range path {
		if strings.EqualFold(p, word) {
			return true
		}
	}
	return false
}

func otherClassHeading(path []string, mine string) bool {
	for _, p := range path {
		for _, c := range classNames {
			if strings.EqualFold(p, c) && c != mine {
				return true
			}
		}
	}
	return false
}

func otherRaceHeading(path []string, mine []string) bool {
	for _, p := range path {
		for _, words := range raceWords {
			if pathHas([]string{p}, words[0]) && !pathHas(mine, p) {
				return true
			}
		}
	}
	return false
}

// note decides how one note concerns the character; tier "" drops it.
func (m *changeMatcher) note(n hotfixNote) changeItem {
	item := changeItem{Date: n.Date, Category: n.Category, Text: clip(n.Text, changesTextMax), Kind: "note", dev: n.Dev}
	if otherClassHeading(n.Path, m.class) || otherRaceHeading(n.Path, m.races) {
		return item
	}
	plain := quotePattern.ReplaceAllString(n.Text, " ")
	var named []string
	for _, c := range classNames {
		if wordIn(c, plain) {
			named = append(named, c)
		}
	}
	if len(named) > 0 && !slices.Contains(named, m.class) {
		return item // about another class
	}
	refs := m.index.scan(n.Text, false)
	for _, p := range n.Path {
		refs = append(refs, m.index.scan(p, true)...)
	}
	gated, specific := false, len(named) > 0
	// A dungeon's loot and materials matter to a profession only in a dungeon of the character's level; a dungeon the
	// dataset does not know yet gives no level, so its notes reach a profession never.
	inDungeon, dungeonFits := n.Category == "Dungeons" || n.Category == "Raids", false
	for _, r := range refs {
		specific = true
		if (r.e.kind == "quests" || r.e.kind == "places") && m.f.Level > 0 && r.e.lo > m.f.Level+10 {
			gated = true // far above the character's level
		}
		if r.e.kind == "places" && strings.HasPrefix(r.e.id, "dungeon-") && r.e.lo > 0 && r.e.lo <= m.f.Level+10 {
			dungeonFits = true
		}
	}
	professionGate := gated || (inDungeon && !dungeonFits)
	set := func(tier, reason, name string, id int64) changeItem {
		item.tier, item.Reason, item.Name, item.ID = tier, reason, name, id
		return item
	}
	lower := strings.ToLower(n.Text)
	for _, r := range refs {
		if id, _ := strconv.ParseInt(r.e.id, 10, 64); r.e.kind == "quests" && m.quests[id] != "" {
			return set(tierMine, "quest", r.e.en, id)
		}
	}
	for id, title := range m.quests {
		if utf8.RuneCountInString(title) >= 5 && strings.Contains(lower, strings.ToLower(title)) {
			return set(tierMine, "quest", title, id)
		}
	}
	if m.zone != "" && (pathHas(n.Path, m.zone) || wordIn(m.zone, n.Text)) {
		return set(tierMine, "zone", m.zone, 0)
	}
	for _, r := range refs {
		id, _ := strconv.ParseInt(r.e.id, 10, 64)
		if r.e.kind == "items" && (m.gear[id] || m.bags[id]) {
			return set(tierMine, "item", r.e.en, id)
		}
		if r.e.kind == "spells" && !r.possessive && m.spells[id] {
			return set(tierMine, "spell", r.e.en, id)
		}
	}
	anyProfession := false
	for name, words := range professionWords {
		for _, w := range words {
			if pathHas(n.Path, w) || wordIn(w, plain) {
				anyProfession = true
				if slices.Contains(m.profs, name) && !professionGate {
					return set(tierMine, "profession", name, 0)
				}
			}
		}
	}
	if !professionGate {
		for _, r := range refs {
			if r.e.kind != "items" {
				continue
			}
			for _, p := range itemProfessions(m.world, r.e.id) {
				if slices.Contains(m.profs, p) {
					return set(tierMine, "profession", p, 0)
				}
			}
		}
	}
	raceNamed := false
	for _, words := range raceWords {
		for _, w := range words {
			raceNamed = raceNamed || wordIn(w, plain)
		}
	}
	if !gated && m.class != "" && (pathHas(n.Path, m.class) || slices.Contains(named, m.class)) {
		return set(tierClass, "class", m.class, 0)
	}
	if !gated {
		for _, w := range m.races {
			if pathHas(n.Path, w) || wordIn(w, plain) {
				return set(tierClass, "race", m.races[0], 0)
			}
		}
		for _, r := range refs {
			if c, _ := r.e.brief["class"].(string); r.e.kind == "spells" && !r.possessive && c != "" && c == m.f.Class {
				id, _ := strconv.ParseInt(r.e.id, 10, 64)
				return set(tierClass, "class", r.e.en, id)
			}
		}
	}
	if !specific && !anyProfession && !raceNamed && !zonePrefix.MatchString(n.Text) && len(n.Path) == 0 {
		return set(tierGeneral, "", "", 0)
	}
	return item
}

// zonePrefix is the "Zone - " the notes put before creature and quest fixes in one place.
var zonePrefix = regexp.MustCompile(`^[A-Z][\p{L}'’ ]{2,40} [-–] `)

func itemProfessions(d *worldData, id string) []string {
	if d == nil {
		return nil
	}
	raw := d.records["items"][id]
	if !bytes.Contains(raw, []byte(`"created_by"`)) {
		return nil
	}
	var r struct {
		Foreverdb struct {
			CreatedBy []struct {
				Profession string `json:"profession"`
			} `json:"created_by"`
		} `json:"foreverdb"`
	}
	json.Unmarshal(raw, &r)
	var out []string
	for _, c := range r.Foreverdb.CreatedBy {
		if c.Profession != "" && !slices.Contains(out, c.Profession) {
			out = append(out, c.Profession)
		}
	}
	return out
}

// fieldLabels names the client columns a player understands; the add-on translates the labels.
var fieldLabels = map[string]string{
	"Bonding": "binding", "ItemLevel": "item_level", "RequiredLevel": "required_level", "SellPrice": "sell_price",
	"BuyPrice": "buy_price", "OverallQualityID": "quality", "EffectBasePointsF": "value", "EffectBonusCoefficient": "coefficient",
	"BonusCoefficientFromAP": "ap_coefficient", "CastingTimeIndex": "cast_time", "DurationIndex": "duration",
	"RangeIndex": "range", "CoolDownMSec": "cooldown", "RecoveryTime": "cooldown", "CategoryRecoveryTime": "cooldown",
	"ManaCost": "mana_cost", "PowerCostPct": "mana_cost", "EffectAuraPeriod": "period", "BaseLevel": "level", "SpellLevel": "level",
}

func rawText(v json.RawMessage) string {
	if t := bytes.TrimSpace(v); len(t) == 0 || string(t) == "null" {
		return "" // the side of an added or removed row; the add-on shows a dash
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return clip(string(bytes.TrimSpace(v)), 60)
}

// bindingCodes and qualityNames turn Battle.net's English words into the client's numbers, which the add-on shows
// in the player's language with the game's own strings.
var bindingCodes = map[string]string{"Binds when picked up": "1", "Binds when equipped": "2", "Binds when used": "3", "Quest Item": "4"}

// valueText writes one Battle.net value the way a tooltip reads.
func valueText(key string, v json.RawMessage) string {
	switch key {
	case "binding":
		if code := bindingCodes[rawText(v)]; code != "" {
			return code
		}
	case "quality":
		if i := slices.Index(qualityNames, rawText(v)); i >= 0 {
			return strconv.Itoa(i)
		}
	case "stats":
		var stats []struct {
			Type  string  `json:"type"`
			Value float64 `json:"value"`
		}
		if json.Unmarshal(v, &stats) == nil {
			parts := make([]string, 0, len(stats))
			for _, s := range stats {
				parts = append(parts, fmt.Sprintf("%s %+g", s.Type, s.Value))
			}
			return strings.Join(parts, ", ")
		}
	case "weapon_damage":
		var d []float64
		if json.Unmarshal(v, &d) == nil && len(d) == 2 {
			return fmt.Sprintf("%g–%g", d[0], d[1])
		}
	case "weapon_speed":
		var ms float64
		if json.Unmarshal(v, &ms) == nil && ms > 0 {
			return strconv.FormatFloat(ms/1000, 'f', 2, 64) // Battle.net gives milliseconds
		}
	}
	return rawText(v)
}

// dataFields lists what a data change changed: Battle.net's readable values when the backend fetched them, the
// client's numbers otherwise.
func dataFields(c dataChange) [][3]string {
	var out [][3]string
	if len(c.Before) > 0 || len(c.After) > 0 {
		keys := map[string]bool{}
		for k := range c.Before {
			keys[k] = true
		}
		for k := range c.After {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			was, now := c.Before[k], c.After[k]
			if k != "name" && !bytes.Equal(bytes.TrimSpace(was), bytes.TrimSpace(now)) {
				out = append(out, [3]string{k, valueText(k, was), valueText(k, now)})
			}
		}
	}
	if len(out) == 0 {
		cols := make([]string, 0, len(c.Fields))
		for k := range c.Fields {
			cols = append(cols, k)
		}
		sort.Strings(cols)
		stats := false
		for _, col := range cols {
			pair := c.Fields[col]
			if len(pair) != 2 {
				continue
			}
			if strings.HasPrefix(col, "StatModifier_") || strings.HasPrefix(col, "StatPercentEditor_") {
				if !stats {
					out = append(out, [3]string{"stats", "", ""})
					stats = true
				}
				continue
			}
			label := fieldLabels[col]
			if label == "" {
				label = col
			}
			out = append(out, [3]string{label, rawText(pair[0]), rawText(pair[1])})
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func (m *changeMatcher) data(c dataChange) changeItem {
	item := changeItem{Date: c.Date, Kind: "data", ID: c.ID, Fields: dataFields(c)}
	name := ""
	if m.world != nil {
		if e := m.world.byKey[c.Kind+":"+strconv.FormatInt(c.ID, 10)]; e != nil {
			name = e.en
		}
	}
	if name == "" {
		name = rawText(c.After["name"]) // a new item, named by Battle.net before the dataset knows it
	}
	item.Name = name
	switch c.Kind {
	case "items":
		if m.gear[c.ID] || m.bags[c.ID] {
			item.tier, item.Reason = tierMine, "item"
		}
	case "spells":
		if m.spells[c.ID] {
			item.tier, item.Reason = tierMine, "spell"
		} else if e := m.lookup("spells", c.ID); e != nil && c.Change == "changed" {
			if cls, _ := e.brief["class"].(string); cls != "" && cls == m.f.Class {
				item.tier, item.Reason = tierClass, "class"
			}
		}
	}
	if item.Name == "" {
		item.Name = fmt.Sprintf("%s %d", strings.TrimSuffix(c.Kind, "s"), c.ID)
	}
	return item
}

func (m *changeMatcher) lookup(kind string, id int64) *worldEntry {
	if m.world == nil {
		return nil
	}
	return m.world.byKey[kind+":"+strconv.FormatInt(id, 10)]
}

// matchChanges sorts the notes and data changes of the last two weeks into the three tiers, newest first.
func matchChanges(f changeFacts, feed *gameFeed, ch *changesFile, d *worldData, now time.Time) (mine, class, general []changeItem) {
	m := newChangeMatcher(f, d)
	var all []changeItem
	if feed != nil {
		for _, n := range parseNotes(feed.Days) {
			all = append(all, m.note(n))
		}
	}
	if ch != nil {
		since := now.AddDate(0, 0, -changesDays).Format("2006-01-02")
		for _, c := range ch.Changes {
			if c.Date >= since && c.ID > 0 {
				all = append(all, m.data(c))
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Date > all[j].Date })
	for _, it := range all {
		switch it.tier {
		case tierMine:
			mine = append(mine, it)
		case tierClass:
			class = append(class, it)
		case tierGeneral:
			general = append(general, it)
		}
	}
	return mine, class, general
}

// --- the card's answer ---------------------------------------------------------------------------------------------

type changesAnswer struct {
	Newest  string       `json:"newest,omitempty"`
	URL     string       `json:"url,omitempty"`
	Mine    []changeItem `json:"mine"`
	Class   []changeItem `json:"class"`
	General int          `json:"general"`
	More    int          `json:"more,omitempty"`
	Key     string       `json:"key,omitempty"`      // moves when the mine list does: the add-on announces it once
	Data    bool         `json:"data"`               // whether the backend's data changes were here to compare
	NoWorld bool         `json:"no_world,omitempty"` // names could not be resolved: the world dataset is missing
	NoNotes bool         `json:"no_notes,omitempty"` // Blizzard's notes could not be read
}

func answerChanges(req insightRequest, env insightEnv) (any, error) {
	var f changeFacts
	if len(req.Facts) == 0 || json.Unmarshal(req.Facts, &f) != nil {
		return nil, errors.New("the journal sent no character facts")
	}
	mine, class, general := matchChanges(f, env.Feed, env.Changes, env.World, time.Now())
	out := changesAnswer{Mine: []changeItem{}, Class: []changeItem{}, General: len(general), Data: env.Changes != nil,
		NoWorld: env.World == nil, NoNotes: env.Feed == nil}
	if env.Feed != nil {
		out.URL = env.Feed.URL
		if len(env.Feed.Days) > 0 {
			out.Newest = env.Feed.Days[0].Date
		}
	}
	h := sha1.New()
	for _, it := range mine {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\n", it.Date, it.Text, it.Name, it.ID)
	}
	if len(mine) > 0 {
		out.Key = hex.EncodeToString(h.Sum(nil)[:6])
	}
	for _, group := range []struct {
		from []changeItem
		to   *[]changeItem
	}{{mine, &out.Mine}, {class, &out.Class}} {
		for i, it := range group.from {
			if i >= changesShown {
				out.More += len(group.from) - changesShown
				break
			}
			*group.to = append(*group.to, it)
		}
	}
	return out, nil
}

var changesKind = &insightKind{
	version: "changes-1", ttl: time.Hour, feed: true, changes: true, perCharacter: true, maxData: 32 << 10,
	local: answerChanges,
}

// saveChangeFacts keeps the latest facts per character, so get_changes can match without the game asking again.
func saveChangeFacts(insightsDir string, req insightRequest) {
	if req.Character == "" || len(req.Facts) == 0 {
		return
	}
	path := changeFactsPath(insightsDir, req.Character)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		writeFileAtomic(path, req.Facts)
	}
}

func changeFactsPath(insightsDir, character string) string {
	sum := sha1.Sum([]byte(character))
	return filepath.Join(insightsDir, "changes-facts", hex.EncodeToString(sum[:10])+".json")
}

// --- the summary's shortlist ---------------------------------------------------------------------------------------

// changeShortlist is what the summary reads instead of the whole article: the notes and data changes matched to the
// character, and the general notes, but none about other classes, races, quests or places.
func changeShortlist(req insightRequest, env insightEnv) string {
	var f changeFacts
	json.Unmarshal(req.Facts, &f)
	mine, class, general := matchChanges(f, env.Feed, env.Changes, env.World, time.Now())
	var b strings.Builder
	section := func(title string, items []changeItem, limit int) {
		fmt.Fprintf(&b, "%s\n", title)
		if len(items) == 0 {
			b.WriteString("(none)\n")
		}
		for i, it := range items {
			if i >= limit {
				fmt.Fprintf(&b, "(%d more not shown)\n", len(items)-limit)
				break
			}
			if it.Kind == "data" {
				var parts []string
				for _, fl := range it.Fields {
					parts = append(parts, fmt.Sprintf("%s %s → %s", fl[0], fl[1], fl[2]))
				}
				fmt.Fprintf(&b, "- [%s] %s (%s): %s\n", it.Date, it.Name, it.Reason, strings.Join(parts, "; "))
				continue
			}
			fmt.Fprintf(&b, "- [%s] (%s) %s", it.Date, it.Category, it.Text)
			if it.dev != "" {
				fmt.Fprintf(&b, " %s", clip(it.dev, 400))
			}
			if it.Reason != "" {
				fmt.Fprintf(&b, " — matched: %s %s", it.Reason, it.Name)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	var notes, data []changeItem
	for _, it := range mine {
		if it.Kind == "data" {
			data = append(data, it)
		} else {
			notes = append(notes, it)
		}
	}
	section("Notes naming this character's quests, zone, items, spells or professions:", notes, 40)
	section("Notes for the character's class or race:", class, 40)
	section("General notes (no class, race, quest or place named):", general, 40)
	section("Data changes to the character's items and spells (game client numbers; the card already shows them):", data, 20)
	return b.String()
}

// --- the adviser's tool -------------------------------------------------------------------------------------------

func changesToolSchema() mcpObject {
	return mcpObject{
		"since": mcpObject{"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`, "description": "Oldest date to include, YYYY-MM-DD; default two weeks back"},
		"about": mcpObject{"type": "array", "maxItems": 10, "description": "Records to ask about regardless of the character, as search_game_data returns them",
			"items": mcpObject{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"}, "properties": mcpObject{
				"kind": mcpObject{"type": "string", "enum": []string{"items", "quests", "spells", "places"}},
				"id":   mcpObject{"type": "string", "pattern": `^[a-z]*-?[0-9]{1,9}$`, "maxLength": 24}}}},
		"mine": mcpObject{"type": "boolean", "description": "Match this character (default true): notes naming its quests, zone, items, spells, professions, class or race, and data changes to its items and spells"},
	}
}

func queryChanges(dir string, raw json.RawMessage) (mcpObject, error) {
	var a struct {
		ID    string `json:"snapshot_id"`
		Since string `json:"since"`
		About []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"about"`
		Mine *bool `json:"mine"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	now := time.Now()
	since := now.AddDate(0, 0, -changesDays).Format("2006-01-02")
	if a.Since != "" {
		if _, err := time.Parse("2006-01-02", a.Since); err != nil {
			return nil, errors.New("since must be a date as YYYY-MM-DD")
		}
		since = a.Since
	}
	if len(a.About) > 10 {
		return nil, errors.New("about lists at most 10 records")
	}
	mine := a.Mine == nil || *a.Mine
	if !mine && len(a.About) == 0 {
		return nil, errors.New("pass about, or leave mine true to match the character")
	}
	insightsDir := filepath.Join(filepath.Dir(dir), "insights")
	world, _ := loadWorld(dir)
	ch, _ := readChanges(worldDir(dir))
	var feed *gameFeed
	if body, err := os.ReadFile(filepath.Join(insightsDir, "feed.json")); err == nil {
		var g gameFeed
		if json.Unmarshal(body, &g) == nil {
			feed = &g
		}
	}
	out := mcpObject{"notes": []mcpObject{}, "data_changes": []mcpObject{}}
	var sources []mcpObject
	if feed != nil {
		sources = append(sources, mcpObject{"title": "WoW: Forever Hotfixes (Blizzard)", "url": feed.URL})
	} else {
		out["note"] = "Blizzard's hotfix notes have not been read on this computer yet; the journal's What changed card reads them."
	}
	if ch != nil {
		sources = append(sources, mcpObject{"title": "Game client data changes (numbers via wago.tools)", "built": ch.Built})
	}
	out["sources"] = sources
	add := func(it changeItem, tier string) {
		if it.Date < since {
			return
		}
		o := mcpObject{"date": it.Date, "tier": tier}
		if it.Reason != "" {
			o["reason"] = it.Reason
		}
		if it.Name != "" {
			o["name"] = it.Name
		}
		if it.Kind == "data" {
			o["id"], o["fields"] = it.ID, it.Fields
			out["data_changes"] = append(out["data_changes"].([]mcpObject), o)
			return
		}
		o["category"], o["text"] = it.Category, it.Text
		if it.dev != "" {
			o["developers_notes"] = clip(it.dev, 600)
		}
		out["notes"] = append(out["notes"].([]mcpObject), o)
	}
	if len(a.About) > 0 {
		want := map[string]bool{}
		for _, r := range a.About {
			want[r.Kind+":"+r.ID] = true
		}
		index := indexFor(world)
		if feed != nil {
			for _, n := range parseNotes(feed.Days) {
				for _, r := range index.scan(n.Text, false) {
					if want[r.e.kind+":"+r.e.id] {
						add(changeItem{Date: n.Date, Category: n.Category, Text: n.Text, Kind: "note", Name: r.e.en, dev: n.Dev}, "asked")
						break
					}
				}
			}
		}
		if ch != nil {
			m := newChangeMatcher(changeFacts{}, world)
			for _, c := range ch.Changes {
				if want[c.Kind+":"+strconv.FormatInt(c.ID, 10)] {
					add(m.data(c), "asked")
				}
			}
		}
		return out, nil
	}
	s, err := loadGameSnapshot(dir, a.ID)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(changeFactsPath(insightsDir, s.Character))
	if err != nil {
		out["note"] = "This character's facts are not here yet: they arrive when the journal's What changed card is answered, at login. Use about to ask about records."
		return out, nil
	}
	var f changeFacts
	json.Unmarshal(body, &f)
	m, c, g := matchChanges(f, feed, ch, world, now)
	for _, it := range m {
		add(it, tierMine)
	}
	for _, it := range c {
		add(it, tierClass)
	}
	general := 0
	for _, it := range g {
		if it.Date >= since {
			general++
		}
	}
	out["general_notes"] = general
	if general > 0 {
		out["general_hint"] = "General notes name no class, race, quest or place; read them on Blizzard's page when the player asks about something general."
	}
	return out, nil
}
