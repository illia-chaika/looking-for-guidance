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

var gameLinkType = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
var entrySections = []string{"gear", "bags", "spells", "skills", "talents", "buffs", "references", "reputation"}

// Rows retain the client fields for each kind, including instance-specific item bonuses.
type gameEntry map[string]any

func (e gameEntry) str(key string) string {
	s, _ := e[key].(string)
	return s
}

func (e gameEntry) id() int {
	v, _ := e["id"].(float64)
	if v < 1 || v > 2147483647 || v != float64(int(v)) {
		return 0
	}
	return int(v)
}

func (e gameEntry) valid() bool {
	return slices.Contains(entrySections, e.str("section")) && (e.id() > 0 || e.str("name") != "")
}

const defaultEntryLimit = 12
const maxEntryLimit = 30

const pageHelp = "Return 1–30 entries per page (default 12). If next_offset is present, use it as offset for the next page."

func entryProperties() mcpObject {
	return mcpObject{
		"section": mcpObject{"type": "string", "enum": entrySections},
		"id":      mcpObject{"type": "integer", "minimum": 1, "maximum": 2147483647},
		"query":   mcpObject{"type": "string", "maxLength": 180, "description": "Case-insensitive substring of the localized name, up to 180 characters."},
		"link":    mcpObject{"type": "string", "maxLength": 512, "description": "Exact canonical game link, up to 4096 bytes."},
		"slot":    mcpObject{"type": "integer", "minimum": 0, "description": "Game slot number, e.g. 16 for main hand."},
		"offset":  mcpObject{"type": "integer", "minimum": 0, "description": "Zero-based offset. Copy next_offset from the previous page."},
		"limit":   mcpObject{"type": "integer", "minimum": 1, "maximum": maxEntryLimit, "default": defaultEntryLimit, "description": pageHelp},
		"view":    mcpObject{"type": "string", "enum": []string{"summary", "invested", "purchasable", "all"}, "description": "Default summary returns captured point balances and previews of invested and currently purchasable talents. Use a list view to page; a name or ID without a view searches all."},
		"known":   mcpObject{"type": "boolean", "description": "true: learned spells; false: explicitly marked future spells; omit for both."},
		"pet":     mcpObject{"type": "boolean", "description": "true: pet spellbook; false: player spellbook; omit for both."},
		"kind":    mcpObject{"type": "string", "pattern": gameLinkType.String(), "description": "Native type of an attached game link, such as currency, battlepet, item, quest, spell, player or trade."},
	}
}

