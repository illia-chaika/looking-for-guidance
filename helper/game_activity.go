package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Deliberately exclude raw tool arguments/results, snapshot keys and model reasoning
// from the HUD. Only display a whitelist of useful, observed call information.
type turnEvent struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
	Status string `json:"status"`
}

var sectionRU = map[string]string{
	"character": "Персонаж", "location": "Местность", "game": "Версия игры", "quests": "Задания",
	"gear": "Экипировка", "bags": "Сумки", "talents": "Таланты", "skills": "Навыки", "spells": "Заклинания",
	"buffs": "Эффекты", "target": "Цель", "reputation": "Репутация", "references": "Вложения", "links": "Вложения",
}

type activityRef struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

type activityArgs struct {
	Sections []string      `json:"sections"`
	Section  string        `json:"section"`
	Scope    string        `json:"scope"`
	Quest    int           `json:"quest_id"`
	ID       int           `json:"id"`
	Query    string        `json:"query"`
	Refs     []activityRef `json:"refs"`
	View     string        `json:"view"`
	Kind     string        `json:"kind"`
	Known    *bool         `json:"known"`
	Pet      *bool         `json:"pet"`
	Slot     *int          `json:"slot"`
	Limit    *int          `json:"limit"`
	Offset   int           `json:"offset"`
}

func (a activityArgs) section(tool string) string {
	if tool == "get_entries" {
		return a.Section
	}
	return taskEntrySection(tool)
}

func (a activityArgs) label(tool string) string {
	if label := sectionRU[a.section(tool)]; label != "" {
		return label
	}
	switch tool {
	case "get_recipes":
		return "Рецепты"
	case "get_auction_prices":
		return "Аукцион"
	case "find_auction_upgrades":
		return "Подбор на аукционе"
	case "search_game_data":
		return "Поиск в базе мира"
	case "get_game_data":
		return "База мира"
	case "get_changes":
		return "Изменения в игре"
	case "get_memory":
		return "Память персонажа"
	case "upsert_memory":
		return "Сохранение заметки"
	case "forget_memory":
		return "Удаление заметки"
	case "get_context":
		if len(a.Sections) == 1 && sectionRU[a.Sections[0]] != "" {
			return sectionRU[a.Sections[0]]
		}
		return "Данные персонажа"
	case "get_entries":
		return "Игровой справочник"
	case "get_quests":
		return "Журнал заданий"
	case "get_details":
		var items, spells bool
		for _, ref := range a.Refs {
			items = items || ref.Kind == "item"
			spells = spells || ref.Kind == "spell"
		}
		switch {
		case items && spells:
			return "Карточки предметов и заклинаний"
		case items:
			return "Карточки предметов"
		case spells:
			return "Карточки заклинаний"
		}
		return "Подробности из игры"
	}
	return ""
}

func safeWebURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(s) > 2048 || strings.ContainsAny(s, "|\\<>\" \r\n\t") {
		return ""
	}
	return s
}
func tidyEventText(s string) string { return trimText(strings.Join(strings.Fields(s), " "), 240) }

// Read only display metadata. Raw arguments, descriptions and snapshot keys
// never reach the activity rows; the same result drives the source and status.
type activityRow struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type activityResult struct {
	Status      string        `json:"status"`
	Books       []activityRow `json:"books"`
	View        string        `json:"view"`
	Captured    string        `json:"captured"`
	Unavailable []string      `json:"unavailable"`
	Cards       []activityRow `json:"cards"`
	Entries     []activityRow `json:"entries"`
	Quests      []activityRow `json:"quests"`
	Total       *int          `json:"total"`
}

func gameOutcome(it cxItem) (r activityResult, status, note string) {
	if it.Result == nil || it.Result.IsError || it.Error != nil || it.Status != "completed" {
		return
	}
	for _, part := range it.Result.Content {
		if json.Unmarshal([]byte(part.Text), &r) == nil && r.Captured != "" {
			break
		}
		r = activityResult{}
	}
	if r.Captured == "" {
		return r, "failed", ""
	}
	if it.Tool == "get_recipes" {
		switch r.Status {
		case "ok", "partial", "unavailable":
			return r, r.Status, ""
		default:
			return r, "failed", ""
		}
	}
	if len(r.Unavailable) > 0 {
		var names []string
		for _, s := range r.Unavailable {
			if label := sectionRU[s]; label != "" {
				names = append(names, strings.ToLower(label))
			}
		}
		return r, "partial", "Недоступно: " + strings.Join(names, ", ")
	}
	if it.Tool == "get_details" && len(r.Cards) > 0 {
		ready, partial := 0, 0
		for _, c := range r.Cards {
			switch c.Status {
			case "ready":
				ready++
			case "partial":
				partial++
			}
		}
		if ready+partial == 0 {
			return r, "unavailable", "Клиент не вернул подробности"
		}
		if ready < len(r.Cards) {
			return r, "partial", fmt.Sprintf("Полных карточек: %d из %d", ready, len(r.Cards))
		}
	}
	return r, "ok", ""
}

