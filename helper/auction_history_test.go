package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRows(t *testing.T, path string, rows ...auctionRow) {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		line, _ := json.Marshal(r)
		b.Write(append(line, '\n'))
	}
	if strings.HasSuffix(path, ".gz") {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		z := gzip.NewWriter(f)
		z.Write([]byte(b.String()))
		z.Close()
		f.Close()
		return
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuctionDaySummaryKeepsMedianLowestAndPresence(t *testing.T) {
	day := summarizeAuctionDay("2026-11-21", []auctionRow{
		{T: 1, Items: map[int][2]int64{2589: {30, 100}, 7469: {42000, 1}}},
		{T: 2, Items: map[int][2]int64{2589: {24, 125}}},
		{T: 3, Items: map[int][2]int64{2589: {26, 90}}},
		{T: 4, Items: map[int][2]int64{2589: {25, 80}, 7469: {30000, 2}}},
	})
	if day.Snapshots != 4 || day.Items[2589] != [4]int64{25, 24, 95, 4} || day.Items[7469] != [4]int64{36000, 30000, 1, 2} {
		t.Fatalf("%+v", day)
	}
}

func TestAuctionHistorySummarisesFinishedDaysOnce(t *testing.T) {
	folder := t.TempDir()
	writeRows(t, filepath.Join(folder, "2026-11-19.jsonl.gz"), auctionRow{T: 1, Items: map[int][2]int64{2589: {20, 50}}})
	writeRows(t, filepath.Join(folder, "2026-11-20.jsonl"), auctionRow{T: 2, Items: map[int][2]int64{2589: {22, 60}}})
	writeRows(t, filepath.Join(folder, "2026-11-21.jsonl"), auctionRow{T: 3, Items: map[int][2]int64{2589: {99, 1}}})
	writeRows(t, filepath.Join(folder, "2026-10-01.jsonl.gz"), auctionRow{T: 0, Items: map[int][2]int64{2589: {1, 1}}})
	os.WriteFile(filepath.Join(folder, "2026-11-18.sum.json"), []byte(`{"date":"2026-11-18","snapshots":9,"items":{}}`), 0o600)

	if err := summarizeAuctionHistory(folder, "2026-11-21", "2026-10-22"); err != nil {
		t.Fatal(err)
	}
	var names []string
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sum.json") {
			names = append(names, e.Name())
		}
	}
	if strings.Join(names, " ") != "2026-11-18.sum.json 2026-11-19.sum.json 2026-11-20.sum.json" {
		t.Fatal("finished days within 30 days get a summary, today and older days none:", names)
	}
	data, _ := os.ReadFile(filepath.Join(folder, "2026-11-18.sum.json"))
	if !strings.Contains(string(data), `"snapshots":9`) {
		t.Fatal("an existing summary is kept:", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(folder, "2026-11-19.sum.json"))
	if string(data) != `{"date":"2026-11-19","snapshots":1,"items":{"2589":[20,20,50,1]}}` {
		t.Fatal("a gzipped day is read too:", string(data))
	}
}

