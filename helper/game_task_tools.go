package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type entryTool struct {
	name, section, description string
	filters                    []string
}

var entryTools = []entryTool{
	{"get_talents", "talents", "Start here for a build or the next point. By default returns a compact summary, recorded points, invested ranks, and candidate entries the client reports as available on purchasable nodes, excluding known unaffordable entries. These flags alone do not guarantee a purchase. Page a list view or search by spell ID or name for more.", []string{"view"}},
	{"search_spellbook", "spells", "Find learned or future player and pet spells, ranks and required levels in this question's captured spellbook.", []string{"pet", "known"}},
	{"get_equipment", "gear", "Read worn items, their exact links, stats and equipment slots. For a comparison ask for the worn slot and use its exact link in get_details.", []string{"slot", "link"}},
	{"search_bags", "bags", "Find carried item variants and stack counts by name, ID or exact link.", []string{"link"}},
	{"get_attached_links", "references", "Read the native game hyperlinks attached to this question, including nonnumeric types and exact item variants. Generic links contain their original type, caption and payload.", []string{"kind", "link"}},
	{"get_skills", "skills", "Read the character's captured skills, proficiency, ranks and modifiers.", nil},
	{"get_buffs", "buffs", "Read the character's captured helpful and harmful auras, including stacks and remaining time when visible.", nil},
	{"get_reputation", "reputation", "Read captured faction names, standing and progress toward the next threshold.", nil},
}

func gameEntryTool(name string) *entryTool {
	for i := range entryTools {
		if entryTools[i].name == name {
			return &entryTools[i]
		}
	}
	return nil
}

func taskEntrySection(name string) string {
	if tool := gameEntryTool(name); tool != nil {
		return tool.section
	}
	return ""
}

func (t entryTool) fields() []string {
	return append([]string{"id", "query", "offset", "limit"}, t.filters...)
}

func (t entryTool) properties() mcpObject {
	all, result := entryProperties(), mcpObject{}
	for _, name := range t.fields() {
		result[name] = all[name]
	}
	return result
}

// The legacy endpoint remains callable by older chats with a cached tool declaration.
func queryTaskEntries(dir, name string, raw json.RawMessage) (mcpObject, error) {
	var a entryArgs
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	t := gameEntryTool(name)
	var fields []string
	if t != nil {
		fields = t.fields()
		a.Section = t.section
	} else if name == "get_entries" {
		fields = []string{"section", "id", "query", "offset", "limit", "slot", "link"}
	} else {
		return nil, errors.New("unknown game tool")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	for key, val := range keys {
		if key != "snapshot_id" && !slices.Contains(fields, key) {
			return nil, fmt.Errorf("%s does not accept %q; supported fields: snapshot_id, %s", name, key, strings.Join(fields, ", "))
		}
		if bytes.Equal(bytes.TrimSpace(val), []byte("null")) {
			return nil, fmt.Errorf("%s cannot be null; omit optional fields", key)
		}
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	a.Query = strings.ToLower(strings.TrimSpace(a.Query))
	if name == "get_talents" {
		if a.View == "" {
			a.View = "summary"
			if a.ID != nil || a.Query != "" || a.Offset > 0 {
				a.View = "all"
			}
		}
		if a.View == "summary" && (a.ID != nil || a.Query != "" || a.Offset > 0) {
			return nil, errors.New("summary has no id, query or offset; use view=all, invested or purchasable for a filtered page")
		}
	}
	s, err := loadGameSnapshot(dir, a.Snapshot)
	if err != nil {
		return nil, err
	}
	summary, available := s.Sections[a.Section]
	hasRows := slices.ContainsFunc(s.Entries, func(r gameEntry) bool { return r.str("section") == a.Section })
	if !hasRows && !available && a.Section != "references" {
		return nil, fmt.Errorf("%s was disabled or unavailable in this snapshot", a.Section)
	}
	var out mcpObject
	if a.View == "summary" {
		out = talentSummary(s, summary, hasRows, a.pageSize())
	} else {
		out = entryPage(s, a)
		if a.View != "" {
			out["view"] = a.View
		}
	}
	if !hasRows && summary != "" {
		if s.Lookup {
			out["section_summary"] = trimText(summary, 1500)
		} else {
			out["legacy_summary"] = trimText(summary, 1500)
			out["note"] = "This older snapshot stored a text summary. Individual cards were not captured."
		}
	}
	return out, nil
}

var talentCurrency = regexp.MustCompile(`tree ([0-9]+) currency ([0-9]+): ([0-9]+) unspent, ([0-9]+) spent`)

func investedTalent(row gameEntry) bool {
	if purchased, known := row["purchasedRank"].(float64); known {
		return purchased > 0
	}
	rank, _ := row["rank"].(float64)
	return rank > 0
}

func purchasableTalent(row gameEntry) bool {
	// canPurchase comes from the node; a choice node can have unavailable or capped entries.
	if row["canPurchase"] != true || row["available"] != true || row["canAfford"] == false {
		return false
	}
	rank, hasRank := row["rank"].(float64)
	max, hasMax := row["maxRank"].(float64)
	return !hasRank || !hasMax || rank < max
}

func talentSummary(s gameSnapshot, summary string, hasRows bool, limit int) mcpObject {
	out := mcpObject{"captured": s.Captured, "section": "talents", "view": "summary"}
	var points []map[string]int
	for _, m := range talentCurrency.FindAllStringSubmatch(summary, -1) {
		p := map[string]int{}
		for i, key := range []string{"tree_id", "currency_id", "unspent", "spent"} {
			p[key], _ = strconv.Atoi(m[i+1])
		}
		points = append(points, p)
	}
	if len(points) > 0 {
		out["points"] = points
	}
	if !hasRows {
		out["entries_available"] = false
		return out
	}
	if slices.ContainsFunc(s.Entries, func(row gameEntry) bool {
		return row.str("section") == "talents" && row["purchasedRank"] == nil && investedTalent(row)
	}) {
		out["rank_note"] = "For entries without purchasedRank, invested uses active rank from an older or incomplete snapshot; a granted rank can appear there."
	}
	for _, view := range []string{"invested", "purchasable"} {
		page := entryPage(s, entryArgs{Section: "talents", View: view, Limit: &limit})
		delete(page, "captured")
		delete(page, "section")
		// Previews omit full descriptions. Targeted and paginated list views return the original records.
		rows := page["entries"].([]gameEntry)
		for i, row := range rows {
			compact := gameEntry{}
			for _, key := range []string{"id", "name", "rank", "purchasedRank", "maxRank", "available", "canPurchase", "canAfford", "entry", "node"} {
				if value, ok := row[key]; ok {
					compact[key] = value
				}
			}
			rows[i] = compact
		}
		out[view] = page
	}
	return out
}
