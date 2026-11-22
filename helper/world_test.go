package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWorld serves a world dataset like data.lfguidance.com: one file per kind and the manifest listing them.
type fakeWorld struct {
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte
	manifest worldManifest
	asked    []string
}

func newFakeWorld(t *testing.T, kinds map[string]map[string]any, oldest string) *fakeWorld {
	f := &fakeWorld{files: map[string][]byte{}}
	f.set(t, kinds, oldest)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asked = append(f.asked, r.URL.Path)
		if r.URL.Path == "/world/manifest.json" {
			json.NewEncoder(w).Encode(f.manifest)
			return
		}
		body, ok := f.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWorld) set(t *testing.T, kinds map[string]map[string]any, oldest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := worldManifest{Format: 1, Built: "2026-11-22T10:00:00Z", Credits: []string{worldCredit}}
	m.Sources.Battlenet.Oldest = oldest
	for _, kind := range worldKinds {
		records, ok := kinds[kind]
		if !ok {
			continue
		}
		raw, _ := json.Marshal(map[string]any{"format": 1, "kind": kind, "legend": map[string]string{"client": "numbers"}, "records": records})
		var gz bytes.Buffer
		z := gzip.NewWriter(&gz)
		z.Write(raw)
		z.Close()
		name := sha256.Sum256(raw)
		sum := sha256.Sum256(gz.Bytes())
		p := "world/" + kind + "-" + hex.EncodeToString(name[:])[:16] + ".json.gz"
		f.files[p] = gz.Bytes()
		m.Files = append(m.Files, worldFile{Kind: kind, Path: p, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(gz.Len()), Records: len(records)})
	}
	f.manifest = m
}

