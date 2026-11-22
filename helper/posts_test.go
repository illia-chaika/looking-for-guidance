package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// postsNow is a day the made-up fixtures in testdata/posts are recent on.
var postsNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func postsFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "posts", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// postsInternet serves the made-up news list, forum searches and posts by address; fail names addresses that fail.
func postsInternet(t *testing.T, calls map[string]int, fail map[string]bool) func(context.Context, string) (string, error) {
	pages := map[string]string{
		defaultNewsURL:       "news.json",
		blueForums[0].Search: "search-us.json",
		blueForums[1].Search: "search-eu.json",
		"https://us.forums.blizzard.com/en/wow/posts/900002.json": "post-900002.json",
		"https://us.forums.blizzard.com/en/wow/posts/900003.json": "post-900003.json",
		"https://eu.forums.blizzard.com/en/wow/posts/700010.json": "post-700010.json",
	}
	return func(_ context.Context, url string) (string, error) {
		calls[url]++
		name, ok := pages[url]
		if !ok || fail[url] {
			return "", errors.New(url + " answered 500")
		}
		return postsFixture(t, name), nil
	}
}

func TestParseNewsAndBluePosts(t *testing.T) {
	since := postsNow.AddDate(0, 0, -feedDays)
	news, err := parseNews(postsFixture(t, "news.json"), since)
	if err != nil {
		t.Fatal(err)
	}
	us, err := parseBlue(blueForums[0], postsFixture(t, "search-us.json"), since)
	if err != nil {
		t.Fatal(err)
	}
	eu, err := parseBlue(blueForums[1], postsFixture(t, "search-eu.json"), since)
	if err != nil {
		t.Fatal(err)
	}
	posts := mergePosts(news, us, eu)
	var ids []string
	byID := map[string]blizzardPost{}
	for _, p := range posts {
		ids = append(ids, p.ID)
		byID[p.ID] = p
	}
	// Left out: the hotfix article and thread, news without Forever, the known-issues thread, posts older than two
	// weeks, and the forum copy of an article.
	if got := strings.Join(ids, " "); got != "news:90000001 news:90000004 us:900002 us:900003 eu:700010" {
		t.Fatalf("posts: %s", got)
	}
	tuning := byID["us:900002"]
	if tuning.Source != "blue" || !tuning.First || tuning.Author != "Bluestaff" || tuning.Title != "Class Tuning Coming Soon - Made-up" ||
		tuning.Links["us"] != "https://us.forums.blizzard.com/en/wow/t/800002/1" || tuning.Links["eu"] != "https://eu.forums.blizzard.com/en/wow/t/600002/1" {
		t.Fatalf("the EU copy of a US post is another link of it: %+v", tuning)
	}
	if reply := byID["us:900003"]; reply.First || reply.Title != "Shaman totems & weapons were bugged" || reply.Links["us"] != "https://us.forums.blizzard.com/en/wow/t/800003/2" {
		t.Fatalf("a reply links to its post number: %+v", reply)
	}
	if n := byID["news:90000004"]; n.Links["news"] != "https://news.blizzard.com/en-us/article/90000004" || n.link("eu") != n.Links["news"] {
		t.Fatalf("an article links to Blizzard's news site only: %+v", n)
	}
	if n := byID["news:90000001"]; n.Title != "WoW: Forever Weekly – A Made-up Edition" || n.date() != "2026-11-20" || !strings.Contains(n.Excerpt, "Thousand Needles") {
		t.Fatalf("%+v", n)
	}
	for _, broken := range []string{"", "<html>", `{"posts":[]}`, `{"error":"rate limited"}`} {
		if _, err := parseBlue(blueForums[0], broken, since); err == nil {
			t.Errorf("a search answer %q is an error, not an empty list", broken)
		}
	}
	if _, err := parseNews(`{"sections":[]}`, since); err == nil {
		t.Error("a news list without items is an error")
	}
}

