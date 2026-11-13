package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseHotfixes(t *testing.T) {
	page, err := os.ReadFile("testdata/hotfixes.html")
	if err != nil {
		t.Fatal(err)
	}
	days := parseHotfixes(string(page))
	if len(days) != 3 || days[0].Date != "2026-11-11" || days[1].Date != "2026-11-10" || days[2].Date != "2026-11-09" {
		t.Fatalf("days: %+v", days)
	}
	if strings.Contains(days[0].Text, "duplicate inside a script") || strings.Contains(days[0].Text, "<") {
		t.Fatalf("markup or script text leaked:\n%s", days[0].Text)
	}
	for _, want := range []string{
		"Classes\n- Druid",
		"    - Stormstrike damage reduced to 90% weapon damage (was 120%)",
		"- Journeyman Campfire Kit can now be placed with Cooking 90, the skill its recipe asks for (was Cooking 140).",
	} {
		if !strings.Contains(days[1].Text, want) {
			t.Fatalf("10 November lacks %q:\n%s", want, days[1].Text)
		}
	}
	if !strings.Contains(days[2].Text, "Enemies in dungeons now award 50% more experience.") {
		t.Fatalf("9 November:\n%s", days[2].Text)
	}
}

func TestLoadFeedCachesAndKeepsAStaleCopy(t *testing.T) {
	dir := t.TempDir()
	page, _ := os.ReadFile("testdata/hotfixes.html")
	calls := 0
	fetch := func(context.Context, string) (string, error) { calls++; return string(page), nil }
	now := time.Unix(1794480000, 0)
	first, err := loadFeed(context.Background(), dir, "https://example.test/a", now, fetch)
	if err != nil || len(first.Days) != 3 || first.Hash == "" {
		t.Fatalf("first load: %+v %v", first, err)
	}
	if _, err := loadFeed(context.Background(), dir, "https://example.test/a", now.Add(time.Hour), fetch); err != nil || calls != 1 {
		t.Fatalf("a fresh copy was fetched again: calls=%d err=%v", calls, err)
	}
	broken := func(context.Context, string) (string, error) { calls++; return "", errors.New("offline") }
	stale, err := loadFeed(context.Background(), dir, "https://example.test/a", now.Add(5*time.Hour), broken)
	if err != nil || stale.Hash != first.Hash || calls != 2 {
		t.Fatalf("a failed refresh must keep the old copy: %+v %v", stale, err)
	}
	if _, err := loadFeed(context.Background(), t.TempDir(), "https://example.test/a", now, broken); err == nil {
		t.Fatal("no copy and no network must be an error")
	}
}
