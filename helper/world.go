package main

// The world dataset: quests, NPCs, items, spells and places merged on the project's backend from ForeverDB's add-on
// data (GPL-3.0; its credit is "Data from ForeverDB.net (https://foreverdb.net)"), Blizzard's Game Data API and the
// numbers in the game client's tables, served by the project's server to players who signed in with Battle.net
// (backend_login.go). The tray downloads the files that changed once a day; search_game_data and get_game_data answer
// from them without going online.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultWorldMirror = defaultAuctionMirror + "/v1" // the same server as the auction mirror, for the same sign-in
	worldEvery         = 24 * time.Hour
	worldMaxAge        = 30 * 24 * time.Hour // Blizzard's API terms: its data is kept at most 30 days
	worldMaxFile       = 64 << 20
	maxWorldResults    = 25
	worldCredit        = "Data from ForeverDB.net (https://foreverdb.net)"
)

var (
	worldMirrorURL = func() string { return defaultWorldMirror }
	worldKinds     = []string{"items", "quests", "npcs", "spells", "places"}
	worldFileName  = regexp.MustCompile(`^[a-z]+(-[0-9]+)?-[0-9a-f]{16}\.json\.gz$`)
	worldIDPattern = regexp.MustCompile(`^[a-z]*-?[0-9]{1,9}$`)
	errWorldAbsent = errors.New("the world dataset is not on this computer yet: the companion downloads it from the project's server a minute after it starts. Use other sources meanwhile")
	errWorldSignIn = errors.New("the world dataset is not on this computer: it comes from the project's server, which needs a one-time Battle.net sign-in first. " + backendSignIn + " The dataset arrives a minute after the sign-in. Use other sources meanwhile")
)

func worldDir(snapshots string) string { return filepath.Join(filepath.Dir(snapshots), "world") }

type worldFile struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Records int    `json:"records"`
}

type worldManifest struct {
	Format  int      `json:"format"`
	Built   string   `json:"built"`
	Credits []string `json:"credits"`
	Sources struct {
		Battlenet struct {
			Oldest string `json:"oldest"`
		} `json:"battlenet"`
	} `json:"sources"`
	Files []worldFile `json:"files"`
}

// expired tells whether the Battle.net data in the dataset is older than Blizzard lets anyone keep it.
func (m *worldManifest) expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, m.Sources.Battlenet.Oldest)
	return err == nil && now.Sub(t) > worldMaxAge
}

func (m *worldManifest) check() error {
	if m.Format != 1 {
		return fmt.Errorf("the world dataset has format %d, this companion reads format 1", m.Format)
	}
	for _, f := range m.Files {
		if !strings.HasPrefix(f.Path, "world/") || !worldFileName.MatchString(path.Base(f.Path)) || !slices.Contains(worldKinds, f.Kind) ||
			f.Bytes <= 0 || f.Bytes > worldMaxFile || len(f.SHA256) != 64 {
			return fmt.Errorf("the world dataset lists an unexpected file %q", f.Path)
		}
	}
	return nil
}

type worldWatcher struct {
	dir  string
	http *http.Client
	base func() string
	now  func() time.Time
	lost func() // after the server turned the sign-in token down
}

// refresh downloads a new manifest and the files it lists that this computer lacks, at most once a day, and removes
// what the manifest no longer lists. Files are named by their content, so one already here is the same file. A stored
// copy whose Battle.net data is older than 30 days goes first, whether or not the mirror answers.
func (w *worldWatcher) refresh(ctx context.Context) error {
	if err := w.dropExpired(); err != nil {
		return err
	}
	manifestPath := filepath.Join(w.dir, "manifest.json")
	if info, err := os.Stat(manifestPath); err == nil && w.now().Sub(info.ModTime()) < worldEvery {
		return nil
	}
	base := strings.TrimSuffix(w.base(), "/")
	if base == "" || backendToken() == "" {
		return nil // without the sign-in the tools say how to get it
	}
	body, err := w.get(ctx, base+"/world/manifest.json", 1<<20)
	if err != nil {
		return err
	}
	var m worldManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("the world dataset's manifest is unreadable: %w", err)
	}
	if err := m.check(); err != nil {
		return err
	}
	if m.expired(w.now()) {
		return errors.Join(errors.New("the world dataset on the mirror is older than 30 days"), w.remove(nil))
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, f := range m.Files {
		name := path.Base(f.Path)
		keep[name] = true
		if _, err := os.Stat(filepath.Join(w.dir, name)); err == nil {
			continue
		}
		if err := w.download(ctx, base+"/"+f.Path, filepath.Join(w.dir, name), f); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
	}
	if err := writeFileAtomic(manifestPath, body); err != nil {
		return err
	}
	return w.remove(keep)
}

