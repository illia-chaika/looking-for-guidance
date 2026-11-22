package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Blizzard keeps one running article of WoW: Forever hotfixes. Only the en-us edition is current.
const (
	defaultHotfixURL = "https://news.blizzard.com/en-us/article/24315281"
	feedMaxAge       = 3 * time.Hour
	feedMaxBytes     = 2 << 20
	feedDays         = 14
)

type feedDay struct {
	Date string `json:"date"` // 2026-11-11
	Text string `json:"text"`
}

type gameFeed struct {
	URL     string    `json:"url"`
	Fetched int64     `json:"fetched"`
	Hash    string    `json:"hash"`
	Days    []feedDay `json:"days"`
}

// newest returns the text of the most recent days, newest first, and the dates it covers.
func (f *gameFeed) newest(days int) string {
	var b strings.Builder
	for i, d := range f.Days {
		if i >= days {
			break
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", d.Date, d.Text)
	}
	return b.String()
}

func (f *gameFeed) day(date string) string {
	for _, d := range f.Days {
		if d.Date == date {
			return d.Text
		}
	}
	return ""
}

var (
	feedScript = regexp.MustCompile(`(?is)<(script|style|template)\b.*?</(script|style|template)>`)
	feedTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	feedSpaces = regexp.MustCompile(`[ \t\r\n\x{00a0}]+`)
)

// parseHotfixes turns the article into indented plain text per day. A paragraph that is only a date starts a day.
func parseHotfixes(page string) []feedDay {
	var days []feedDay
	var current *feedDay
	var body []string
	end := func() {
		if current != nil && len(body) > 0 {
			current.Text = strings.Join(body, "\n")
			days = append(days, *current)
		}
		current, body = nil, nil
	}
	for _, l := range htmlLines(page) {
		if when, err := time.Parse("January 2, 2006", strings.TrimSpace(l)); err == nil {
			end()
			current = &feedDay{Date: when.Format("2006-01-02")}
			continue
		}
		if current != nil {
			body = append(body, l)
		}
	}
	end()
	if len(days) > feedDays {
		days = days[:feedDays]
	}
	return days
}

// htmlLines turns a page into plain lines: one per paragraph or heading, list items as "- " indented two spaces per
// level of nesting.
func htmlLines(page string) []string {
	page = feedScript.ReplaceAllString(page, " ")
	var lines []string
	var line strings.Builder
	depth := 0
	flush := func() {
		text := strings.TrimSpace(feedSpaces.ReplaceAllString(html.UnescapeString(line.String()), " "))
		line.Reset()
		if text == "" || text == "-" {
			return
		}
		lines = append(lines, strings.Repeat("  ", max(depth-1, 0))+text)
	}
	pos := 0
	for _, loc := range feedTag.FindAllStringIndex(page, -1) {
		line.WriteString(page[pos:loc[0]])
		pos = loc[1]
		tag := strings.ToLower(strings.Trim(page[loc[0]:loc[1]], "<>/ "))
		name, _, _ := strings.Cut(tag, " ")
		closing := strings.HasPrefix(page[loc[0]:], "</")
		switch name {
		case "ul", "ol":
			flush()
			if closing {
				depth = max(depth-1, 0)
			} else {
				depth++
			}
		case "li":
			flush()
			if !closing {
				line.WriteString("- ")
			}
		case "p", "br", "hr", "h1", "h2", "h3", "h4", "div", "section":
			flush()
		}
	}
	line.WriteString(page[pos:])
	flush()
	return lines
}

func fetchPage(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "looking-for-guidance/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, feedMaxBytes))
	return string(data), err
}

// Blizzard's "Known Issues" thread for WoW: Forever. Discourse serves the posts as plain text under /raw/.
const (
	defaultIssuesURL  = "https://us.forums.blizzard.com/en/wow/raw/2402363"
	defaultIssuesPage = "https://us.forums.blizzard.com/en/wow/t/2402363"
)

type cachedPage struct {
	URL     string `json:"url"`
	Fetched int64  `json:"fetched"`
	Text    string `json:"text"`
}

// loadIssues returns the known-issues text, or an empty string when it cannot be read: the list is a
// convenience for the quest check, which still has the hotfix notes and web search.
func loadIssues(ctx context.Context, dir, url string, now time.Time, fetch func(context.Context, string) (string, error)) string {
	path := filepath.Join(dir, "known-issues.json")
	var cached cachedPage
	if data, err := os.ReadFile(path); err != nil || json.Unmarshal(data, &cached) != nil || cached.URL != url {
		cached = cachedPage{}
	}
	if cached.Text != "" && !feedStale(cached.Fetched, now, filepath.Join(filepath.Dir(dir), "world"), func(f *changesFile) string { return f.Issues.ChangedAt }) {
		return cached.Text
	}
	text, err := fetch(ctx, url)
	text = strings.TrimSpace(text)
	if err != nil || text == "" || strings.Contains(text, "<html") {
		if err != nil {
			logf("Known issues not refreshed: %v", err)
		}
		return cached.Text
	}
	if len(text) > 24<<10 {
		text = text[:24<<10]
	}
	data, _ := json.Marshal(cachedPage{URL: url, Fetched: now.Unix(), Text: text})
	if os.MkdirAll(dir, 0o700) == nil {
		writeFileAtomic(path, data)
	}
	return text
}

// loadFeed returns the cached hotfix list, refreshing it when the backend's changes feed says the page changed
// (without that feed: when it is older than feedMaxAge).
// A failed refresh keeps the previous copy: stale notes with a date are better than none.
func loadFeed(ctx context.Context, dir, url string, now time.Time, fetch func(context.Context, string) (string, error)) (*gameFeed, error) {
	path := filepath.Join(dir, "feed.json")
	var cached gameFeed
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &cached) == nil && cached.URL == url && len(cached.Days) > 0 {
		if !feedStale(cached.Fetched, now, filepath.Join(filepath.Dir(dir), "world"), func(f *changesFile) string { return f.Hotfixes.ChangedAt }) {
			return &cached, nil
		}
	} else {
		cached = gameFeed{}
	}
	page, err := fetch(ctx, url)
	if err == nil {
		if days := parseHotfixes(page); len(days) > 0 {
			sum := sha256.Sum256([]byte(fmt.Sprint(days)))
			fresh := gameFeed{URL: url, Fetched: now.Unix(), Hash: hex.EncodeToString(sum[:8]), Days: days}
			data, _ := json.Marshal(fresh)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			return &fresh, writeFileAtomic(path, data)
		}
		err = errors.New("the hotfix page has no dated entries")
	}
	if len(cached.Days) > 0 {
		logf("Hotfix list not refreshed: %v", err)
		return &cached, nil
	}
	return nil, err
}