var testWorldRecords = map[string]map[string]any{
	"items": {
		"118": map[string]any{"name": map[string]string{"en_US": "Minor Healing Potion", "ru_RU": "Слабое лечебное зелье"},
			"client":    map[string]any{"class": 0, "subclass": 1, "inventory_type": 0, "quality": 1, "item_level": 5, "required_level": 1},
			"foreverdb": map[string]any{"facts": map[string]any{"drop": map[string]any{"source": "Kobold Tunneler", "chance": 1.43}}}},
		"14158": map[string]any{"name": map[string]string{"en_US": "Embroidered Robe"}, "client": map[string]any{"class": 4, "subclass": 1, "inventory_type": 20, "quality": 2, "item_level": 22, "required_level": 17}},
		"1811":  map[string]any{"name": map[string]string{"en_US": "Blunt Claymore"}, "client": map[string]any{"class": 2, "subclass": 8, "inventory_type": 17}},
		"2589":  map[string]any{"name": map[string]string{"en_US": "Linen Cloth"}},
	},
	"quests": {
		"7": map[string]any{"name": map[string]string{"en_US": "Kobold Camp Cleanup", "ru_RU": "Зачистка лагеря кобольдов"},
			"battlenet": map[string]any{"description": "Your first task."},
			"foreverdb": map[string]any{"forever": map[string]any{"level": 2, "side": "Alliance", "starts": []any{map[string]any{"npc": 197, "name": "Marshal McBride"}}}}},
		"12": map[string]any{"name": map[string]string{"en_US": "The People's Militia"},
			"foreverdb": map[string]any{"forever": map[string]any{"level": 12, "side": "Alliance", "previous": 7}}},
		"13": map[string]any{"name": map[string]string{"en_US": "The People's Militia"},
			"foreverdb": map[string]any{"forever": map[string]any{"level": 17, "side": "Alliance", "previous": 12}}},
		"20": map[string]any{"name": map[string]string{"en_US": "Horde Business"},
			"foreverdb": map[string]any{"classic": map[string]any{"level": 12, "side": "Horde"}}},
	},
	"npcs": {
		"197": map[string]any{"name": map[string]string{"en_US": "Marshal McBride"}, "foreverdb": map[string]any{"spots": []any{map[string]any{"map": 1429, "x": 48.9, "y": 41.6}}}},
		"54": map[string]any{"name": map[string]string{"en_US": "Corina Steele"}, "foreverdb": map[string]any{"title": "Weaponsmith", "side": "Alliance",
			"place": map[string]any{"map": 1429, "x": 41.6, "y": 65.8}, "sells": []any{map[string]any{"item": 1811, "price": 536}, map[string]any{"item": 9999, "price": 1}}}},
		"61": map[string]any{"name": map[string]string{"en_US": "Thuros Lightfingers"}, "foreverdb": map[string]any{"rare": map[string]any{"level": []any{11, 11}, "spawns": "1429:52.7,58.9;28.6,59.9"}}},
		"3347": map[string]any{"name": map[string]string{"en_US": "Yelmak"}, "foreverdb": map[string]any{"title": "Alchemy Trainer", "side": "Horde",
			"place": map[string]any{"map": 1454, "x": 56, "y": 33}, "trains": []any{"Alchemy"}}},
	},
	"spells": {
		"133": map[string]any{"name": map[string]string{"en_US": "Fireball"}, "client": map[string]any{"effects": []any{map[string]any{"coef": 0.429}}},
			"foreverdb": map[string]any{"class_spell": map[string]any{"class": "MAGE", "level": 1, "rank": "Rank 1"}}},
		"143": map[string]any{"name": map[string]string{"en_US": "Fireball"}, "foreverdb": map[string]any{"class_spell": map[string]any{"class": "MAGE", "level": 6, "rank": "Rank 2"}}},
		"2963": map[string]any{"name": map[string]string{"en_US": "Bolt of Linen Cloth"},
			"foreverdb": map[string]any{"recipe": map[string]any{"profession": "Tailoring", "skill": 1, "reagents": []any{[]any{2589, 2}}}}},
	},
	"places": {
		"zone-1429": map[string]any{"name": map[string]string{"en_US": "Elwynn Forest"}, "foreverdb": map[string]any{"zone": map[string]any{"levels": "2-10", "side": "A"}}},
		"zone-1454": map[string]any{"name": map[string]string{"en_US": "Orgrimmar"}, "foreverdb": map[string]any{"zone": map[string]any{"ui_map": 1454}}},
	},
}

func testWorldWatcher(t *testing.T, base string, now time.Time) *worldWatcher {
	return &worldWatcher{dir: worldDir(filepath.Join(t.TempDir(), "snapshots")), http: http.DefaultClient,
		base: func() string { return base }, now: func() time.Time { return now }}
}

func TestWorldDatasetDownloadsChangedFilesOnceADay(t *testing.T) {
	f := newFakeWorld(t, testWorldRecords, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339))
	now := time.Now()
	w := testWorldWatcher(t, f.srv.URL, now)
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 6 {
		t.Fatalf("the manifest and five files: %v", f.asked)
	}
	for _, file := range f.manifest.Files {
		if _, err := os.Stat(filepath.Join(w.dir, filepath.Base(file.Path))); err != nil {
			t.Fatal(err)
		}
	}

	f.asked = nil
	if err := w.refresh(context.Background()); err != nil || len(f.asked) != 0 {
		t.Fatal("asked again within a day:", f.asked, err)
	}

	// A day later only the changed kind is downloaded, and the replaced file goes.
	changed := map[string]map[string]any{}
	for k, v := range testWorldRecords {
		changed[k] = v
	}
	changed["npcs"] = map[string]any{"197": map[string]any{"name": map[string]string{"en_US": "Marshal McBride"}, "foreverdb": map[string]any{"title": "Marshal"}}}
	old := map[string]bool{}
	for _, file := range f.manifest.Files {
		old[filepath.Base(file.Path)] = true
	}
	f.set(t, changed, f.manifest.Sources.Battlenet.Oldest)
	f.asked = nil
	w.now = func() time.Time { return now.Add(25 * time.Hour) }
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 2 || !strings.HasPrefix(f.asked[1], "/world/npcs-") {
		t.Fatalf("the manifest and the npcs file only: %v", f.asked)
	}
	entries, _ := os.ReadDir(w.dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 6 {
		t.Fatalf("five files and the manifest, the old npcs file removed: %v", names)
	}
}