// remove deletes the dataset's files except those in keep; with keep nil it deletes the manifest too.
func (w *worldWatcher) remove(keep map[string]bool) error {
	entries, err := os.ReadDir(w.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if (strings.HasSuffix(name, ".json.gz") && !keep[name]) || (keep == nil && name == "manifest.json") {
			errs = append(errs, os.Remove(filepath.Join(w.dir, name)))
		}
	}
	return errors.Join(errs...)
}

func (w *worldWatcher) dropExpired() error {
	m, err := readWorldManifest(w.dir)
	if err != nil || !m.expired(w.now()) {
		return nil
	}
	logf("World data: the stored dataset is older than 30 days; removing it")
	return w.remove(nil)
}

func (w *worldWatcher) request(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	resp, err := w.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return resp, nil
}

// do sends a request to the server with the sign-in token. A refused token is forgotten, so that the menu offers the
// sign-in again.
func (w *worldWatcher) do(req *http.Request) (*http.Response, error) {
	token := backendToken()
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		dropBackendToken(token, w.lost)
		return nil, errSignInNeeded
	}
	return resp, nil
}

func (w *worldWatcher) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := w.request(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(body)) > limit {
		err = fmt.Errorf("%s is larger than expected", url)
	}
	return body, err
}

func (w *worldWatcher) download(ctx context.Context, url, dest string, f worldFile) error {
	resp, err := w.request(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(w.dir, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, f.Bytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return err
	case n != f.Bytes:
		return fmt.Errorf("got %d bytes instead of %d", n, f.Bytes)
	case hex.EncodeToString(h.Sum(nil)) != f.SHA256:
		return errors.New("the download does not match the manifest's SHA-256")
	}
	return replaceFile(tmp.Name(), dest)
}

// watchWorld runs in the tray for its whole life: a minute after start, then every hour, it lets refresh decide
// whether a day has passed.
func (a *app) watchWorld() {
	w := &worldWatcher{dir: worldDir(snapshotDir()), http: &http.Client{Timeout: 5 * time.Minute}, base: worldMirrorURL, now: time.Now,
		lost: a.noteBackendToken}
	wait := time.NewTimer(time.Minute)
	defer wait.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-worldWake:
		case <-wait.C:
		}
		if err := w.refresh(a.ctx); err != nil {
			logf("World data: %v", err)
		}
		if err := w.refreshChanges(a.ctx); err != nil {
			logf("Changes feed: %v", err)
		}
		wait.Reset(time.Hour)
	}
}

// --- reading and searching -------------------------------------------------------------------------------------

// worldEntry is what search knows about one record: its names, a few facts that tell it apart from others of the
// same name, and what the filters match.
type worldEntry struct {
	kind, id, en string
	names        map[string]string // locale -> name
	folded       map[string]string // locale -> foldName(name)
	brief        mcpObject
	zones        []string // folded zone names
	lo, hi       int      // level range; 0 when unknown
	side         string   // Alliance, Horde, or empty for both or unknown
	roles        []string
	about        string    // folded name with title, trains, roles, type, slot, class or profession, for searches by what it is
	where        mcpObject // an NPC's zone and first known spot, shown wherever a record refers to it
}

type worldData struct {
	manifest worldManifest
	stamp    time.Time
	records  map[string]map[string]json.RawMessage // kind -> id -> record
	entries  []*worldEntry
	byKey    map[string]*worldEntry // kind + ":" + id
}

var worldCache struct {
	sync.Mutex
	data *worldData
}

func readWorldManifest(dir string) (*worldManifest, error) {
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m worldManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return &m, m.check()
}

// loadWorld reads the dataset once and again whenever the tray stores a new manifest.
func loadWorld(snapshots string) (*worldData, error) { return loadWorldFrom(worldDir(snapshots)) }

// loadWorldForTool is loadWorld for the adviser's tools: a missing dataset says how to get it when the sign-in is what
// it waits for.
func loadWorldForTool(snapshots string) (*worldData, error) {
	d, err := loadWorld(snapshots)
	if errors.Is(err, errWorldAbsent) && backendToken() == "" {
		err = errWorldSignIn
	}
	return d, err
}