func rowName(rows []activityRow, id int, kind string) string {
	for _, r := range rows {
		if r.ID == id && (kind == "" || r.Kind == kind) {
			if name := firstOf(r.Name, r.Title); name != "" {
				return tidyEventText(name)
			}
		}
	}
	return fmt.Sprintf("#%d", id)
}

var equipmentSlotsRU = map[int]string{
	1: "голова", 2: "шея", 3: "плечи", 4: "рубашка", 5: "грудь", 6: "пояс", 7: "ноги",
	8: "ступни", 9: "запястья", 10: "кисти рук", 11: "первое кольцо", 12: "второе кольцо",
	13: "первый аксессуар", 14: "второй аксессуар", 15: "спина", 16: "правая рука", 17: "левая рука",
	18: "оружие дальнего боя", 19: "гербовая накидка",
}

func (a activityArgs) details(tool string, r activityResult) []string {
	var bits []string
	switch tool {
	case "get_recipes":
		for _, book := range r.Books {
			if book.Name != "" {
				bits = append(bits, tidyEventText(book.Name))
			}
		}
		if value := a.Query; value != "" {
			bits = append(bits, tidyEventText(value))
		} else if len(r.Entries) == 1 && r.Entries[0].Name != "" {
			bits = append(bits, tidyEventText(r.Entries[0].Name))
		} else if a.ID > 0 {
			bits = append(bits, fmt.Sprintf("#%d", a.ID))
		}
	case "get_context":
		if len(a.Sections) > 1 {
			for _, s := range a.Sections {
				if label := sectionRU[s]; label != "" {
					bits = append(bits, strings.ToLower(label))
				}
			}
		}
	case "get_quests":
		switch {
		case a.Quest > 0:
			bits = append(bits, rowName(r.Quests, a.Quest, ""))
		case a.Scope == "selected":
			bits = append(bits, "выбранное задание")
		case a.Scope == "tracked":
			bits = append(bits, "отслеживаемые задания")
		default:
			bits = append(bits, "список заданий")
		}
	case "get_details":
		for _, ref := range a.Refs {
			if ref.ID > 0 && (ref.Kind == "item" || ref.Kind == "spell") {
				bits = append(bits, rowName(r.Cards, ref.ID, ref.Kind))
			}
		}
	default:
		if a.section(tool) == "" {
			break
		}
		if label := map[string]string{"summary": "обзор дерева", "invested": "изученные таланты", "purchasable": "доступные для изучения", "all": "всё дерево"}[firstOf(r.View, a.View)]; label != "" {
			bits = append(bits, label)
		}
		if a.Known != nil {
			if *a.Known {
				bits = append(bits, "изученные")
			} else {
				bits = append(bits, "неизученные")
			}
		}
		if a.Pet != nil {
			if *a.Pet {
				bits = append(bits, "питомец")
			} else {
				bits = append(bits, "персонаж")
			}
		}
		if label := map[string]string{"item": "предметы", "spell": "заклинания", "quest": "задания"}[a.Kind]; label != "" {
			bits = append(bits, label)
		}
		if a.Slot != nil {
			slot := fmt.Sprintf("слот %d", *a.Slot)
			if a.section(tool) == "gear" && equipmentSlotsRU[*a.Slot] != "" {
				slot = equipmentSlotsRU[*a.Slot]
			}
			bits = append(bits, slot)
		}
		if a.Query != "" {
			bits = append(bits, "«"+tidyEventText(a.Query)+"»")
		} else if a.ID > 0 {
			bits = append(bits, rowName(r.Entries, a.ID, ""))
		}
		if r.Total != nil && r.Entries != nil {
			switch {
			case len(r.Entries) == 0:
				bits = append(bits, "нет совпадений на этой странице")
			case a.Offset > 0 || len(r.Entries) < *r.Total:
				bits = append(bits, fmt.Sprintf("записи %d–%d из %d", a.Offset+1, a.Offset+len(r.Entries), *r.Total))
			default:
				bits = append(bits, fmt.Sprintf("найдено: %d", *r.Total))
			}
		} else if r.Captured == "" {
			if a.Offset > 0 {
				bits = append(bits, fmt.Sprintf("смещение %d", a.Offset))
			}
			if a.Limit != nil {
				bits = append(bits, fmt.Sprintf("лимит %d", *a.Limit))
			}
		}
	}
	return bits
}

