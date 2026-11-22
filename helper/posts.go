package main

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Blizzard's staff ("blue") posts on the WoW: Forever forums and Blizzard's news about Forever. The companion reads
// them itself, like the hotfix article; the backend's changes feed only says when the lists changed. A forum post's
// text is read once, so its lines are matched to the character like the hotfix notes.

const (
	defaultNewsURL = "https://news.blizzard.com/en-us/api/news/world-of-warcraft"
	postsFileName  = "posts.json"
	postsMax       = 60
	postsReadMax   = 10 // forum posts whose text one load reads
	postsReadTries = 3
	postsShown     = 8 // newest posts in the card's answer, for their links
	postTextMax    = 16 << 10
	postMirrorGap  = 10 * time.Minute // the same post on the US and EU forums
)

// blueForum is one forum's search for staff posts in its Forever categories, the same search the backend makes.
type blueForum struct {
	Name, Base, Search string
}

var blueForums = []blueForum{
	{"us", "https://us.forums.blizzard.com/en/wow", "https://us.forums.blizzard.com/en/wow/search.json?q=%23wow-forever%20group%3Ablizzard-tracker%20order%3Alatest"},
	{"eu", "https://eu.forums.blizzard.com/en/wow", "https://eu.forums.blizzard.com/en/wow/search.json?q=category%3A359%20group%3Ablizzard-tracker%20order%3Alatest"},
}

type blizzardPost struct {
	ID      string            `json:"id"`     // us:31002979 or news:24317681
	Source  string            `json:"source"` // blue or news
	At      int64             `json:"at"`
	Title   string            `json:"title"` // the thread's title for a forum post
	Author  string            `json:"author,omitempty"`
	First   bool              `json:"first,omitempty"` // the opening post of its thread: the title is its own
	Links   map[string]string `json:"links"`           // by forum (us, eu), or news
	Excerpt string            `json:"excerpt,omitempty"`
	Text    string            `json:"text,omitempty"` // a forum post's lines, without the posts it quotes
	Read    bool              `json:"read,omitempty"`
	Tries   int               `json:"tries,omitempty"`
}

func (p blizzardPost) date() string { return time.Unix(p.At, 0).UTC().Format("2006-01-02") }

// link is the post's address on the player's forum when it is there.
func (p blizzardPost) link(forum string) string {
	for _, k := range []string{"news", forum, "us", "eu"} {
		if u := p.Links[k]; u != "" {
			return u
		}
	}
	return ""
}

// excerpt is the start of what the post says: its text once read, otherwise the search's or the news list's excerpt.
func (p blizzardPost) excerpt() string {
	if p.Text != "" {
		return clip(strings.Join(strings.Fields(p.Text), " "), 300)
	}
	return p.Excerpt
}

type postsCache struct {
	Fetched int64          `json:"fetched"`
	Posts   []blizzardPost `json:"posts"` // newest first
}

// forumFor picks the forum whose copy of a post the player is sent to, by the game's locale.
func forumFor(locale string) string {
	switch locale {
	case "enGB", "deDE", "frFR", "esES", "itIT", "ruRU", "ptPT", "plPL":
		return "eu"
	}
	return "us"
}

var (
	titleKey   = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	quoteBlock = regexp.MustCompile(`(?is)<aside\b.*?</aside>`)
)

func sameTitle(a, b string) bool {
	return titleKey.ReplaceAllString(strings.ToLower(a), "") == titleKey.ReplaceAllString(strings.ToLower(b), "")
}