func loadWorldFrom(dir string) (*worldData, error) {
	info, err := os.Stat(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, errWorldAbsent
	}
	worldCache.Lock()
	defer worldCache.Unlock()
	if d := worldCache.data; d != nil && d.stamp.Equal(info.ModTime()) {
		return d, nil
	}
	m, err := readWorldManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("the world dataset on this computer is unreadable: %w", err)
	}
	if m.expired(time.Now()) {
		return nil, errWorldAbsent
	}
	d := &worldData{manifest: *m, stamp: info.ModTime(), records: map[string]map[string]json.RawMessage{}, byKey: map[string]*worldEntry{}}
	for _, f := range m.Files {
		var part struct {
			Records map[string]json.RawMessage `json:"records"`
		}
		if err := readGzipJSON(filepath.Join(dir, path.Base(f.Path)), &part); err != nil {
			return nil, fmt.Errorf("the world dataset's %s: %w", path.Base(f.Path), err)
		}
		if d.records[f.Kind] == nil {
			d.records[f.Kind] = map[string]json.RawMessage{}
		}
		for id, rec := range part.Records {
			d.records[f.Kind][id] = rec
		}
	}
	d.describe()
	worldCache.data = d
	return d, nil
}

func readGzipJSON(file string, v any) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer z.Close()
	return json.NewDecoder(z).Decode(v)
}

var (
	nameSpace    = regexp.MustCompile(`[\s\p{P}]+`)
	qualityNames = []string{"Poor", "Common", "Uncommon", "Rare", "Epic", "Legendary", "Artifact", "Heirloom"}
	slotNames    = map[int]string{1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt", 5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet",
		9: "Wrist", 10: "Hands", 11: "Finger", 12: "Trinket", 13: "One-Hand", 14: "Off Hand", 15: "Ranged", 16: "Back",
		17: "Two-Hand", 18: "Bag", 19: "Tabard", 20: "Chest", 21: "Main Hand", 22: "Off Hand", 23: "Held In Off-hand",
		24: "Ammo", 25: "Thrown", 26: "Ranged", 28: "Relic"}
)

// foldName makes names comparable across case, punctuation and the Russian ё.
func foldName(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.TrimSpace(nameSpace.ReplaceAllString(s, " "))
}

func sideOf(s string) string {
	switch strings.ToLower(s) {
	case "alliance", "a":
		return "Alliance"
	case "horde", "h":
		return "Horde"
	}
	return ""
}

func levelRange(s string) (int, int) {
	lo, hi, _ := strings.Cut(s, "-")
	a, _ := strconv.Atoi(strings.TrimSpace(lo))
	b, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil {
		b = a
	}
	return a, b
}

// describe indexes every named record: places first, so that NPCs, quests and flight masters can name their zone, and
// NPCs before quests, so that a quest can name its giver's zone.
func (d *worldData) describe() {
	zoneNames := map[int]string{}
	for id, rec := range d.records["places"] {
		if n, ok := strings.CutPrefix(id, "zone-"); ok {
			var r struct {
				Name map[string]string `json:"name"`
			}
			json.Unmarshal(rec, &r)
			if m, err := strconv.Atoi(n); err == nil && r.Name["en_US"] != "" {
				zoneNames[m] = r.Name["en_US"]
			}
		}
	}
	npcMaps := map[int]int{}
	for _, kind := range []string{"places", "npcs", "quests", "items", "spells"} {
		ids := make([]string, 0, len(d.records[kind]))
		for id := range d.records[kind] {
			ids = append(ids, id)
		}
		slices.SortFunc(ids, compareIDs)
		for _, id := range ids {
			rec := d.records[kind][id]
			var named struct {
				Name map[string]string `json:"name"`
			}
			json.Unmarshal(rec, &named)
			e := &worldEntry{kind: kind, id: id, en: named.Name["en_US"], names: named.Name, folded: map[string]string{}, brief: mcpObject{}}
			for l, s := range named.Name {
				e.folded[l] = foldName(s)
			}
			switch kind {
			case "places":
				describePlace(e, rec, zoneNames)
			case "npcs":
				if m := describeNPC(e, rec, zoneNames); m != 0 {
					n, _ := strconv.Atoi(id)
					npcMaps[n] = m
				}
			case "quests":
				describeQuest(e, rec, zoneNames, npcMaps)
			case "items":
				describeItem(e, rec)
			case "spells":
				describeSpell(e, rec)
			}
			if len(e.names) == 0 {
				continue
			}
			about := []string{e.en}
			for _, k := range []string{"title", "type", "slot", "class", "profession"} {
				if s, ok := e.brief[k].(string); ok {
					about = append(about, s)
				}
			}
			if trains, ok := e.brief["trains"].([]string); ok {
				about = append(about, trains...)
			}
			about = append(about, e.roles...)
			e.about = foldName(strings.Join(about, " "))
			d.entries = append(d.entries, e)
			d.byKey[kind+":"+id] = e
		}
	}
}

