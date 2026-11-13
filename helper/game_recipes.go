package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func recipeProperties() mcpObject {
	fields := entryProperties()
	return mcpObject{
		"profession":    mcpObject{"type": "string", "maxLength": 180, "description": "Substring of the profession's localized name."},
		"profession_id": mcpObject{"type": "integer", "minimum": 1, "maximum": 2147483647, "description": "Profession skill line ID."},
		"id":            mcpObject{"type": "integer", "minimum": 1, "maximum": 2147483647, "description": "Recipe spell ID, not a product item ID."},
		"query":         fields["query"], "offset": fields["offset"], "limit": fields["limit"],
		"known": mcpObject{"type": "boolean", "description": "true: recipes seen as learned, in the open book (learned) or at an earlier look (learned_at_observation; a learned recipe stays learned). false: recipes not learned according to the currently open book only. Omit to include every observation."},
	}
}

type recipeQuery struct {
	Snapshot     string `json:"snapshot_id"`
	Profession   string `json:"profession"`
	ProfessionID *int   `json:"profession_id"`
	ID           *int   `json:"id"`
	Query        string `json:"query"`
	Known        *bool  `json:"known"`
	Offset       int    `json:"offset"`
	Limit        *int   `json:"limit"`
}

func queryRecipes(dir string, raw json.RawMessage) (mcpObject, error) {
	var q recipeQuery
	if err := readToolArgs(raw, &q); err != nil {
		return nil, err
	}
	page := entryArgs{Section: "skills", Query: q.Query, ID: q.ID, Offset: q.Offset, Limit: q.Limit}
	if err := page.validate(); err != nil {
		return nil, err
	}
	if len([]rune(q.Profession)) > 180 || q.ProfessionID != nil && (*q.ProfessionID < 1 || *q.ProfessionID > 2147483647) {
		return nil, fmt.Errorf("profession must be at most 180 characters; profession_id must be in 1–2147483647")
	}
	s, err := loadGameSnapshot(dir, q.Snapshot)
	if err != nil {
		return nil, err
	}
	q.Profession = strings.ToLower(strings.TrimSpace(q.Profession))
	q.Query = strings.ToLower(strings.TrimSpace(q.Query))
	books, items := []mcpObject{}, []mcpObject{}
	total := 0
	exactObserved := false
	add := func(book gameEntry, id int, name string, observed any, details map[string]any) {
		if id <= 0 || q.ID != nil && id != *q.ID || !strings.Contains(strings.ToLower(name), q.Query) {
			return
		}
		live := book.str("freshness") == "open"
		learned, explicit := observed.(bool)
		if live && explicit && q.ID != nil {
			exactObserved = true
		}
		// A recipe seen as learned stays learned, whenever the book was read. One seen as not learned may have
		// been learned since, so that answer counts only from the book open now.
		if q.Known != nil && (!explicit || learned != *q.Known || !learned && !live) {
			return
		}
		total++
		if total <= q.Offset || len(items) >= page.pageSize() {
			return
		}
		row := mcpObject{"recipe_id": id, "name": name, "profession_id": book.id(), "freshness": book.str("freshness"), "observed_at": book["observedAt"]}
		if explicit {
			if live {
				row["learned"] = learned
			} else {
				row["learned_at_observation"] = learned
			}
		}
		if live && details != nil {
			row["selected"] = true
			for _, k := range []string{"requirements", "reagentSlots", "outputItemID", "sourceText"} {
				if v, ok := details[k]; ok {
					row[k] = v
				}
			}
		}
		items = append(items, row)
	}
	status := "unavailable"
	for _, book := range s.Entries {
		if book.str("section") != "skills" || book.str("kind") != "recipe_book" || (q.ProfessionID != nil && *q.ProfessionID != book.id()) || !strings.Contains(strings.ToLower(book.str("name")), q.Profession) {
			continue
		}
		books = append(books, mcpObject{"profession_id": book.id(), "name": book.str("name"), "rank": book["rank"], "max_rank": book["maxRank"], "observed_at": book["observedAt"], "freshness": book.str("freshness"), "coverage": book.str("coverage")})
		if status == "unavailable" {
			status = "ok"
		}
		if book.str("freshness") != "open" || book.str("coverage") != "reported_catalog" {
			status = "partial"
		}
		selected, _ := book["selected"].(map[string]any)
		choice := gameEntry(selected)
		selectedID, _ := choice["recipeID"].(float64)
		rows, _ := book["recipes"].([]any)
		included := false
		for _, v := range rows {
			tuple, ok := v.([]any)
			if !ok || len(tuple) != 3 {
				continue
			}
			id, _ := tuple[0].(float64)
			name, _ := tuple[1].(string)
			flag, hasFlag := tuple[2].(float64)
			if name == "" || id <= 0 {
				continue
			}
			var obs any
			if hasFlag && (flag == 0 || flag == 1) {
				obs = flag == 1
			}
			if id == selectedID {
				included = true
				add(book, int(id), name, selected["learned"], selected)
			} else {
				add(book, int(id), name, obs, nil)
			}
		}
		if !included && selectedID > 0 && book.str("freshness") == "open" {
			add(book, int(selectedID), choice.str("name"), choice["learned"], selected)
		}
	}
	out := mcpObject{"captured": s.Captured, "status": status, "books": books, "entries": items, "total": total,
		"note": "learned is read from the book open now. learned_at_observation true still holds: a learned recipe stays learned. learned_at_observation false may be out of date; open the book to confirm. No matching row means unknown; filters, hidden recipes and the capture limit can omit entries. Profession rank is not a recipe's learning requirement. Check Forever sources for acquisition and required rank."}
	if len(books) == 0 {
		if _, ok := s.Sections["skills"]; ok {
			out["reason"] = "No matching own profession book was observed. Open your own profession and ask again."
		} else {
			out["reason"] = "Skills were disabled or unavailable for this question."
		}
	} else if total == 0 {
		out["status"] = "partial"
		out["match"] = "unknown"
		if exactObserved {
			out["match"] = "excluded_by_filter"
		}
	}
	if q.Offset+len(items) < total {
		out["next_offset"] = q.Offset + len(items)
	}
	_, skillsEnabled := s.Sections["skills"]
	if q.ID != nil && !exactObserved && skillsEnabled && s.Lookup && validIdentity(s.Character, s.Login) &&
		slices.ContainsFunc(s.Entries, func(e gameEntry) bool { return e.str("section") == "skills" && e.str("kind") == "recipe_lookup" }) {
		card, reason := q.probeRecipe(dir, s)
		if card != nil {
			learned := card["learned"].(bool)
			book := mcpObject{"profession_id": card["professionID"], "name": card.str("professionName"), "rank": card["rank"], "max_rank": card["maxRank"], "read_at": card.str("read_at"), "freshness": "live_lookup", "coverage": "one_recipe"}
			out["books"], out["status"], out["match"] = []mcpObject{book}, "ok", "exact"
			delete(out, "reason")
			delete(out, "next_offset")
			out["entries"], out["total"] = []mcpObject{}, 0
			if (q.Known == nil || *q.Known == learned) && strings.Contains(strings.ToLower(card.str("name")), q.Query) {
				out["total"] = 1
				if q.Offset == 0 {
					row := mcpObject{"recipe_id": *q.ID, "name": card.str("name"), "learned": learned, "profession_id": card["professionID"], "freshness": "live_lookup", "read_at": card.str("read_at")}
					for _, k := range []string{"requirements", "reagentSlots", "outputItemID", "sourceText"} {
						if v, ok := card[k]; ok {
							row[k] = v
						}
					}
					out["entries"] = []mcpObject{row}
				}
			} else {
				out["match"] = "excluded_by_filter"
			}
		} else if reason != "" {
			out["live_lookup_reason"] = reason
		}
	}
	return out, nil
}

func (q recipeQuery) probeRecipe(dir string, s gameSnapshot) (gameEntry, string) {
	ref := gameRef{Kind: "recipe", Section: "skills", ID: *q.ID}
	rows, err := askGameForDetails(dir, s, []gameRef{ref})
	if err != nil {
		return nil, err.Error()
	}
	if len(rows) != 1 {
		return nil, "No matching live recipe response."
	}
	r := rows[0]
	_, explicit := r["learned"].(bool)
	prof, ok := r["professionID"].(float64)
	if r.str("status") != "ready" || r.id() != *q.ID || !explicit || !ok || prof < 1 || r.str("professionName") == "" {
		return nil, firstOf(r.str("reason"), "The character's current book did not confirm the recipe.")
	}
	if q.ProfessionID != nil && int(prof) != *q.ProfessionID || !strings.Contains(strings.ToLower(r.str("professionName")), q.Profession) {
		return nil, "The open book is a different profession."
	}
	return r, ""
}
