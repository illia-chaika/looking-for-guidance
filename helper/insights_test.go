package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testEngine runs jobs through a scripted answer instead of Codex.
func testEngine(t *testing.T, answer func(job *insightJob, prompt string) (string, error)) (*insightEngine, *sync.Mutex, *[]string) {
	t.Helper()
	page, err := os.ReadFile("testdata/hotfixes.html")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var prompts []string
	now := time.Unix(1794480000, 0)
	e := &insightEngine{dir: t.TempDir(), feedURL: defaultHotfixURL, notify: func() {}, now: func() time.Time { return now },
		fetch: func(context.Context, string) (string, error) { return string(page), nil }, active: map[string]bool{}, ready: true}
	e.run = func(_ context.Context, job *insightJob, prompt, _, out string) error {
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()
		text, err := answer(job, prompt)
		if err != nil {
			return err
		}
		return os.WriteFile(out, []byte(text), 0o600)
	}
	return e, &mu, &prompts
}

func waitInsight(t *testing.T, e *insightEngine, kind, state string) insight {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, in := range e.view() {
			if in.Kind == kind && in.State == state {
				return in
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s insight reached %s: %+v", kind, state, e.view())
	return insight{}
}

const warlockFacts = `{"class":"SHAMAN","race":"Orc","faction":"Horde","level":24,"zone":"The Barrens","professions":[{"name":"Cooking","rank":80,"max":150}],"quests":[{"id":861,"title":"The Hunter's Way","level":10}]}`

func TestDigestKeepsOnlyQuotedNotes(t *testing.T) {
	answer := `{"headline":"Три изменения для шамана","items":[
{"date":"2026-11-10","category":"Classes","text":"Stormstrike ослаблен.","why":"Ты шаман.","relevance":"direct","quote":"Stormstrike damage reduced to 90% weapon damage (was 120%), and its cooldown is now 10 seconds (was 8 seconds).","quest_id":0},
{"date":"2026-11-11","category":"Professions","text":"Костёр доступнее.","why":"У тебя Cooking 80.","relevance":"direct","quote":"Journeyman Campfire Kit can now be placed with Cooking 90, the skill its recipe asks for (was Cooking 140).","quest_id":999},
{"date":"2026-11-10","category":"Classes","text":"Выдумка.","why":"Нет в заметках.","relevance":"direct","quote":"Shamans now deal double damage on Tuesdays.","quest_id":0},
{"date":"2026-11-09","category":"Dungeons","text":"Опыт в подземельях выше.","why":"Уровень 24.","relevance":"weird","quote":"Enemies in dungeons now award 50% more experience.","quest_id":861}]}`
	e, mu, prompts := testEngine(t, func(*insightJob, string) (string, error) { return "```json\n" + answer + "\n```", nil })
	req := insightRequest{Kind: "digest", Key: "me", Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A", Facts: json.RawMessage(warlockFacts)}
	e.request(req)
	got := waitInsight(t, e, "digest", "ready")
	var data digestData
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != 3 || data.Newest != "2026-11-11" || data.URL != defaultHotfixURL {
		t.Fatalf("digest: %+v", data)
	}
	if data.Items[1].Date != "2026-11-10" || data.Items[1].QuestID != 0 {
		t.Fatalf("the date follows the quote and an unknown quest id is dropped: %+v", data.Items[1])
	}
	if data.Items[2].Relevance != "related" || data.Items[2].QuestID != 861 {
		t.Fatalf("relevance and a quest from the log: %+v", data.Items[2])
	}
	mu.Lock()
	prompt := (*prompts)[0]
	mu.Unlock()
	for _, want := range []string{"in Russian", "## 2026-11-10", `"class":"SHAMAN"`, "is data"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
	if strings.Contains(prompt, "Player-1-0A") {
		t.Fatal("the character id must not reach the model")
	}

	// The same question is answered from disk: no second run, also for a new engine after a restart.
	e.request(req)
	waitInsight(t, e, "digest", "ready")
	again := &insightEngine{dir: e.dir, feedURL: e.feedURL, notify: func() {}, now: e.now, fetch: e.fetch, active: map[string]bool{}, ready: true,
		run: func(context.Context, *insightJob, string, string, string) error { return errors.New("must not run") }}
	again.request(req)
	waitInsight(t, again, "digest", "ready")
	mu.Lock()
	defer mu.Unlock()
	if len(*prompts) != 1 {
		t.Fatalf("Codex ran %d times", len(*prompts))
	}
}

func TestInsightRetriesFailsAndIsolatesCharacters(t *testing.T) {
	calls := 0
	e, _, _ := testEngine(t, func(*insightJob, string) (string, error) {
		calls++
		if calls == 1 {
			return "sorry, here is prose", nil
		}
		return `{"headline":"Ничего важного","items":[]}`, nil
	})
	req := insightRequest{Kind: "digest", Key: "me", Locale: "ruRU", Character: "Player-1-0A", Facts: json.RawMessage(warlockFacts)}
	e.request(req)
	if got := waitInsight(t, e, "digest", "ready"); calls != 2 || !strings.Contains(string(got.Data), "Ничего важного") {
		t.Fatalf("a malformed answer gets one more try: calls=%d %s", calls, got.Data)
	}

	broken, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return "", errors.New("signed out") })
	broken.request(req)
	failed := waitInsight(t, broken, "digest", "failed")
	if !strings.Contains(failed.Note, "signed out") {
		t.Fatalf("failure note: %q", failed.Note)
	}
	// A failure is remembered for a while, so an open card does not start run after run.
	ran := false
	broken.run = func(context.Context, *insightJob, string, string, string) error { ran = true; return nil }
	broken.request(req)
	waitInsight(t, broken, "digest", "failed")
	if ran {
		t.Fatal("a recent failure started another run")
	}

	// Another character sees none of the first one's cards.
	e.request(insightRequest{Kind: "digest", Key: "me", Locale: "ruRU", Character: "Player-1-0B", Facts: json.RawMessage(warlockFacts)})
	waitInsight(t, e, "digest", "ready")
	if len(e.view()) != 1 {
		t.Fatalf("cards of two characters mixed: %+v", e.view())
	}
	for _, bad := range []insightRequest{
		{Kind: "nope", Key: "me", Character: "Player-1-0A"},
		{Kind: "digest", Key: "bad\nkey", Character: "Player-1-0A"},
		{Kind: "digest", Key: "me"},
	} {
		before := len(e.view())
		e.request(bad)
		if len(e.view()) != before {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestInsightWaitsForCodexAndAdoptsALeftoverResult(t *testing.T) {
	e, _, prompts := testEngine(t, func(*insightJob, string) (string, error) { return `{"headline":"Готово","items":[]}`, nil })
	e.ready = false
	req := insightRequest{Kind: "digest", Key: "me", Locale: "enUS", Character: "Player-1-0A", Facts: json.RawMessage(warlockFacts)}
	e.request(req)
	waitInsight(t, e, "digest", "pending")
	time.Sleep(30 * time.Millisecond)
	if len(*prompts) != 0 {
		t.Fatal("a run started before Codex was ready")
	}
	ready := true
	e.pump(&ready)
	waitInsight(t, e, "digest", "ready")

	// A run that finished after the helper quit left its answer in the work folder.
	left, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return "", errors.New("must not run") })
	id := left.id(digestKind, req)
	os.MkdirAll(filepath.Join(left.dir, "work"), 0o700)
	os.WriteFile(left.outPath(id), []byte(`{"headline":"Из прошлого запуска","items":[]}`), 0o600)
	left.request(req)
	if got := waitInsight(t, left, "digest", "ready"); !strings.Contains(string(got.Data), "Из прошлого запуска") {
		t.Fatalf("leftover result: %s", got.Data)
	}
}

func TestInsightDailyCap(t *testing.T) {
	e, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return `{"headline":"x","items":[]}`, nil })
	for i := 0; i < insightDailyCap; i++ {
		if !e.spend() {
			t.Fatalf("run %d refused", i)
		}
	}
	if e.spend() {
		t.Fatal("the daily cap did not hold")
	}
	e.request(insightRequest{Kind: "digest", Key: "me", Locale: "enUS", Character: "Player-1-0A", Facts: json.RawMessage(warlockFacts)})
	if got := waitInsight(t, e, "digest", "failed"); !strings.Contains(got.Note, "daily limit") {
		t.Fatalf("note: %q", got.Note)
	}
}

func TestInsightPacketReachesThePayload(t *testing.T) {
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	e, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return `{"headline":"Готово","items":[]}`, nil })
	s := newSession(t.TempDir(), map[string]string{}, func() {})
	s.projectDir = t.TempDir()
	s.insights = e
	e.notify = func() {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
	}
	s.receive(packet{Type: "insight", Session: "s1", Seq: 2, Kind: "digest", Key: "me", Locale: "ruRU", GameLocale: "enUS",
		Character: "Player-1-0A", Login: "s1", Facts: json.RawMessage(warlockFacts)})
	if len(s.queue) != 0 {
		t.Fatal("a journal request must not wait in the conversation queue")
	}
	waitInsight(t, e, "digest", "ready")
	s.mu.Lock()
	p := s.payload()
	dirty := s.dirty
	s.mu.Unlock()
	if len(p.Insights) != 1 || p.Insights[0].State != "ready" || p.Character != "Player-1-0A" || !dirty {
		t.Fatalf("payload insights: %+v dirty=%v", p.Insights, dirty)
	}
	data := encodePayload(p, maxPayloadBytes)
	if !strings.Contains(string(data), `"insights":[{"kind":"digest","key":"me","state":"ready"`) {
		t.Fatalf("encoded payload: %s", data)
	}
}