func readToolArgs(raw json.RawMessage, v any) error {
	if !json.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return errors.New("tool arguments must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var field *json.UnmarshalTypeError
		if errors.As(err, &field) {
			return fmt.Errorf("invalid %s: expected %s, received %s", field.Field, field.Type, field.Value)
		}
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

type entryArgs struct {
	Snapshot string `json:"snapshot_id"`
	Section  string `json:"section"`
	Query    string `json:"query"`
	ID       *int   `json:"id"`
	Link     string `json:"link"`
	Slot     *int   `json:"slot"`
	Offset   int    `json:"offset"`
	Limit    *int   `json:"limit"`
	View     string `json:"view"`
	Kind     string `json:"kind"`
	Pet      *bool  `json:"pet"`
	Known    *bool  `json:"known"`
}

func (a entryArgs) pageSize() int {
	if a.Limit != nil {
		return *a.Limit
	}
	return defaultEntryLimit
}

func (a entryArgs) validate() error {
	switch {
	case !slices.Contains(entrySections, a.Section):
		return fmt.Errorf("invalid section %q: choose one of %s", a.Section, strings.Join(entrySections, ", "))
	case a.pageSize() < 1 || a.pageSize() > maxEntryLimit:
		return fmt.Errorf("limit %d is outside 1–30; omit limit for 12 or request up to 30, then use next_offset", a.pageSize())
	case a.Offset < 0:
		return fmt.Errorf("offset %d must be zero or positive", a.Offset)
	case a.ID != nil && (*a.ID < 1 || *a.ID > 2147483647):
		return fmt.Errorf("id %d must be in 1–2147483647", *a.ID)
	case a.Slot != nil && *a.Slot < 0:
		return fmt.Errorf("slot %d must be zero or positive", *a.Slot)
	case len([]rune(a.Query)) > 180:
		return errors.New("query must be at most 180 characters")
	case len(a.Link) > 4096:
		return errors.New("link must be at most 4096 bytes")
	case a.Kind != "" && !gameLinkType.MatchString(a.Kind):
		return fmt.Errorf("invalid game link kind %q", a.Kind)
	case a.View != "" && !slices.Contains([]string{"summary", "invested", "purchasable", "all"}, a.View):
		return fmt.Errorf("invalid view %q: choose summary, invested, purchasable or all", a.View)
	}
	return nil
}

func (a entryArgs) matches(row gameEntry) bool {
	if (a.ID != nil && row.id() != *a.ID) || (a.Link != "" && row.str("link") != a.Link) ||
		(a.Slot != nil && fmt.Sprint(row["slot"]) != strconv.Itoa(*a.Slot)) ||
		(a.Query != "" && !strings.Contains(strings.ToLower(row.str("name")), a.Query)) ||
		(a.Kind != "" && row.str("kind") != a.Kind) || (a.Known != nil && row["known"] != *a.Known) {
		return false
	}
	if a.Pet != nil && (row["pet"] == true) != *a.Pet {
		return false
	}
	switch a.View {
	case "invested":
		return investedTalent(row)
	case "purchasable":
		return purchasableTalent(row)
	}
	return true
}

func entryPage(s gameSnapshot, a entryArgs) mcpObject {
	out := mcpObject{"captured": s.Captured, "section": a.Section}
	rows := []gameEntry{}
	total := 0
	for _, row := range s.Entries {
		if row.str("kind") == "recipe_book" || row.str("kind") == "recipe_lookup" || row.str("section") != a.Section || !a.matches(row) {
			continue
		}
		if total >= a.Offset && len(rows) < a.pageSize() {
			rows = append(rows, row)
		}
		total++
	}
	out["entries"], out["total"] = rows, total
	if len(rows) > 0 && a.Offset+len(rows) < total {
		out["next_offset"] = a.Offset + len(rows)
	}
	return out
}

const maxDetailRefs = 6

type gameRef struct {
	Kind    string `json:"kind"`
	ID      int    `json:"id"`
	Link    string `json:"link,omitempty"`
	Section string `json:"section"`
}

var canonicalGameLink = regexp.MustCompile(`^(item|spell):([0-9]+)(:[a-zA-Z0-9:.\-]*)?$`)

func refSchema() mcpObject {
	return mcpObject{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"},
		"properties": mcpObject{
			"kind":    mcpObject{"type": "string", "enum": []string{"item", "spell"}},
			"id":      mcpObject{"type": "integer", "minimum": 1, "maximum": 2147483647},
			"link":    mcpObject{"type": "string", "maxLength": 512, "description": "Exact canonical item:… or spell:… payload from the snapshot. Required to distinguish variants sharing an ID."},
			"section": mcpObject{"type": "string", "enum": []string{"gear", "bags", "spells", "talents", "buffs", "references"}, "description": "Optional for a matching snapshot entry. For an outside candidate, specify an enabled item section (gear or bags) or spells. Use references only for a player-attached link."},
		}}
}

func (r gameRef) validateIdentity() error {
	if r.Kind != "item" && r.Kind != "spell" {
		return errors.New("kind must be item or spell")
	}
	if r.ID < 1 || r.ID > 2147483647 {
		return errors.New("id must be an integer from 1 to 2147483647")
	}
	if r.Link != "" {
		if len(r.Link) > 512 {
			return errors.New("link must be at most 512 bytes")
		}
		m := canonicalGameLink.FindStringSubmatch(r.Link)
		if m == nil || m[1] != r.Kind || m[2] != strconv.Itoa(r.ID) {
			return fmt.Errorf("link must be a canonical %s:%d payload matching kind and id", r.Kind, r.ID)
		}
	}
	return nil
}

func (r gameRef) checkSection(s gameSnapshot) error {
	if r.Section == "references" {
		if !slices.ContainsFunc(s.Entries, func(e gameEntry) bool {
			return e.str("section") == "references" && e.str("kind") == r.Kind && e.id() == r.ID && (r.Link == "" || r.Link == e.str("link"))
		}) {
			return errors.New("no matching player-attached reference in this question; use the exact attached ID and link")
		}
		return nil
	}
	if r.Kind == "item" && r.Section != "gear" && r.Section != "bags" {
		return errors.New("section for an item must be gear, bags, or references")
	}
	if r.Kind == "spell" && r.Section != "spells" && r.Section != "buffs" && r.Section != "talents" {
		return errors.New("section for a spell must be spells, talents, buffs, or references")
	}
	if _, ok := s.Sections[r.Section]; !ok {
		return fmt.Errorf("section %q was disabled or unavailable in this snapshot", r.Section)
	}
	return nil
}

// The player explicitly picked instances when attaching links. Preserve their
// choice even when the model sends only an ID. Two variants require an exact link.
func (r gameRef) attachedVariant(s gameSnapshot) (gameRef, error) {
	if r.Section != "references" || r.Link != "" {
		return r, nil
	}
	var found bool
	for _, e := range s.Entries {
		if e.str("section") != "references" || e.str("kind") != r.Kind || e.id() != r.ID {
			continue
		}
		link := e.str("link")
		if found && r.Link != link {
			return r, errors.New("multiple attached variants share this ID; pass the exact link from the attached references")
		}
		r.Link, found = link, true
	}
	return r, nil
}
