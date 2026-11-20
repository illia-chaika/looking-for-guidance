package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	maxAuctionItems    = 50
	auctionShownTiers  = 5
	auctionStaleAfter  = 6 * time.Hour
	auctionToolMessage = "Asking prices on this character's own auction house from Blizzard's snapshot: lowest buyout per unit in copper and the quantity on sale. They are not sale prices; a non-commodity stack is bought whole."
)

func auctionToolSchema() mcpObject {
	return mcpObject{
		"items": mcpObject{"type": "array", "minItems": 1, "maxItems": maxAuctionItems,
			"items": mcpObject{"type": "object", "additionalProperties": false, "required": []string{"id"},
				"properties": mcpObject{
					"id":   mcpObject{"type": "integer", "minimum": 1, "description": "Item ID"},
					"link": mcpObject{"type": "string", "maxLength": 512, "description": "Exact item link of a carried or worn variant, to price that random suffix"},
				}}},
		"quantity": mcpObject{"type": "integer", "minimum": 1, "maximum": 100000, "description": "Optional: also return what buying this many of each would cost"},
	}
}

// auctionReady tells whether this computer downloads auction data at all; without it the tool is not offered.
func auctionReady(dir string) bool {
	books, _ := filepath.Glob(filepath.Join(auctionDir(dir), "*-*.json"))
	return len(books) > 0
}

type auctionArgs struct {
	ID    string `json:"snapshot_id"`
	Items []struct {
		ID   int    `json:"id"`
		Link string `json:"link,omitempty"`
	} `json:"items"`
	Quantity int `json:"quantity,omitempty"`
}

func queryAuctionPrices(dir string, raw []byte) (mcpObject, error) {
	var a auctionArgs
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	if len(a.Items) < 1 || len(a.Items) > maxAuctionItems || a.Quantity < 0 || a.Quantity > 100000 {
		return nil, fmt.Errorf("items must list 1 to %d item IDs and quantity must be 1 to 100000", maxAuctionItems)
	}
	s, err := loadGameSnapshot(dir, a.ID)
	if err != nil {
		return nil, err
	}
	m, err := snapshotMarket(s)
	if err != nil {
		return nil, errors.New("auction prices are unavailable for this question: " + err.Error())
	}
	book, err := loadAuctionBook(filepath.Join(auctionDir(dir), m.key()+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no snapshot of the %s auction house has been downloaded yet; the companion fetches it within minutes of the game reporting the market", m.label())
	} else if err != nil {
		return nil, err
	}
	fetched := time.UnixMilli(book.Fetched).UTC()
	out := mcpObject{"captured": fetched.Format(time.RFC3339), "market": m.label(), "note": auctionToolMessage}
	if at := book.modifiedAt(); !at.IsZero() {
		out["snapshot_at"] = at.UTC().Format(time.RFC3339)
		out["snapshot_age_minutes"] = int(time.Since(at).Minutes())
		if time.Since(at) > auctionStaleAfter {
			out["stale"] = true
		}
	}
	var items []mcpObject
	for _, want := range a.Items {
		if want.ID <= 0 {
			return nil, errors.New("item IDs start at 1")
		}
		items = append(items, auctionEntry(book.Items[want.ID], want.ID, want.Link, a.Quantity))
	}
	out["items"] = items
	return out, nil
}

func auctionEntry(it *auctionItem, id int, link string, quantity int) mcpObject {
	entry := mcpObject{"id": id}
	if it == nil {
		entry["listed"] = 0
		return entry
	}
	variants := it.Variants
	if link != "" {
		bonus, tuning, err := linkVariant(link, id)
		if err != nil {
			entry["link_problem"] = err.Error()
		} else {
			entry["variant"] = mcpObject{"bonus": bonus}
			variants = slices.DeleteFunc(slices.Clone(variants), func(v *auctionVariant) bool {
				return joinInts(v.Bonus) != joinInts(bonus) || (tuning != 0 && v.Tuning != tuning)
			})
			if len(variants) == 0 {
				entry["listed"] = 0
				entry["other_variants"] = variantSummaries(it.Variants)
				return entry
			}
		}
	}
	var tiers [][2]int64
	for _, v := range variants {
		entry["listed"] = asInt(entry["listed"]) + v.Listed
		entry["listings"] = asInt(entry["listings"]) + v.Listings
		tiers = append(tiers, v.Tiers...)
	}
	slices.SortFunc(tiers, func(a, b [2]int64) int { return int(a[0] - b[0]) })
	entry["unit"] = tiers[0][0]
	entry["cheapest"] = tiers[:min(len(tiers), auctionShownTiers)]
	if quantity > 0 {
		left, cost := int64(quantity), int64(0)
		for _, t := range tiers {
			take := min(left, t[1])
			cost += take * t[0]
			if left -= take; left == 0 {
				break
			}
		}
		entry["cost_for_quantity"] = cost
		if left > 0 {
			entry["quantity_priced"] = int64(quantity) - left
		}
	}
	if link == "" && len(it.Variants) > 1 {
		entry["variants"] = variantSummaries(it.Variants)
	}
	return entry
}

func asInt(v any) int {
	n, _ := v.(int)
	return n
}

func variantSummaries(variants []*auctionVariant) []mcpObject {
	var out []mcpObject
	for _, v := range variants[:min(len(variants), auctionShownTiers)] {
		summary := mcpObject{"unit": v.Unit, "listed": v.Listed}
		if len(v.Bonus) > 0 {
			summary["bonus"] = v.Bonus
		}
		if v.Tuning != 0 {
			summary["content_tuning"] = v.Tuning
		}
		out = append(out, summary)
	}
	return out
}

var itemPayload = regexp.MustCompile(`item:[0-9]+[0-9:\-]*`)

// linkVariant reads the bonus IDs and the ContentTuningID modifier from an item link:
// item:ID:enchant:gem1:gem2:gem3:gem4:suffix:unique:level:spec:modifiersMask:context:numBonus:bonus…:numModifiers:type:value…
func linkVariant(link string, id int) (bonus []int, tuning int, err error) {
	fields := strings.Split(itemPayload.FindString(link), ":")
	if len(fields) < 2 || fields[1] != strconv.Itoa(id) {
		return nil, 0, errors.New("the link is not for this item ID")
	}
	at := func(i int) int {
		if i >= len(fields) {
			return 0
		}
		n, _ := strconv.Atoi(fields[i])
		return n
	}
	count := at(13)
	if count < 0 || count > 16 {
		return nil, 0, errors.New("the link has an unreadable bonus list")
	}
	for i := 0; i < count; i++ {
		bonus = append(bonus, at(14+i))
	}
	slices.Sort(bonus)
	mods := at(14 + count)
	for i := 0; i < mods && i < 16; i++ {
		if at(15+count+2*i) == auctionContentTune {
			tuning = at(16 + count + 2*i)
		}
	}
	return bonus, tuning, nil
}
