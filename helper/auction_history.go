package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The price history of a house: remember (auction.go) appends one row per snapshot to history/<market>/<day>.jsonl;
// a finished day is reduced to <day>.sum.json, the same day format the project's server keeps for every house, so that
// its days can later fill the ones this companion did not see. get_auction_prices reads the summaries.

const (
	auctionHistoryShownDays = 7 // recent days listed per item
	auctionHistoryMinDays   = 3 // finished days needed before a usual price is given
)

// auctionRow is one snapshot in a day's history file: per item the lowest unit price and the quantity on sale.
type auctionRow struct {
	T     int64            `json:"t"`
	Items map[int][2]int64 `json:"items"`
}

// auctionDay is one finished day: per item the median and the lowest of its lowest prices, the median quantity on
// sale, and in how many of the day's snapshots it was listed.
type auctionDay struct {
	Date      string           `json:"date"`
	Snapshots int              `json:"snapshots"`
	Items     map[int][4]int64 `json:"items"`
}

func summarizeAuctionDay(date string, rows []auctionRow) auctionDay {
	units, listed := map[int][]int64{}, map[int][]int64{}
	for _, r := range rows {
		for id, v := range r.Items {
			units[id] = append(units[id], v[0])
			listed[id] = append(listed[id], v[1])
		}
	}
	day := auctionDay{Date: date, Snapshots: len(rows), Items: make(map[int][4]int64, len(units))}
	for id, u := range units {
		day.Items[id] = [4]int64{median(u), slices.Min(u), median(listed[id]), int64(len(u))}
	}
	return day
}

// median of a non-empty list; an even count gives the mean of the middle two, rounded down.
func median(values []int64) int64 {
	v := slices.Clone(values)
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// readAuctionRows reads a day's history file, plain or gzipped; a line that does not parse is skipped.
func readAuctionRows(path string) ([]auctionRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer z.Close()
		r = z
	}
	var rows []auctionRow
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scan.Scan() {
		var row auctionRow
		if json.Unmarshal(scan.Bytes(), &row) == nil && row.Items != nil {
			rows = append(rows, row)
		}
	}
	return rows, scan.Err()
}

// summarizeAuctionHistory writes <day>.sum.json for every finished day in the folder that has rows and no summary yet,
// from cutoff (the oldest day kept) up to the day before today.
func summarizeAuctionHistory(folder, today, cutoff string) error {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	rows := map[string]string{} // day: its rows file
	for _, e := range entries {
		name := e.Name()
		day, rest, _ := strings.Cut(name, ".")
		switch {
		case rest == "sum.json":
			have[day] = true
		case (rest == "jsonl" || rest == "jsonl.gz") && day < today && day >= cutoff:
			rows[day] = filepath.Join(folder, name)
		}
	}
	for day, path := range rows {
		if have[day] {
			continue
		}
		list, err := readAuctionRows(path)
		if err != nil {
			return err
		}
		data, err := json.Marshal(summarizeAuctionDay(day, list))
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(folder, day+".sum.json"), data); err != nil {
			return err
		}
	}
	return nil
}

// auctionHistory tells, for each item seen on at least three finished days of the last 30, its usual lowest price
// (the median of the daily medians), the lowest it went, the last days and how the current price compares. current
// holds each item's lowest price now; today's rows so far appear in the recent days but not in the usual price.
func auctionHistory(folder string, ids []int, current map[int]int64, now time.Time) map[int]mcpObject {
	now = now.UTC()
	today := now.Format(time.DateOnly)
	cutoff := now.AddDate(0, 0, -auctionHistoryDays).Format(time.DateOnly)
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var days []auctionDay
	if entries, err := os.ReadDir(folder); err == nil {
		for _, e := range entries {
			day, rest, _ := strings.Cut(e.Name(), ".")
			if rest != "sum.json" || day < cutoff || day >= today {
				continue
			}
			var d auctionDay
			if data, err := os.ReadFile(filepath.Join(folder, e.Name())); err == nil && json.Unmarshal(data, &d) == nil {
				days = append(days, d)
			}
		}
	}
	slices.SortFunc(days, func(a, b auctionDay) int { return strings.Compare(a.Date, b.Date) })
	finished := len(days)
	if rows, err := readAuctionRows(filepath.Join(folder, today+".jsonl")); err == nil && len(rows) > 0 {
		days = append(days, summarizeAuctionDay(today, rows))
	}
	out := map[int]mcpObject{}
	for id := range want {
		var medians []int64
		low := int64(-1)
		var recent [][]any
		for i, d := range days {
			v, ok := d.Items[id]
			if !ok {
				continue
			}
			if i < finished {
				medians = append(medians, v[0])
				if low < 0 || v[1] < low {
					low = v[1]
				}
			}
			recent = append(recent, []any{d.Date, v[0], v[2]})
		}
		if len(medians) < auctionHistoryMinDays {
			continue
		}
		usual := median(medians)
		entry := mcpObject{"days": len(medians), "usual_unit": usual, "low_unit": low,
			"recent": recent[max(0, len(recent)-auctionHistoryShownDays):]}
		if unit, ok := current[id]; ok && usual > 0 {
			entry["vs_usual_percent"] = int((unit - usual) * 100 / usual)
		}
		out[id] = entry
	}
	return out
}