func TestAuctionHistoryGivesTheUsualPriceFromThreeDays(t *testing.T) {
	folder := t.TempDir()
	put := func(day auctionDay) {
		data, _ := json.Marshal(day)
		os.WriteFile(filepath.Join(folder, day.Date+".sum.json"), data, 0o600)
	}
	for i, unit := range []int64{100, 120, 110, 130, 90, 105, 115, 125} {
		date := time.Date(2026, 11, 13+i, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		put(auctionDay{Date: date, Snapshots: 20, Items: map[int][4]int64{2589: {unit, unit - 10, 50 + int64(i), 20}, 7469: {5000, 5000, 1, 3}}})
	}
	put(auctionDay{Date: "2026-10-01", Items: map[int][4]int64{2589: {1, 1, 1, 1}}})
	writeRows(t, filepath.Join(folder, "2026-11-21.jsonl"), auctionRow{T: 1, Items: map[int][2]int64{2589: {150, 40}}})
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	h := auctionHistory(folder, []int{2589, 4306}, map[int]int64{2589: 150}, now)
	linen := h[2589]
	if linen == nil || linen["days"] != 8 || linen["usual_unit"] != int64(112) || linen["low_unit"] != int64(80) || linen["vs_usual_percent"] != 33 {
		t.Fatalf("usual price over finished days only, without the 31-day-old one: %+v", linen)
	}
	recent := linen["recent"].([][]any)
	if len(recent) != 7 || recent[6][0] != "2026-11-21" || recent[6][1] != int64(150) || recent[0][0] != "2026-11-15" {
		t.Fatalf("the last seven days, today's rows included: %v", recent)
	}
	if _, ok := h[4306]; ok {
		t.Fatal("an item without days has no history")
	}

	short := t.TempDir()
	put2 := func(date string) {
		os.WriteFile(filepath.Join(short, date+".sum.json"), []byte(`{"date":"`+date+`","snapshots":1,"items":{"2589":[10,10,1,1]}}`), 0o600)
	}
	put2("2026-11-19")
	put2("2026-11-20")
	if h := auctionHistory(short, []int{2589}, nil, now); len(h) != 0 {
		t.Fatal("two days give no usual price:", h)
	}
}

func TestNextAuctionCheckFollowsBlizzardsHour(t *testing.T) {
	lm := time.Date(2026, 11, 22, 8, 22, 51, 0, time.UTC)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 22, h, m, 0, 0, time.UTC) }
	cases := []struct {
		modified, lastTry, now, want time.Time
	}{
		{time.Time{}, time.Time{}, at(9, 0), at(9, 0)},          // no snapshot yet: now
		{lm, at(8, 40), at(9, 0), lm.Add(65 * time.Minute)},     // the next one cannot be there yet
		{lm, at(9, 20), at(9, 28), at(9, 30)},                   // due: every 10 minutes
		{lm, time.Time{}, at(9, 28), lm.Add(65 * time.Minute)},  // never tried: when it was due
		{lm, at(10, 0), at(10, 5), at(10, 20)},                  // half an hour late: every 20 minutes
		{lm.Add(-3 * time.Hour), at(9, 0), at(9, 1), at(9, 20)}, // a skipped hour keeps the slow pace
		{lm, at(8, 0), at(8, 30), lm.Add(65 * time.Minute)},     // a try before the snapshot changes nothing
	}
	for i, c := range cases {
		if got := nextAuctionCheck(c.modified, c.lastTry, c.now); !got.Equal(c.want) {
			t.Errorf("case %d: got %s, want %s", i, got.Format("15:04:05"), c.want.Format("15:04:05"))
		}
	}
}

func TestMarketArgumentFillsInTheCharactersMarket(t *testing.T) {
	own, _ := marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Horde"}.market()
	cases := []struct {
		arg   marketArg
		key   string
		label string
	}{
		{marketArg{Faction: "Neutral"}, "eu-117", "EU Normal Neutral (house 117)"},
		{marketArg{Faction: "Alliance"}, "eu-111", "EU Normal Alliance (house 111)"},
		{marketArg{Region: "US"}, "us-114", "US Normal Horde (house 114)"},
		{marketArg{Ruleset: "PvP", Faction: "Neutral"}, "eu-116", "EU PvP Neutral (house 116)"},
	}
	for _, c := range cases {
		m, err := c.arg.apply(own, nil)
		if err != nil || m.key() != c.key || m.label() != c.label {
			t.Errorf("%+v: got %s %q, %v", c.arg, m.key(), m.label(), err)
		}
	}
	if _, err := (marketArg{Ruleset: "RP"}).apply(own, nil); err == nil || !strings.Contains(err.Error(), "not known yet") {
		t.Fatal("a Roleplay house is not guessed:", err)
	}
	hardcore := errors.New("hardcore characters trade on a market the API does not list")
	if _, err := (marketArg{Faction: "Alliance"}).apply(auctionMarket{}, hardcore); err != hardcore {
		t.Fatal("without a market of its own the argument must name all three:", err)
	}
	if m, err := (marketArg{Region: "KR", Ruleset: "PvE", Faction: "Horde"}).apply(auctionMarket{}, hardcore); err != nil || m.key() != "kr-114" {
		t.Fatal(m, err)
	}
	if _, err := (marketFacts{Region: "EU", Ruleset: "PvE", Faction: "Neutral"}).market(); err == nil {
		t.Fatal("a character's own market is still its faction's")
	}
}