func activitySummary(events []turnEvent, tools, web int) string {
	labels := []string{}
	for _, e := range events {
		if e.Kind == "mcp" && !slices.Contains(labels, e.Label) {
			labels = append(labels, e.Label)
		}
	}
	if len(labels) > 1 {
		labels = slices.DeleteFunc(labels, func(s string) bool { return s == "Данные персонажа" })
	}
	if len(labels) > 3 {
		labels = append(labels[:3], fmt.Sprintf("ещё %d", len(labels)-3))
	}
	if len(labels) == 0 && tools > 0 {
		labels = append(labels, fmt.Sprintf("Инструменты: %d", tools))
	}
	if web > 0 {
		labels = append(labels, fmt.Sprintf("Интернет: %d", web))
	}
	if n := tools + web - len(events); n > 0 {
		labels = append(labels, fmt.Sprintf("скрыто: %d", n))
	}
	return strings.Join(labels, " · ")
}

func gameActivityEvent(it cxItem) (e turnEvent, ok bool) {
	switch it.Type {
	case "webSearch":
		e = turnEvent{Kind: "web", Label: "Поиск в интернете", Status: "unknown"}
		if it.StartedOnly || it.Status == "inProgress" {
			e.Status = "running"
		} else if it.Error != nil || it.Status == "failed" || (it.Result != nil && it.Result.IsError) {
			e.Status = "failed"
		} else if (it.Status == "" || it.Status == "completed") && it.WebResults {
			e.Status = "ok"
		}
		switch a := it.Action; a.Type {
		case "openPage", "findInPage":
			e.Label = "Открытие страницы"
			if a.Type == "findInPage" {
				e.Label = "Поиск на странице"
			}
			e.URL = safeWebURL(a.URL)
			var host string
			if u, err := url.Parse(e.URL); err == nil {
				host = u.Hostname()
			}
			e.Detail = strings.Trim(strings.Join([]string{host, a.Pattern}, " · "), " ·")
		case "search", "":
			e.Detail = firstOf(strings.Join(a.Queries, "; "), a.Query, it.Query)
		default:
			e.Label = "Обращение к интернету"
			e.Detail = it.Query
		}
	case "mcpToolCall", "dynamicToolCall":
		e = turnEvent{Kind: "mcp", Label: toolLine(it.Server, it.Tool), Status: "unknown"}
		if it.Type == "dynamicToolCall" {
			e.Kind = "other"
			e.Label = it.Tool
		}
		switch it.Status {
		case "inProgress":
			e.Status = "running"
		case "completed":
			e.Status = "ok"
		case "failed":
			e.Status = "failed"
		}
		if it.StartedOnly {
			e.Status = "running"
		} else if it.Error != nil || (it.Result != nil && it.Result.IsError) {
			e.Status = "failed"
		}
		if it.Server == "wow_game" {
			// Codex versions differ on retaining isError. A successful wow_game result
			// always has a capture stamp. Useful error text can be shown without raw JSON.
			var args activityArgs
			_ = json.Unmarshal(it.Arguments, &args)
			if label := args.label(it.Tool); label != "" {
				e.Label = label
			}
			result, status, note := gameOutcome(it)
			bits := args.details(it.Tool, result)
			if status != "" {
				e.Status = status
			}
			if note != "" {
				bits = append(bits, note)
			}
			if e.Status == "failed" {
				if it.Error != nil && it.Error.Message != "" {
					bits = append(bits, it.Error.Message)
				} else if it.Result != nil && len(it.Result.Content) > 0 {
					msg := it.Result.Content[0].Text
					if !json.Valid([]byte(msg)) {
						if args.Limit != nil && (*args.Limit < 1 || *args.Limit > maxEntryLimit) && (msg == "invalid section, entry filter or page bounds" || strings.HasPrefix(msg, "limit ")) {
							msg = fmt.Sprintf("допустимо от 1 до %d записей", maxEntryLimit)
						}
						bits = append(bits, msg)
					}
				}
			}
			e.Detail = strings.Join(bits, " · ")
		}
	default:
		return turnEvent{}, false
	}
	e.Label = tidyEventText(e.Label)
	e.Detail = tidyEventText(e.Detail)
	return e, true
}