func compareIDs(a, b string) int {
	x, errX := strconv.Atoi(a)
	y, errY := strconv.Atoi(b)
	if errX == nil && errY == nil {
		return x - y
	}
	return strings.Compare(a, b)
}

func (e *worldEntry) addZone(name string) {
	if name == "" {
		return
	}
	if _, ok := e.brief["zone"]; !ok {
		e.brief["zone"] = name
	}
	if f := foldName(name); !slices.Contains(e.zones, f) {
		e.zones = append(e.zones, f)
	}
}

func (e *worldEntry) setLevels(lo, hi int) {
	e.lo, e.hi = lo, hi
	if hi < lo {
		e.hi = lo
	}
}

func describePlace(e *worldEntry, rec json.RawMessage, zones map[int]string) {
	var r struct {
		Foreverdb struct {
			Zone *struct {
				Levels string `json:"levels"`
				Side   string `json:"side"`
			} `json:"zone"`
			Flight *struct {
				UIMap int    `json:"ui_map"`
				Side  string `json:"side"`
			} `json:"flight"`
			Dungeon *struct {
				Kind   string `json:"kind"`
				Zone   string `json:"zone"`
				Levels []int  `json:"levels"`
				Size   int    `json:"size"`
			} `json:"dungeon"`
		} `json:"foreverdb"`
	}
	json.Unmarshal(rec, &r)
	f := r.Foreverdb
	switch {
	case f.Zone != nil:
		e.roles = []string{"zone"}
		e.brief["type"] = "zone"
		e.addZone(e.en)
		if f.Zone.Levels != "" {
			e.setLevels(levelRange(f.Zone.Levels))
			e.brief["levels"] = f.Zone.Levels
		}
		e.side = sideOf(f.Zone.Side)
	case f.Flight != nil:
		e.roles = []string{"flight"}
		e.brief["type"] = "flight master"
		e.addZone(zones[f.Flight.UIMap])
		e.side = sideOf(f.Flight.Side)
	case f.Dungeon != nil:
		e.roles = []string{"dungeon"}
		e.brief["type"] = cmpOr(f.Dungeon.Kind, "dungeon")
		e.addZone(f.Dungeon.Zone)
		if len(f.Dungeon.Levels) == 2 {
			e.setLevels(f.Dungeon.Levels[0], f.Dungeon.Levels[1])
			e.brief["levels"] = fmt.Sprintf("%d-%d", f.Dungeon.Levels[0], f.Dungeon.Levels[1])
		}
	}
	if e.side != "" {
		e.brief["side"] = e.side
	}
}

func describeNPC(e *worldEntry, rec json.RawMessage, zones map[int]string) int {
	type spot struct {
		Map int     `json:"map"`
		X   float64 `json:"x"`
		Y   float64 `json:"y"`
	}
	var r struct {
		Foreverdb struct {
			Title        string            `json:"title"`
			Side         string            `json:"side"`
			Place        *spot             `json:"place"`
			Spots        []spot            `json:"spots"`
			ClassicSpots []spot            `json:"classic_spots"`
			Vendor       json.RawMessage   `json:"vendor"`
			Sells        []json.RawMessage `json:"sells"`
			Trains       []string          `json:"trains"`
			Rare         *struct {
				Level  []int  `json:"level"`
				Spawns string `json:"spawns"`
			} `json:"rare"`
			Boss *struct {
				Instance string `json:"instance"`
			} `json:"boss"`
		} `json:"foreverdb"`
	}
	json.Unmarshal(rec, &r)
	f := r.Foreverdb
	var at *spot
	switch {
	case f.Place != nil:
		at = f.Place
	case len(f.Spots) > 0:
		at = &f.Spots[0]
	case len(f.ClassicSpots) > 0:
		at = &f.ClassicSpots[0]
	case f.Rare != nil && f.Rare.Spawns != "":
		// "1429:52.7,58.9;28.6,59.9;…": the rare's map and its spawn points.
		mapID, points, _ := strings.Cut(f.Rare.Spawns, ":")
		first, _, _ := strings.Cut(points, ";")
		x, y, _ := strings.Cut(first, ",")
		s := spot{}
		s.Map, _ = strconv.Atoi(mapID)
		s.X, _ = strconv.ParseFloat(x, 64)
		s.Y, _ = strconv.ParseFloat(y, 64)
		at = &s
	}
	m := 0
	if at != nil {
		m = at.Map
		e.where = mcpObject{"map": at.Map, "x": at.X, "y": at.Y}
		if z := zones[at.Map]; z != "" {
			e.where["zone"] = z
		}
	}
	if f.Title != "" {
		e.brief["title"] = f.Title
	}
	e.addZone(zones[m])
	if f.Boss != nil {
		e.addZone(f.Boss.Instance)
		e.roles = append(e.roles, "boss")
		e.brief["boss_of"] = f.Boss.Instance
	}
	e.side = sideOf(f.Side)
	if e.side != "" {
		e.brief["side"] = e.side
	}
	if len(f.Vendor) > 0 || len(f.Sells) > 0 {
		e.roles = append(e.roles, "vendor")
	}
	if len(f.Trains) > 0 {
		e.roles = append(e.roles, "trainer")
		e.brief["trains"] = f.Trains[:min(len(f.Trains), 3)]
	}
	if f.Rare != nil {
		e.roles = append(e.roles, "rare")
		if len(f.Rare.Level) == 2 {
			e.setLevels(f.Rare.Level[0], f.Rare.Level[1])
			e.brief["level"] = f.Rare.Level
		}
	}
	if len(e.roles) > 0 {
		e.brief["roles"] = e.roles
	}
	return m
}