func TestQuestVerdictNeedsEvidence(t *testing.T) {
	facts := json.RawMessage(`{"id":1234,"title":"A Taste of the Sea","level":12,"objectives":[{"text":"Saltwater Murloc Eyes","done":0,"need":12}],"class":"WARLOCK","faction":"Horde","plevel":12}`)
	run := func(answer string) (questData, insight) {
		e, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return answer, nil })
		e.fetch = func(_ context.Context, url string) (string, error) {
			if url == defaultIssuesURL {
				return "Known Issues\n**Quests**\n* \"Vorsha the Lasher\" in Ashenvale has been disabled until a bug causing a crash can be fixed.", nil
			}
			page, _ := os.ReadFile("testdata/hotfixes.html")
			return string(page), nil
		}
		e.request(insightRequest{Kind: "quest", Key: "q1234:WARLOCK:Horde", Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A", Facts: facts})
		got := waitInsight(t, e, "quest", "ready")
		var data questData
		json.Unmarshal(got.Data, &data)
		return data, got
	}

	fixed, _ := run(`{"verdict":"fixed","confidence":"confirmed","summary":"Исправлено 10 ноября.","cause":"Глаза не падали.","steps":[{"title":"Убей ещё","text":"Murloc Tideskimmers."}],
"sources":[{"title":"Hotfixes","url":"https://news.blizzard.com/en-us/article/24315281","quote":"Murloc Tideskimmers drop Saltwater Murloc Eyes for the quest \"A Taste of the Sea\" again.","date":"2026-11-10"}],"forever_verified":true}`)
	if fixed.Verdict != "fixed" || len(fixed.Sources) != 1 || !fixed.Verified {
		t.Fatalf("a quoted fix stands: %+v", fixed)
	}

	invented, _ := run(`{"verdict":"bug","confidence":"confirmed","summary":"Это баг.","cause":"","steps":[],
"sources":[{"title":"Hotfixes","url":"https://news.blizzard.com/en-us/article/24315281","quote":"A Taste of the Sea is permanently broken for Warlocks.","date":"2026-11-11"},
{"title":"x","url":"http://insecure.example/page","quote":"whatever it says here","date":""}],"forever_verified":true}`)
	if invented.Verdict != "unknown" || invented.Confidence != "unverified" || len(invented.Sources) != 0 || invented.Verified {
		t.Fatalf("a quote that is not in Blizzard's notes proves nothing: %+v", invented)
	}

	guess, _ := run(`{"verdict":"changed","confidence":"likely","summary":"Похоже, условия изменились.","cause":"","steps":[],"sources":[{"title":"Гайд","url":"https://example.org/guide","quote":"","date":""}],"forever_verified":false}`)
	if guess.Verdict != "unknown" || len(guess.Sources) != 1 {
		t.Fatalf("a strong verdict needs a quote: %+v", guess)
	}

	fine, _ := run(`{"verdict":"intended","confidence":"likely","summary":"Глаза падают только с Tideskimmers.","cause":"","steps":[{"title":"","text":"skip"},{"title":"A","text":"1"},{"title":"B","text":"2"},{"title":"C","text":"3"},{"title":"D","text":"4"},{"title":"E","text":"5"}],"sources":[],"forever_verified":true}`)
	if fine.Verdict != "intended" || len(fine.Steps) != 4 || fine.Verified {
		t.Fatalf("intended needs no proof, steps are capped, and nothing is verified without a source: %+v", fine)
	}
}

