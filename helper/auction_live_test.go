package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveAuction downloads one real house with the Battle.net client in the keychain:
//
//	FOREVER_AUCTION="EU PvE Horde" go test -run TestLiveAuction -v ./helper
func TestLiveAuction(t *testing.T) {
	fields := strings.Fields(os.Getenv("FOREVER_AUCTION"))
	if len(fields) != 3 {
		t.Skip(`set FOREVER_AUCTION to "<region> <ruleset> <faction>", e.g. "EU PvE Horde"`)
	}
	m, err := marketFacts{Region: fields[0], Ruleset: fields[1], Faction: fields[2]}.market()
	if err != nil {
		t.Fatal(err)
	}
	w := newAuctionWatcher(t.TempDir())
	start := time.Now()
	if changed, err := w.update(context.Background(), w.bn, m); err != nil || !changed {
		t.Fatal(changed, err)
	}
	path := filepath.Join(w.dir, m.key()+".json")
	book, err := loadAuctionBook(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	listings := 0
	for _, it := range book.Items {
		listings += it.Listings
	}
	t.Logf("%s: %d items, %d listings, summary %d KB, snapshot %s, took %s", m.label(), len(book.Items), listings,
		info.Size()>>10, book.modifiedAt().Format(time.RFC3339), time.Since(start).Round(time.Millisecond))
	if len(book.Items) < 500 || time.Since(book.modifiedAt()) > 3*time.Hour {
		t.Fatal("expected a busy and fresh house")
	}
	if changed, err := w.update(context.Background(), w.bn, m); err != nil || changed {
		t.Fatal("the same snapshot is not downloaded twice:", changed, err)
	}
	if err := w.refreshItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := loadItemTable(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	candidates, left := upgradeCandidates(book, items, "chest", 10000)
	t.Logf("item table of build %s: %d gear items; chest under 1 gold: %d items sent, %d left out", items.Build, len(items.Items), len(candidates), left)
}