func describeQuest(e *worldEntry, rec json.RawMessage, zones map[int]string, npcMaps map[int]int) {
	type part struct {
		Level    int    `json:"level"`
		MinLevel int    `json:"min_level"`
		Side     string `json:"side"`
		Starts   []struct {
			NPC  int    `json:"npc"`
			Name string `json:"name"`
		} `json:"starts"`
		Flags []string `json:"flags"`
	}
	var r struct {
		Battlenet *struct {
			Area *struct {
				Name string `json:"name"`
			} `json:"area"`
		} `json:"battlenet"`
		Foreverdb struct {
			Forever *part `json:"forever"`
			Classic *part `json:"classic"`
			Found   *struct {
				Zone  string `json:"zone"`
				Giver string `json:"giver"`
			} `json:"found"`
		} `json:"foreverdb"`
	}
	json.Unmarshal(rec, &r)
	f := r.Foreverdb
	q := f.Forever
	if q == nil {
		q = f.Classic
	}
	if q != nil {
		e.setLevels(q.Level, q.Level)
		if q.Level > 0 {
			e.brief["level"] = q.Level
		}
		if q.MinLevel > 0 {
			e.brief["min_level"] = q.MinLevel
		}
		e.side = sideOf(q.Side)
		if e.side == "" && f.Classic != nil {
			e.side = sideOf(f.Classic.Side)
		}
		starts := q.Starts
		if len(starts) == 0 && f.Classic != nil {
			starts = f.Classic.Starts
		}
		if len(starts) > 0 {
			if starts[0].Name != "" {
				e.brief["giver"] = starts[0].Name
			}
			e.addZone(zones[npcMaps[starts[0].NPC]])
		}
		if f.Classic != nil && len(f.Classic.Flags) > 0 {
			e.brief["flags"] = f.Classic.Flags
		}
	}
	if f.Found != nil {
		e.addZone(f.Found.Zone)
		if _, ok := e.brief["giver"]; !ok && f.Found.Giver != "" {
			e.brief["giver"] = f.Found.Giver
		}
	}
	if r.Battlenet != nil && r.Battlenet.Area != nil {
		e.addZone(r.Battlenet.Area.Name)
	}
	if e.side != "" {
		e.brief["side"] = e.side
	}
}

func describeItem(e *worldEntry, rec json.RawMessage) {
	var r struct {
		Client *struct {
			Quality       *int `json:"quality"`
			ItemLevel     int  `json:"item_level"`
			RequiredLevel int  `json:"required_level"`
			InventoryType int  `json:"inventory_type"`
		} `json:"client"`
		Battlenet *struct {
			Quality      string `json:"quality"`
			ItemSubclass *struct {
				Name string `json:"name"`
			} `json:"item_subclass"`
		} `json:"battlenet"`
	}
	json.Unmarshal(rec, &r)
	if r.Battlenet != nil {
		if r.Battlenet.Quality != "" {
			e.brief["quality"] = r.Battlenet.Quality
		}
		if r.Battlenet.ItemSubclass != nil && r.Battlenet.ItemSubclass.Name != "" {
			e.brief["type"] = r.Battlenet.ItemSubclass.Name
		}
	}
	if c := r.Client; c != nil {
		if _, ok := e.brief["quality"]; !ok && c.Quality != nil && *c.Quality >= 0 && *c.Quality < len(qualityNames) {
			e.brief["quality"] = qualityNames[*c.Quality]
		}
		if c.ItemLevel > 0 {
			e.brief["item_level"] = c.ItemLevel
		}
		if c.RequiredLevel > 1 {
			e.brief["required_level"] = c.RequiredLevel
		}
		if s := slotNames[c.InventoryType]; s != "" {
			e.brief["slot"] = s
		}
		e.setLevels(c.RequiredLevel, c.RequiredLevel)
	}
}