func TestBriefIsCleaned(t *testing.T) {
	e, _, prompts := testEngine(t, func(*insightJob, string) (string, error) {
		return `{"summary":"Качайся через Affliction.","stat_priority":["Spell Power","Stamina","","Intellect","Spirit","Armor"],
"buy":[{"name":"Corruption","rank":2,"level":14,"why":"Основной урон."},{"name":"","rank":1,"level":10,"why":"x"},{"name":"Fear","rank":1,"level":99,"why":"x"}],
"skip":[{"name":"Curse of Weakness","rank":0,"level":12,"why":"Не нужен соло."}],
"class_quests":[{"name":"Halgar's Summons","level":10,"where":"Brill","reward":"Voidwalker"}],
"riding":{"level":40,"cost_copper":999999999,"note":"Чернокнижник получает коня по заданию."},"food":"",
"sources":[{"title":"Guide","url":"https://example.org/warlock"},{"title":"bad","url":"javascript:alert(1)"}],"forever_verified":true}`, nil
	})
	e.request(insightRequest{Kind: "brief", Key: "WARLOCK:Horde:Scourge:10", Locale: "ruRU", GameLocale: "enUS",
		Facts: json.RawMessage(`{"class":"WARLOCK","race":"Scourge","faction":"Horde","level":12,"from":10,"to":19}`)})
	var data briefData
	json.Unmarshal(waitInsight(t, e, "brief", "ready").Data, &data)
	if len(data.Stats) != 4 || len(data.Buy) != 1 || data.Buy[0].Rank != 2 || len(data.Skip) != 1 || len(data.Quests) != 1 {
		t.Fatalf("lists: %+v", data)
	}
	if data.Riding.Cost != 0 || data.Riding.Level != 0 || data.Riding.Note == "" {
		t.Fatalf("an absurd riding price is dropped, the note stays: %+v", data.Riding)
	}
	if len(data.Sources) != 1 || !data.Verified {
		t.Fatalf("sources: %+v", data.Sources)
	}
	if p := (*prompts)[0]; !strings.Contains(p, "live web search") || !strings.Contains(p, `"from":10`) {
		t.Fatalf("prompt: %s", p)
	}
}