func TestPostTextLeavesOutQuotes(t *testing.T) {
	var post struct {
		Cooked string `json:"cooked"`
	}
	json.Unmarshal([]byte(postsFixture(t, "post-900002.json")), &post)
	text := postText(post.Cooked)
	if strings.Contains(text, "Sprint") || strings.Contains(text, "someone") || strings.Contains(text, "<") {
		t.Fatalf("quoted posts and markup stay out:\n%s", text)
	}
	if !strings.Contains(text, "Shaman\n- Lightning Bolt damage increased by 5%.") {
		t.Fatalf("headings and list items as in the hotfix notes:\n%s", text)
	}
	notes := postNotes(blizzardPost{At: postsNow.Unix(), Title: "Class Tuning Coming Soon", First: true, Text: text})
	var lines []string
	for _, n := range notes {
		lines = append(lines, strings.Join(append(n.Path, n.Text), " > "))
	}
	want := "Class Tuning Coming Soon | Made-up tuning notes written for these tests. | Shaman > Lightning Bolt damage increased by 5%. | " +
		"Priest > Vampiric Embrace now heals the whole party for 10% of the damage done (was 5%). | Priest > We will share more soon."
	if got := strings.Join(lines, " | "); got != want {
		t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoadPostsReadsEachPostOnceAndKeepsAListThatFails(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "insights")
	calls := map[string]int{}
	fail := map[string]bool{}
	fetch := postsInternet(t, calls, fail)
	got, err := loadPosts(context.Background(), dir, postsNow, fetch)
	if err != nil || len(got.Posts) != 5 {
		t.Fatalf("%+v %v", got, err)
	}
	total := func() int {
		n := 0
		for _, c := range calls {
			n += c
		}
		return n
	}
	if total() != 6 {
		t.Fatalf("three lists and the text of three forum posts: %v", calls)
	}
	for _, p := range got.Posts {
		if p.Source == "blue" && (!p.Read || p.Text == "") {
			t.Fatalf("a forum post's text is read: %+v", p)
		}
	}

	// Within 3 hours and without the backend's feed nothing is asked again.
	if _, err := loadPosts(context.Background(), dir, postsNow.Add(time.Hour), fetch); err != nil || total() != 6 {
		t.Fatalf("asked again: %v %v", calls, err)
	}

	// Later the EU forum fails: its own posts stay, the texts are not read again.
	fail[blueForums[1].Search] = true
	got, err = loadPosts(context.Background(), dir, postsNow.Add(4*time.Hour), fetch)
	if err != nil || total() != 9 {
		t.Fatalf("the three lists only: %v %v", calls, err)
	}
	found := false
	for _, p := range got.Posts {
		found = found || p.ID == "eu:700010" && p.Text != ""
	}
	if !found || len(got.Posts) != 5 {
		t.Fatalf("a forum that cannot be read keeps its posts: %+v", got.Posts)
	}

	// A post whose text cannot be read is asked again a few times, then shown with the search's excerpt.
	os.Remove(filepath.Join(dir, postsFileName))
	fail = map[string]bool{"https://us.forums.blizzard.com/en/wow/posts/900003.json": true}
	fetch = postsInternet(t, calls, fail)
	for i := 0; i < postsReadTries+2; i++ {
		got, _ = loadPosts(context.Background(), dir, postsNow.Add(time.Duration(i)*time.Minute), fetch)
	}
	if calls["https://us.forums.blizzard.com/en/wow/posts/900003.json"] != 1+postsReadTries {
		t.Fatalf("asked %d times", calls["https://us.forums.blizzard.com/en/wow/posts/900003.json"])
	}
	for _, p := range got.Posts {
		if p.ID == "us:900003" && (!p.Read || p.Text != "" || !strings.Contains(p.excerpt(), "deployed a fix")) {
			t.Fatalf("%+v", p)
		}
	}

	// No list and no stored posts: an error.
	broken := func(context.Context, string) (string, error) { return "", errors.New("offline") }
	if _, err := loadPosts(context.Background(), filepath.Join(root, "other"), postsNow, broken); err == nil {
		t.Fatal("nothing read and nothing stored must be an error")
	}
}

func TestPostsFollowTheBackendsSignals(t *testing.T) {
	f := &changesFile{}
	f.News.ChangedAt, f.Blue.ChangedAt = "2026-11-22T09:50:00.000Z", "2026-11-22T10:50:00.000Z"
	if postsChangedAt(f) != f.Blue.ChangedAt {
		t.Fatal("the later of the two lists' changes")
	}
	f.Blue.ChangedAt = ""
	if postsChangedAt(f) != "" {
		t.Fatal("a list the backend could not read: every 3 hours, as without the feed")
	}

	useBackendToken(t)
	root := t.TempDir()
	dir := filepath.Join(root, "insights")
	calls := map[string]int{}
	fetch := postsInternet(t, calls, nil)
	feed := func(blue string) {
		os.MkdirAll(filepath.Join(root, "world"), 0o700)
		body := `{"built":"` + time.Now().UTC().Format(time.RFC3339) + `","news":{"newest":null,"changed_at":"2026-11-22T09:50:00.000Z"},"blue":{"newest":null,"changed_at":"` + blue + `"},"changes":[]}`
		os.WriteFile(filepath.Join(root, "world", changesFileName), []byte(body), 0o600)
	}
	feed("2026-11-22T10:50:00.000Z")
	loadPosts(context.Background(), dir, postsNow, fetch)
	lists := func() int { return calls[defaultNewsURL] }
	loadPosts(context.Background(), dir, postsNow.Add(20*time.Hour), fetch)
	if lists() != 1 {
		t.Fatal("no change since: not asked again, even after 3 hours")
	}
	feed("2026-11-23T07:50:00.000Z")
	loadPosts(context.Background(), dir, postsNow.Add(20*time.Hour), fetch)
	if lists() != 2 {
		t.Fatal("a new post on a forum: asked again")
	}
}

func TestPostsInTheCardAndTheAdvisersTool(t *testing.T) {
	snapshots, world := changesFixture(t)
	dir := filepath.Join(filepath.Dir(snapshots), "insights")
	posts, err := loadPosts(context.Background(), dir, postsNow, postsInternet(t, map[string]int{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	since := postsNow.AddDate(0, 0, -changesDays).Format("2006-01-02")
	match := func(f changeFacts, forum string) (map[string]changeItem, []changeItem, []changeItem) {
		mine, class, all := matchPosts(f, world, posts, since, forum)
		out := map[string]changeItem{}
		for _, it := range all {
			out[it.Text] = it
		}
		return out, mine, class
	}

	// A priest who knows Vampiric Embrace: the line about it, not the shaman's.
	got, mine, _ := match(facts("PRIEST", "Human", 30, map[string]any{"spells": "15286"}), "us")
	tuning := got["Class Tuning Coming Soon - Made-up"]
	if len(mine) != 1 || tuning.tier != tierMine || tuning.Reason != "spell" || !strings.HasPrefix(tuning.Line, "Vampiric Embrace now heals") ||
		tuning.URL != "https://us.forums.blizzard.com/en/wow/t/800002/1" || tuning.Kind != "post" || tuning.Source != "blue" {
		t.Fatalf("%+v", tuning)
	}
	if len(got) != 5 {
		t.Fatalf("every recent post stays in the list: %+v", got)
	}

	// A shaman in Thousand Needles: the reply about its zone and the news about it are its own; the tuning post is
	// for its class. A Russian client reads the EU forum's copy.
	got, mine, class := match(facts("SHAMAN", "Orc", 34, map[string]any{"map": 1441}), "eu")
	if reply := got["Shaman totems & weapons were bugged"]; reply.tier != tierMine || reply.Reason != "zone" || !strings.Contains(reply.Line, "Thousand Needles") {
		t.Fatalf("%+v", reply)
	}
	if n := got["WoW: Forever Weekly – A Made-up Edition"]; n.tier != tierMine || n.Source != "news" || n.URL != "https://news.blizzard.com/en-us/article/90000001/wow-forever-weekly-a-made-up-edition" {
		t.Fatalf("%+v", n)
	}
	if tuning := got["Class Tuning Coming Soon - Made-up"]; tuning.tier != tierClass || tuning.URL != "https://eu.forums.blizzard.com/en/wow/t/600002/1" {
		t.Fatalf("%+v", tuning)
	}
	if len(mine) != 2 || len(class) != 1 {
		t.Fatalf("mine %+v class %+v", mine, class)
	}

	// A rogue who knows Sprint: a player's quoted question about it is not Blizzard's word.
	if _, mine, class := match(facts("ROGUE", "Human", 26, map[string]any{"spells": "2983"}), "us"); len(mine)+len(class) != 0 {
		t.Fatalf("mine %+v class %+v", mine, class)
	}

	// The card: matched posts join the groups (and the notice's key), the newest posts come with their links.
	req := insightRequest{Kind: "changes", GameLocale: "ruRU", Facts: json.RawMessage(`{"class":"SHAMAN","race":"Orc","level":34,"map":1441}`)}
	answer, err := answerChanges(req, insightEnv{World: world, Posts: posts})
	if err != nil {
		t.Fatal(err)
	}
	a := answer.(changesAnswer)
	if a.Key == "" || a.NoPosts || len(a.Posts) != 5 || a.Posts[0].URL == "" {
		t.Fatalf("%+v", a)
	}
	kinds := map[string]int{}
	for _, it := range append(a.Mine, a.Class...) {
		kinds[it.Kind]++
	}
	if kinds["post"] != 3 {
		t.Fatalf("matched posts in the groups: %+v %+v", a.Mine, a.Class)
	}
	if a, _ := answerChanges(req, insightEnv{World: world}); !a.(changesAnswer).NoPosts {
		t.Fatal("no posts read: said so")
	}

	// The adviser's tool lists them from what is stored, matched to the character, or by a record asked about.
	saveChangeFacts(dir, insightRequest{Character: "Player-1-0B", Facts: json.RawMessage(`{"class":"PRIEST","race":"Human","level":30,"spells":"15286"}`)})
	id, err := saveGameSnapshot(snapshots, gameSnapshot{Character: "Player-1-0B", Captured: "now", Sections: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := queryChanges(snapshots, json.RawMessage(`{"snapshot_id":"`+id+`","since":"2026-11-08"}`))
	if err != nil {
		t.Fatal(err)
	}
	list := out["posts"].([]mcpObject)
	matched := 0
	for _, p := range list {
		if p["tier"] == tierMine {
			matched++
			if p["url"] != "https://us.forums.blizzard.com/en/wow/t/800002/1" || p["author"] != "Bluestaff" || !strings.Contains(p["matched_line"].(string), "Vampiric Embrace") {
				t.Fatalf("%v", p)
			}
		}
		if p["url"] == "" || p["excerpt"] == "" {
			t.Fatalf("every post with its link and excerpt: %v", p)
		}
	}
	if len(list) != 5 || matched != 1 {
		t.Fatalf("%v", list)
	}
	out, err = queryChanges(snapshots, json.RawMessage(`{"snapshot_id":"`+id+`","since":"2026-11-08","mine":false,"about":[{"kind":"spells","id":"15286"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if list := out["posts"].([]mcpObject); len(list) != 1 || list[0]["tier"] != "asked" || list[0]["name"] != "Vampiric Embrace" {
		t.Fatalf("asked about a spell: %v", list)
	}
}