func describeSpell(e *worldEntry, rec json.RawMessage) {
	var r struct {
		Client *struct {
			Levels *struct {
				Base int `json:"base"`
			} `json:"levels"`
		} `json:"client"`
		Foreverdb *struct {
			ClassSpell *struct {
				Class string `json:"class"`
				Level int    `json:"level"`
				Rank  string `json:"rank"`
			} `json:"class_spell"`
			Recipe *struct {
				Profession string `json:"profession"`
				Skill      int    `json:"skill"`
			} `json:"recipe"`
		} `json:"foreverdb"`
	}
	json.Unmarshal(rec, &r)
	if r.Client != nil && r.Client.Levels != nil && r.Client.Levels.Base > 0 {
		e.setLevels(r.Client.Levels.Base, r.Client.Levels.Base)
	}
	if f := r.Foreverdb; f != nil {
		if c := f.ClassSpell; c != nil {
			if c.Rank != "" {
				e.brief["rank"] = c.Rank
			}
			e.brief["class"] = c.Class
			e.brief["level"] = c.Level
			e.setLevels(c.Level, c.Level)
			e.roles = append(e.roles, "class")
		}
		if c := f.Recipe; c != nil {
			e.brief["profession"] = c.Profession
			e.brief["skill"] = c.Skill
			e.roles = append(e.roles, "recipe")
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// worldFilter narrows a search: zone (a name or a uiMap number), a level range, a side and a role.
type worldFilter struct {
	zone           string
	minLvl, maxLvl int
	side, role     string
}

func (f worldFilter) empty() bool {
	return f.zone == "" && f.minLvl == 0 && f.maxLvl == 0 && f.role == ""
}

func (f worldFilter) match(e *worldEntry) bool {
	if f.zone != "" && !slices.ContainsFunc(e.zones, func(z string) bool { return strings.Contains(z, f.zone) }) {
		return false
	}
	if (f.minLvl > 0 || f.maxLvl > 0) && e.hi == 0 {
		return false
	}
	if f.minLvl > 0 && e.hi < f.minLvl {
		return false
	}
	if f.maxLvl > 0 && e.lo > f.maxLvl {
		return false
	}
	if f.side != "" && e.side != "" && e.side != f.side {
		return false
	}
	return f.role == "" || slices.Contains(e.roles, f.role)
}

type worldHit struct {
	score int
	e     *worldEntry
	match string
	loc   string
}

func (d *worldData) search(query, kind string, f worldFilter, limit int) (out []mcpObject, total int) {
	q := foldName(query)
	words := strings.Fields(q)
	var hits []worldHit
	for _, e := range d.entries {
		if (kind != "" && e.kind != kind) || !f.match(e) {
			continue
		}
		best := worldHit{e: e}
		if q == "" {
			best.score = 1
		} else if e.id == q {
			best.score = 120
		}
		for loc, s := range e.folded {
			score := 0
			switch {
			case q == "":
			case s == q:
				score = 100
			case strings.HasPrefix(s, q):
				score = 80
			case len(words) > 0 && containsAll(s, words):
				score = 60
			}
			if score > best.score || (score == best.score && score > 0 && loc == "en_US") {
				best = worldHit{score: score, e: e, match: e.names[loc], loc: loc}
			}
		}
		if best.score == 0 && len(words) > 0 && containsAll(e.about, words) {
			best = worldHit{score: 40, e: e} // "alchemy trainer", "plate legs", "mage"
		}
		if best.score > 0 {
			hits = append(hits, best)
		}
	}
	slices.SortFunc(hits, func(a, b worldHit) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if q == "" && a.e.lo != b.e.lo {
			return a.e.lo - b.e.lo
		}
		if la, lb := len(a.e.en), len(b.e.en); q != "" && la != lb {
			return la - lb
		}
		if a.e.kind != b.e.kind {
			return slices.Index(worldKinds, a.e.kind) - slices.Index(worldKinds, b.e.kind)
		}
		return compareIDs(a.e.id, b.e.id)
	})
	out = []mcpObject{}
	for _, h := range hits[:min(len(hits), limit)] {
		r := mcpObject{"kind": h.e.kind, "id": h.e.id}
		if h.e.en != "" {
			r["name"] = h.e.en
		}
		if h.loc != "" && h.loc != "en_US" && h.match != h.e.en {
			r["matched"] = h.match
		}
		for k, v := range h.e.brief {
			r[k] = v
		}
		out = append(out, r)
	}
	return out, len(hits)
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// named adds names beside the IDs a record refers to, so that the adviser needs no further call to read them.
func (d *worldData) named(v any, key string) any {
	nameOf := func(kind string, id any) string {
		n, ok := id.(float64)
		if !ok {
			return ""
		}
		if e := d.byKey[kind+":"+strconv.Itoa(int(n))]; e != nil {
			return e.en
		}
		return ""
	}
	idList := map[string]string{"needs_all": "quests", "needs_one_of": "quests", "quests": "quests", "set_items": "items",
		"flight_nodes": "places:flight-", "dungeons": "places:dungeon-"}
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			x[k] = d.named(child, k)
		}
		for field, kind := range map[string]string{"item": "items", "npc": "npcs", "spell": "spells"} {
			if _, has := x["name"]; !has {
				if n := nameOf(kind, x[field]); n != "" {
					x["name"] = n
				}
			}
		}
		if id, ok := x["npc"].(float64); ok {
			if e := d.byKey["npcs:"+strconv.Itoa(int(id))]; e != nil && e.where != nil {
				x["where"] = e.where
			}
		}
		for field, kind := range map[string]string{"makes": "items", "trigger": "spells", "previous": "quests", "quest_id": "quests", "starts_quest": "quests"} {
			if n := nameOf(kind, x[field]); n != "" {
				x[field+"_name"] = n
			}
		}
		return x
	case []any:
		if key == "reagents" {
			for i, pair := range x {
				if p, ok := pair.([]any); ok && len(p) == 2 {
					x[i] = map[string]any{"item": p[0], "name": nameOf("items", p[0]), "count": p[1]}
				}
			}
			return x
		}
		if kind, ok := idList[key]; ok {
			prefixKind, prefix, _ := strings.Cut(kind, ":")
			for i, id := range x {
				n, isNum := id.(float64)
				if !isNum {
					continue
				}
				name := ""
				if prefix != "" {
					if e := d.byKey[prefixKind+":"+prefix+strconv.Itoa(int(n))]; e != nil {
						name = e.en
					}
				} else {
					name = nameOf(prefixKind, id)
				}
				if name != "" {
					x[i] = map[string]any{"id": id, "name": name}
				}
			}
			return x
		}
		for i, child := range x {
			x[i] = d.named(child, key)
		}
		return x
	}
	return v
}

// --- tools -----------------------------------------------------------------------------------------------------

const maxWorldRefs = 10

var worldRoles = []string{"vendor", "trainer", "rare", "boss", "class", "recipe", "zone", "flight", "dungeon"}

func worldSearchSchema() mcpObject {
	return mcpObject{
		"query":       mcpObject{"type": "string", "maxLength": 100, "description": "Name or part of it in any game language, or an ID; also matches what a record is in English (an NPC's title or what it trains, an item's type or slot, a spell's class or profession), e.g. \"alchemy trainer\". May be left out when kind and a filter are given"},
		"kind":        mcpObject{"type": "string", "enum": worldKinds},
		"zone":        mcpObject{"type": "string", "maxLength": 60, "description": "Zone, city or instance name in English (part of it is enough), or a uiMap number"},
		"min_level":   mcpObject{"type": "integer", "minimum": 1, "maximum": 100, "description": "Lowest level: a quest's level, an item's required level, a spell's learning level, a rare's or zone's level range"},
		"max_level":   mcpObject{"type": "integer", "minimum": 1, "maximum": 100, "description": "Highest level, read like min_level"},
		"side":        mcpObject{"type": "string", "enum": []string{"Alliance", "Horde"}, "description": "Leaves out what only the other faction can use"},
		"role":        mcpObject{"type": "string", "enum": worldRoles, "description": "NPCs: vendor, trainer, rare, boss. Spells: class, recipe. Places: zone, flight, dungeon"},
		"max_results": mcpObject{"type": "integer", "minimum": 1, "maximum": maxWorldResults, "description": "1–25, default 10"},
	}
}

func worldGetSchema() mcpObject {
	ref := mcpObject{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"}, "properties": mcpObject{
		"kind": mcpObject{"type": "string", "enum": worldKinds},
		"id":   mcpObject{"type": "string", "pattern": `^[a-z]*-?[0-9]{1,9}$`, "maxLength": 24, "description": "As search_game_data returns it, e.g. 19019 or zone-1429"},
	}}
	return mcpObject{"refs": mcpObject{"type": "array", "minItems": 1, "maxItems": maxWorldRefs, "items": ref}}
}