func TestDungeonPrepDropsTheOtherFactionAndUnsourcedQuests(t *testing.T) {
	answer := `{"name":"Ragefire Chasm","level_min":13,"level_max":18,"entrance":"Orgrimmar, Cleft of Shadow.",
"quests":[{"name":"The Power to Destroy...","min_level":9,"giver":"Varimathras","zone":"Undercity, Royal Quarter","note":"Две книги из подземелья.","inside":false,"faction":"Horde"},
{"name":"Alliance Only","min_level":10,"giver":"x","zone":"Stormwind","note":"","inside":false,"faction":"Alliance"},
{"name":"Slaying the Beast","min_level":9,"giver":"Neeru Fireblade","zone":"Orgrimmar","note":"","inside":false,"faction":"Both"}],
"tips":["Возьми воду."],"sources":%s,"forever_verified":true}`
	run := func(sources, key string) dungeonData {
		e, _, _ := testEngine(t, func(*insightJob, string) (string, error) { return strings.Replace(answer, "%s", sources, 1), nil })
		e.request(insightRequest{Kind: "dungeon", Key: key, Locale: "ruRU", GameLocale: "enUS",
			Facts: json.RawMessage(`{"name":"Ragefire Chasm","faction":"Horde","class":"WARLOCK","plevel":14}`)})
		var data dungeonData
		json.Unmarshal(waitInsight(t, e, "dungeon", "ready").Data, &data)
		return data
	}
	sourced := run(`[{"title":"RFC quests","url":"https://example.org/forever/rfc"}]`, "Ragefire Chasm:Horde")
	if len(sourced.Quests) != 2 || sourced.Quests[1].Faction != "Both" || sourced.MinLevel != 13 || !sourced.Verified {
		t.Fatalf("quests: %+v", sourced)
	}
	bare := run(`[]`, "Огненная пропасть:Horde")
	if len(bare.Quests) != 0 || bare.Verified || len(bare.Tips) != 1 {
		t.Fatalf("a quest list with no page behind it is not shown: %+v", bare)
	}
}