// parseNews reads Blizzard's news list: articles whose title names Forever, without the hotfix article (read on its
// own), published since the given time.
func parseNews(body string, since time.Time) ([]blizzardPost, error) {
	var a struct {
		Feed struct {
			Items *[]struct {
				Properties struct {
					ID      json.RawMessage `json:"newsId"`
					Title   string          `json:"title"`
					Summary string          `json:"summary"`
					Updated string          `json:"lastUpdated"`
					URL     string          `json:"newsUrl"`
				} `json:"properties"`
			} `json:"contentItems"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil || a.Feed.Items == nil {
		return nil, errors.New("the news list came back in another shape")
	}
	hotfix := defaultHotfixURL[strings.LastIndex(defaultHotfixURL, "/")+1:]
	var out []blizzardPost
	for _, it := range *a.Feed.Items {
		p := it.Properties
		id := strings.Trim(string(p.ID), `"`)
		at, err := time.Parse(time.RFC3339, p.Updated)
		if id == "" || id == hotfix || !strings.Contains(strings.ToLower(p.Title), "forever") || err != nil || at.Before(since) {
			continue
		}
		link := p.URL
		if !strings.HasPrefix(link, "https://news.blizzard.com/") {
			link = "https://news.blizzard.com/en-us/article/" + id
		}
		out = append(out, blizzardPost{ID: "news:" + id, Source: "news", At: at.Unix(), Title: strings.TrimSpace(p.Title),
			Author: "Blizzard Entertainment", First: true, Links: map[string]string{"news": link}, Excerpt: clip(strings.TrimSpace(p.Summary), 300)})
	}
	return out, nil
}

// parseBlue reads one forum's search. The hotfix thread and the known-issues thread are read on their own; a forum
// copy of a news article counts as news.
func parseBlue(f blueForum, body string, since time.Time) ([]blizzardPost, error) {
	var a struct {
		Posts *[]struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Username string `json:"username"`
			Created  string `json:"created_at"`
			Blurb    string `json:"blurb"`
			Number   int    `json:"post_number"`
			Topic    int64  `json:"topic_id"`
		} `json:"posts"`
		Topics []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"topics"`
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil || a.Posts == nil || len(*a.Posts) == 0 {
		return nil, errors.New("the forum search found no posts")
	}
	titles := map[int64]string{}
	for _, t := range a.Topics {
		titles[t.ID] = html.UnescapeString(t.Title)
	}
	var out []blizzardPost
	for _, p := range *a.Posts {
		title := strings.TrimSpace(titles[p.Topic])
		lower := strings.ToLower(title)
		at, err := time.Parse(time.RFC3339, p.Created)
		if p.ID == 0 || title == "" || strings.HasPrefix(lower, "wow: forever hotfixes") || strings.HasPrefix(lower, "known issues") || err != nil || at.Before(since) {
			continue
		}
		source, author := "blue", p.Name
		if author == "" {
			author = p.Username
		}
		if p.Username == "BlizzardEntertainment" {
			source = "news"
		}
		out = append(out, blizzardPost{ID: f.Name + ":" + strconv.FormatInt(p.ID, 10), Source: source, At: at.Unix(), Title: title,
			Author: author, First: p.Number == 1, Excerpt: clip(strings.TrimSpace(html.UnescapeString(p.Blurb)), 300),
			Links: map[string]string{f.Name: f.Base + "/t/" + strconv.FormatInt(p.Topic, 10) + "/" + strconv.Itoa(p.Number)}})
	}
	return out, nil
}

// mergePosts joins the lists, the earlier ones first. A forum post that is an earlier forum's post again (the same
// author, within ten minutes) becomes another link of it; a forum copy of a news article is dropped.
func mergePosts(lists ...[]blizzardPost) []blizzardPost {
	var out []blizzardPost
	for _, list := range lists {
	next:
		for _, p := range list {
			for i := range out {
				o := &out[i]
				if o.ID == p.ID {
					continue next
				}
				if p.Source == "news" && o.Source == "news" && sameTitle(o.Title, p.Title) {
					continue next
				}
				gap := time.Duration(o.At-p.At) * time.Second
				if p.Source == "blue" && o.Source == "blue" && o.Author == p.Author && gap <= postMirrorGap && gap >= -postMirrorGap {
					merged := false
					for forum, link := range p.Links {
						switch o.Links[forum] {
						case link:
							merged = true // a stored post that already carries this link
						case "":
							o.Links[forum], merged = link, true
						}
					}
					if merged {
						continue next
					}
				}
			}
			p.Links = mapsClone(p.Links)
			out = append(out, p)
		}
	}
	return out
}

func mapsClone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// postText is a forum post's cooked HTML as lines, without the posts it quotes and the link previews.
func postText(cooked string) string {
	return strings.Join(htmlLines(quoteBlock.ReplaceAllString(cooked, " ")), "\n")
}

// postsChangedAt is the later of the two lists' changes, or nothing when the backend could not read one of them.
func postsChangedAt(f *changesFile) string {
	a, errA := time.Parse(time.RFC3339, f.News.ChangedAt)
	b, errB := time.Parse(time.RFC3339, f.Blue.ChangedAt)
	if errA != nil || errB != nil {
		return ""
	}
	if a.After(b) {
		return f.News.ChangedAt
	}
	return f.Blue.ChangedAt
}

// loadPosts returns the posts of the last two weeks. The lists are fetched again when the backend's changes feed says
// one changed (without that feed: every 3 hours); a list that cannot be read keeps its previous posts. Forum posts
// whose text is not read yet are read, up to postsReadMax at a time.
func loadPosts(ctx context.Context, dir string, now time.Time, fetch func(context.Context, string) (string, error)) (*postsCache, error) {
	path := filepath.Join(dir, postsFileName)
	var cached postsCache
	if data, err := os.ReadFile(path); err != nil || json.Unmarshal(data, &cached) != nil {
		cached = postsCache{}
	}
	since := now.AddDate(0, 0, -feedDays)
	out := cached
	changed := false
	if cached.Fetched == 0 || feedStale(cached.Fetched, now, filepath.Join(filepath.Dir(dir), "world"), postsChangedAt) {
		kept := func(prefix string) []blizzardPost {
			var list []blizzardPost
			for _, p := range cached.Posts {
				if strings.HasPrefix(p.ID, prefix) {
					list = append(list, p)
				}
			}
			return list
		}
		var lists [][]blizzardPost
		read := 0
		var news []blizzardPost
		body, err := fetch(ctx, defaultNewsURL)
		if err == nil {
			news, err = parseNews(body, since)
		}
		if err == nil {
			read++
			lists = append(lists, news)
		} else {
			logf("Blizzard news not refreshed: %v", err)
			lists = append(lists, kept("news:"))
		}
		for _, f := range blueForums {
			var posts []blizzardPost
			body, err := fetch(ctx, f.Search)
			if err == nil {
				posts, err = parseBlue(f, body, since)
			}
			if err == nil {
				read++
				lists = append(lists, posts)
			} else {
				logf("Blue posts of the %s forum not refreshed: %v", f.Name, err)
				lists = append(lists, kept(f.Name+":"))
			}
		}
		if read == 0 && len(cached.Posts) == 0 {
			return nil, errors.New("neither Blizzard's news nor the forums could be read")
		}
		known := map[string]blizzardPost{}
		for _, p := range cached.Posts {
			known[p.ID] = p
		}
		out = postsCache{Fetched: cached.Fetched}
		if read > 0 {
			out.Fetched = now.Unix()
		}
		for _, p := range mergePosts(lists...) {
			if old, ok := known[p.ID]; ok {
				p.Text, p.Read, p.Tries = old.Text, old.Read, old.Tries
			}
			out.Posts = append(out.Posts, p)
		}
		changed = true
	}
	var recent []blizzardPost
	for _, p := range out.Posts {
		if time.Unix(p.At, 0).After(since) {
			recent = append(recent, p)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].At > recent[j].At })
	if len(recent) > postsMax {
		recent = recent[:postsMax]
	}
	changed = changed || len(recent) != len(out.Posts)
	out.Posts = recent

	reads := 0
	for i := range out.Posts {
		p := &out.Posts[i]
		forum, id, _ := strings.Cut(p.ID, ":")
		if p.Source != "blue" || p.Read || reads >= postsReadMax {
			continue
		}
		var base string
		for _, f := range blueForums {
			if f.Name == forum {
				base = f.Base
			}
		}
		if base == "" {
			continue
		}
		reads++
		changed = true
		body, err := fetch(ctx, base+"/posts/"+id+".json")
		var post struct {
			Cooked string `json:"cooked"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(body), &post)
		}
		if err != nil || post.Cooked == "" {
			p.Tries++
			p.Read = p.Tries >= postsReadTries // a post that keeps failing is shown with the search's excerpt
			continue
		}
		p.Text, p.Read = trimText(postText(post.Cooked), postTextMax), true // trimText keeps the lines
	}
	if changed {
		data, _ := json.Marshal(out)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return &out, err
		}
		if err := writeFileAtomic(path, data); err != nil {
			return &out, err
		}
	}
	return &out, nil
}