func queryWorldSearch(dir string, raw []byte) (mcpObject, error) {
	var a struct {
		ID       string `json:"snapshot_id"`
		Query    string `json:"query"`
		Kind     string `json:"kind"`
		Zone     string `json:"zone"`
		MinLevel int    `json:"min_level"`
		MaxLevel int    `json:"max_level"`
		Side     string `json:"side"`
		Role     string `json:"role"`
		Limit    int    `json:"max_results"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	f := worldFilter{zone: foldName(a.Zone), minLvl: a.MinLevel, maxLvl: a.MaxLevel, side: a.Side, role: a.Role}
	switch {
	case len([]rune(a.Query)) > 100 || (a.Kind != "" && !slices.Contains(worldKinds, a.Kind)) || a.Limit < 0 || a.Limit > maxWorldResults:
		return nil, fmt.Errorf("query is at most 100 characters, kind one of %s and max_results 1 to %d", strings.Join(worldKinds, ", "), maxWorldResults)
	case a.Side != "" && a.Side != "Alliance" && a.Side != "Horde", a.Role != "" && !slices.Contains(worldRoles, a.Role),
		a.MinLevel < 0, a.MaxLevel < 0, a.MaxLevel > 0 && a.MinLevel > a.MaxLevel:
		return nil, fmt.Errorf("side is Alliance or Horde, role one of %s, and min_level no higher than max_level", strings.Join(worldRoles, ", "))
	case strings.TrimSpace(a.Query) == "" && (a.Kind == "" || f.empty()):
		return nil, errors.New("give a query, or a kind with at least one of zone, min_level, max_level and role (for example kind quests, zone Westfall, min_level 10, max_level 14)")
	}
	d, err := loadWorldForTool(dir)
	if err != nil {
		return nil, err
	}
	if n, err := strconv.Atoi(f.zone); err == nil {
		if e := d.byKey["places:zone-"+strconv.Itoa(n)]; e != nil {
			f.zone = foldName(e.en)
		}
	}
	limit := a.Limit
	if limit == 0 {
		limit = 10
	}
	results, total := d.search(a.Query, a.Kind, f, limit)
	out := mcpObject{"captured": d.manifest.Built, "results": results, "total": total}
	switch {
	case total == 0:
		out["note"] = "Nothing matches. Try another spelling, the English name, a part of the name, an ID, or fewer filters."
	case total > len(results):
		out["note"] = fmt.Sprintf("%d more match; narrow with kind, zone, min_level/max_level, side or role, or raise max_results.", total-len(results))
	}
	return out, nil
}

func queryWorldGet(dir string, raw []byte) (mcpObject, error) {
	var a struct {
		ID   string `json:"snapshot_id"`
		Refs []struct {
			Kind string          `json:"kind"`
			Key  json.RawMessage `json:"id"`
		} `json:"refs"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	if len(a.Refs) == 0 || len(a.Refs) > maxWorldRefs {
		return nil, fmt.Errorf("refs lists 1 to %d records as {kind, id}", maxWorldRefs)
	}
	d, err := loadWorldForTool(dir)
	if err != nil {
		return nil, err
	}
	var records, missing []mcpObject
	credit := false
	for _, ref := range a.Refs {
		key := strings.Trim(string(bytes.TrimSpace(ref.Key)), `"`)
		if !slices.Contains(worldKinds, ref.Kind) || !worldIDPattern.MatchString(key) {
			return nil, fmt.Errorf("each ref needs kind (one of %s) and id as search_game_data returns it", strings.Join(worldKinds, ", "))
		}
		rec, ok := d.records[ref.Kind][key]
		if !ok {
			missing = append(missing, mcpObject{"kind": ref.Kind, "id": key})
			continue
		}
		var v any
		if err := json.Unmarshal(rec, &v); err != nil {
			return nil, err
		}
		records = append(records, mcpObject{"kind": ref.Kind, "id": key, "record": d.named(v, "")})
		credit = credit || bytes.Contains(rec, []byte(`"foreverdb"`))
	}
	out := mcpObject{"captured": d.manifest.Built, "records": records}
	if len(missing) > 0 {
		out["missing"] = missing
		out["note"] = "The world dataset has no record for the missing refs; find them with search_game_data."
	}
	if credit {
		out["credit"] = worldCredit
	}
	return out, nil
}