func TestBagTriageJudgesOnlyListedItemsAndHonoursNotes(t *testing.T) {
	e, _, prompts := testEngine(t, func(*insightJob, string) (string, error) {
		return `{"summary":"Освободишь 2 ячейки.","items":[{"item_id":2589,"verdict":"keep","reason":"Ты копишь ткань на бинты."},
{"item_id":4562,"verdict":"auction","reason":"Топор не для чернокнижника."},{"item_id":4562,"verdict":"sell","reason":"дубль"},
{"item_id":99999,"verdict":"sell","reason":"нет в сумках"},{"item_id":765,"verdict":"burn","reason":"нет такого вердикта"}]}`, nil
	})
	notes := []string{"Не продавай Linen Cloth: коплю на бинты."}
	e.notes = func(string) []string { return notes }
	req := insightRequest{Kind: "bags", Key: "WARLOCK:10:3-8916", Locale: "ruRU", GameLocale: "enUS", Character: "Player-1-0A",
		Facts: json.RawMessage(`{"class":"WARLOCK","level":10,"items":[{"id":2589,"name":"Linen Cloth","q":1,"n":26},{"id":4562,"name":"Severing Axe of the Monkey","q":2,"n":1},{"id":765,"name":"Silverleaf","q":1,"n":34}]}`)}
	e.request(req)
	var data bagsData
	json.Unmarshal(waitInsight(t, e, "bags", "ready").Data, &data)
	if len(data.Items) != 2 || data.Items[0].ID != 2589 || data.Items[1].Verdict != "auction" {
		t.Fatalf("verdicts: %+v", data.Items)
	}
	if p := (*prompts)[0]; !strings.Contains(p, "Не продавай Linen Cloth") || !strings.Contains(p, "These wishes win") {
		t.Fatalf("the player's notes must reach the prompt: %s", p)
	}
	// A new wish makes the cached advice stale.
	notes = append(notes, "Травы оставляю для алхимии.")
	e.request(req)
	waitInsight(t, e, "bags", "ready")
	time.Sleep(20 * time.Millisecond)
	if len(*prompts) != 2 {
		t.Fatalf("advice was not prepared again after the notes changed: %d runs", len(*prompts))
	}
}

func TestLevelingAdviceNeedsASource(t *testing.T) {
	answers := []string{
		`{"summary":"Иди в Stonetalon.","zones":[{"name":"Stonetalon Mountains","levels":"18-25","why":"Мало игроков.","crowd":"empty"}],"dungeons":[],"tips":[],"sources":[],"forever_verified":true}`,
		`{"summary":"Иди в Stonetalon.","zones":[{"name":"Stonetalon Mountains","levels":"18-25","why":"Мало игроков.","crowd":"empty"},{"name":"","levels":"","why":"","crowd":"quiet"}],"dungeons":[{"name":"Wailing Caverns","levels":"17-24","why":"Много заданий.","crowd":"unknown"}],"tips":["Ешь приготовленную еду."],"sources":[{"title":"Zones","url":"https://example.org/forever/zones"}],"forever_verified":true}`,
	}
	calls := 0
	e, _, _ := testEngine(t, func(*insightJob, string) (string, error) { calls++; return answers[calls-1], nil })
	e.request(insightRequest{Kind: "leveling", Key: "Horde:20:WARLOCK", Locale: "ruRU", GameLocale: "enUS",
		Facts: json.RawMessage(`{"faction":"Horde","class":"WARLOCK","level":21,"from":20,"to":24,"zone":"The Barrens"}`)})
	var data levelingData
	json.Unmarshal(waitInsight(t, e, "leveling", "ready").Data, &data)
	if calls != 2 || len(data.Zones) != 1 || data.Zones[0].Crowd != "unknown" || len(data.Dungeons) != 1 || len(data.Tips) != 1 {
		t.Fatalf("advice without a page is asked for again; an unknown crowd label is not passed on: calls=%d %+v", calls, data)
	}
}