func TestWorldDatasetRefusesABadDownloadAndExpiresBattleNetData(t *testing.T) {
	f := newFakeWorld(t, testWorldRecords, "")
	f.manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	w := testWorldWatcher(t, f.srv.URL, time.Now())
	if err := w.refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatal("a file that does not match the manifest is refused:", err)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "manifest.json")); err == nil {
		t.Fatal("no manifest is stored after a failed download")
	}

	f.set(t, testWorldRecords, time.Now().Add(-10*24*time.Hour).UTC().Format(time.RFC3339))
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Twenty-one days later, with the mirror gone, the stored copy holds Battle.net data older than 30 days.
	w.base = func() string { return "" }
	w.now = func() time.Time { return time.Now().Add(21 * 24 * time.Hour) }
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(w.dir)
	if len(entries) != 0 {
		t.Fatalf("everything is removed: %v", entries)
	}
}

func TestWorldToolsSearchAndRead(t *testing.T) {
	f := newFakeWorld(t, testWorldRecords, "")
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	if _, err := queryWorldSearch(snapshots, []byte(`{"query":"potion"}`)); err != errWorldAbsent {
		t.Fatal("before the download the tool says the dataset is not here yet:", err)
	}
	w := &worldWatcher{dir: worldDir(snapshots), http: http.DefaultClient, base: func() string { return f.srv.URL }, now: time.Now}
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	search := func(args string) []mcpObject {
		t.Helper()
		out, err := queryWorldSearch(snapshots, []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		if out["captured"] != "2026-11-22T10:00:00Z" {
			t.Fatal(out)
		}
		return out["results"].([]mcpObject)
	}
	ids := func(r []mcpObject) string {
		var s []string
		for _, x := range r {
			s = append(s, x["kind"].(string)+":"+x["id"].(string))
		}
		return strings.Join(s, " ")
	}
	if r := search(`{"query":"слабое лечебное ЗЕЛЬЕ"}`); ids(r) != "items:118" || r[0]["matched"] != "Слабое лечебное зелье" || r[0]["quality"] != "Common" {
		t.Fatalf("a Russian name, any case, with the item's facts: %v", r)
	}
	if r := search(`{"query":"kobold cleanup"}`); ids(r) != "quests:7" || r[0]["giver"] != "Marshal McBride" || r[0]["zone"] != "Elwynn Forest" || r[0]["level"] != 2 {
		t.Fatalf("by its words, with giver and the giver's zone: %v", r)
	}
	if r := search(`{"query":"Fireball"}`); ids(r) != "spells:133 spells:143" || r[1]["rank"] != "Rank 2" || r[1]["level"] != 6 {
		t.Fatalf("same names told apart by rank and level: %v", r)
	}
	if r := search(`{"query":"197"}`); ids(r) != "npcs:197" {
		t.Fatalf("by ID: %v", r)
	}

	// Filters alone: a zone, a level range, a side, a role.
	if r := search(`{"kind":"quests","min_level":10,"max_level":14}`); ids(r) != "quests:12 quests:20" {
		t.Fatalf("quests of levels 10-14: %v", r)
	}
	if r := search(`{"kind":"quests","min_level":10,"max_level":14,"side":"Horde"}`); ids(r) != "quests:20" {
		t.Fatalf("the Horde's only: %v", r)
	}
	if r := search(`{"kind":"npcs","role":"trainer","zone":"orgrim"}`); ids(r) != "npcs:3347" || r[0]["title"] != "Alchemy Trainer" {
		t.Fatalf("trainers in Orgrimmar: %v", r)
	}
	if r := search(`{"kind":"npcs","role":"rare","zone":"Elwynn"}`); ids(r) != "npcs:61" || r[0]["zone"] != "Elwynn Forest" {
		t.Fatalf("a rare placed by its spawns: %v", r)
	}
	if r := search(`{"query":"alchemy trainer","zone":"Orgrimmar"}`); ids(r) != "npcs:3347" {
		t.Fatalf("by what an NPC is: %v", r)
	}
	if r := search(`{"kind":"npcs","zone":"1429","role":"vendor"}`); ids(r) != "npcs:54" {
		t.Fatalf("a zone by its uiMap number: %v", r)
	}
	if out, err := queryWorldSearch(snapshots, []byte(`{"kind":"npcs","max_results":1,"zone":"Elwynn"}`)); err != nil || out["total"] != 3 || !strings.Contains(out["note"].(string), "2 more") {
		t.Fatal("the total and how to narrow it:", out, err)
	}
	if _, err := queryWorldSearch(snapshots, []byte(`{"kind":"quests"}`)); err == nil || !strings.Contains(err.Error(), "zone Westfall") {
		t.Fatal("a kind alone is refused with an example:", err)
	}

	out, err := queryWorldGet(snapshots, []byte(`{"refs":[{"kind":"npcs","id":"54"},{"kind":"quests","id":13},{"kind":"spells","id":"2963"},{"kind":"quests","id":"99"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(out)
	for _, want := range []string{`"item":1811,"name":"Blunt Claymore"`, `"previous":12,"previous_name":"The People's Militia"`,
		`{"count":2,"item":2589,"name":"Linen Cloth"}`, `"missing":[{"id":"99","kind":"quests"}]`, `"credit":"Data from ForeverDB.net`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("names beside IDs, several records, the missing one and the credit; %s not in %s", want, body)
		}
	}
	quest, err := queryWorldGet(snapshots, []byte(`{"snapshot_id":"0123456789abcdef0123456789abcdef","refs":[{"kind":"quests","id":"7"}]}`))
	if body, _ := json.Marshal(quest); err != nil || !strings.Contains(string(body), `"name":"Marshal McBride","npc":197,"where":{"map":1429,"x":48.9,"y":41.6,"zone":"Elwynn Forest"}`) {
		t.Fatalf("a quest's giver comes with where they stand, and a habitual snapshot_id is accepted: %s %v", body, err)
	}
	if strings.Contains(string(body), `"item":9999,"name"`) {
		t.Fatal("an unknown item gets no name")
	}
	if _, err := queryWorldGet(snapshots, []byte(`{"refs":[{"kind":"quests","id":"../x"}]}`)); err == nil {
		t.Fatal("a bad ID is refused")
	}
}

func TestItemTableComesFromTheWorldDataset(t *testing.T) {
	f := newFakeWorld(t, testWorldRecords, "")
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	dir := auctionDir(snapshots)
	aw := testWatcher(dir, time.Now())
	if err := aw.refreshItems(context.Background()); err == nil {
		t.Fatal("no item table before the world dataset is here")
	}
	w := &worldWatcher{dir: worldDir(snapshots), http: http.DefaultClient, base: func() string { return f.srv.URL }, now: time.Now}
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := aw.refreshItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	table, err := loadItemTable(dir)
	if err != nil || len(table.Items) != 2 || table.Items[14158] != [3]int{4, 1, 20} || table.Items[1811] != [3]int{2, 8, 17} {
		t.Fatalf("weapons and armour only: %+v %v", table, err)
	}
	stamp, _ := os.Stat(filepath.Join(dir, "items.json"))
	time.Sleep(10 * time.Millisecond)
	if err := aw.refreshItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.Stat(filepath.Join(dir, "items.json")); !again.ModTime().Equal(stamp.ModTime()) {
		t.Fatal("the same dataset does not rebuild the table")
	}
}