// readPosts returns the stored posts without fetching, for the adviser's tool.
func readPosts(dir string) *postsCache {
	var c postsCache
	data, err := os.ReadFile(filepath.Join(dir, postsFileName))
	if err != nil || json.Unmarshal(data, &c) != nil {
		return nil
	}
	return &c
}

// --- matching ------------------------------------------------------------------------------------------------------

// postNotes are a post's lines as notes: the title of an article or of a thread's opening post, and each paragraph
// and list item of the text (a news summary, a forum post's text, or the search's excerpt of an opening post not
// read yet). Short paragraphs stay headings over the notes below them.
func postNotes(p blizzardPost) []hotfixNote {
	date := p.date()
	var notes []hotfixNote
	if p.First {
		notes = append(notes, hotfixNote{Date: date, Text: p.Title})
	}
	text := p.Text
	if text == "" && (p.Source == "news" || p.First) {
		text = p.Excerpt
	}
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && line == t && !strings.HasPrefix(t, "- ") && (len(t) >= 45 || strings.HasSuffix(t, ".")) {
			line = "- " + t
		}
		b.WriteString(line + "\n")
	}
	for _, n := range parseNotes([]feedDay{{Date: date, Text: b.String()}}) {
		if n.Category != "" {
			// A post's headings are paragraphs, such as a class over its changes: a heading like the article's lists.
			n.Path = append([]string{n.Category}, n.Path...)
		}
		notes = append(notes, n)
	}
	return notes
}

// matchPosts lists the recent posts for the character: one whose line names something the character has is "mine",
// one about its class or race "class"; every post stays in the list, newest first, with its link.
func matchPosts(f changeFacts, d *worldData, posts *postsCache, since, forum string) (mine, class, all []changeItem) {
	if posts == nil {
		return nil, nil, nil
	}
	m := newChangeMatcher(f, d)
	for _, p := range posts.Posts {
		item := changeItem{Date: p.date(), Kind: "post", Source: p.Source, Text: clip(p.Title, changesTextMax), URL: p.link(forum)}
		if item.Date < since || item.URL == "" {
			continue
		}
		for _, n := range postNotes(p) {
			it := m.note(n)
			if it.tier != tierMine && (it.tier != tierClass || item.tier != "") {
				continue
			}
			item.tier, item.Reason, item.Name, item.ID, item.Line = it.tier, it.Reason, it.Name, it.ID, ""
			if n.Text != p.Title {
				item.Line = clip(n.Text, changesTextMax)
			}
			if it.tier == tierMine {
				break
			}
		}
		switch item.tier {
		case tierMine:
			mine = append(mine, item)
		case tierClass:
			class = append(class, item)
		}
		all = append(all, item)
	}
	return mine, class, all
}
