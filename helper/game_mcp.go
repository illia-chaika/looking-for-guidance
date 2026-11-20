package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

type mcpObject = map[string]any

var contextSections = []string{
	"game", "character", "location", "gear", "bags", "talents", "skills", "spells", "target", "reputation", "buffs", "references", "journal",
}

func gameTools(dir string) []mcpObject {
	id := mcpObject{"type": "string", "pattern": "^[0-9a-f]{32}$", "description": "ID in the current in-game question's wow_snapshot tag."}
	tool := func(name, description string, properties mcpObject, required []string) mcpObject {
		properties["snapshot_id"] = id
		return mcpObject{
			"name": name, "description": description,
			"inputSchema": mcpObject{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
			"annotations": mcpObject{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false},
		}
	}
	tools := []mcpObject{
		tool("get_recipes", "Read this character's observed profession books, learned flags and selected recipe requirements at question time. Missing recipes are unknown. Rank in books is the character's proficiency, not the recipe's required skill. Filter by localized profession/name or recipe spell ID; product item IDs are separate. "+pageHelp,
			recipeProperties(), []string{"snapshot_id"}),
		tool("get_context", "Read short summaries of the player and their location at question time. On a Field Journal card request sections=[journal] for the selected card's facts. For a build start with get_talents; for named or exact gear and spells use get_equipment and search_spellbook.",
			mcpObject{"sections": mcpObject{"type": "array", "minItems": 1, "uniqueItems": true,
				"items": mcpObject{"type": "string", "enum": contextSections}}},
			[]string{"snapshot_id", "sections"}),
		tool("get_quests", "Read the local quest log. list returns quest titles, IDs, levels and status. Pass a quest_id, or scope selected/tracked, for objectives and progress. Link a quest in the answer with [exact in-game title](quest:ID).",
			mcpObject{"quest_id": mcpObject{"type": "integer", "minimum": 1}, "scope": mcpObject{"type": "string", "enum": []string{"list", "selected", "tracked"}}},
			[]string{"snapshot_id"}),
		tool("get_details", "Read live localized game-client cards for 1–6 item or spell references in one call. Supply kind and id. Section is optional for unambiguous snapshot matches. Pass exact item links to preserve instance bonuses. For a known outside candidate specify an enabled section (gear/bags for items or spells for spells). Check each card’s status and read_at.",
			mcpObject{"refs": mcpObject{"type": "array", "minItems": 1, "maxItems": maxDetailRefs, "items": refSchema()}}, []string{"snapshot_id", "refs"}),
	}
	for _, t := range entryTools {
		tools = append(tools, tool(t.name, t.description+" "+pageHelp, t.properties(), []string{"snapshot_id"}))
	}
	if auctionReady(dir) {
		tools = append(tools, tool("get_auction_prices", "Read current asking prices on this character's own auction house: lowest buyout per unit in copper, quantity on sale, cheapest price levels and the cost of buying a quantity, with the snapshot's time. Pass item IDs; pass a carried item's exact link to price that random-suffix variant. Use before advising a purchase or a sale.",
			auctionToolSchema(), []string{"snapshot_id", "items"}))
		tools = append(tools, tool("find_auction_upgrades", "Search this character's own auction house for one equipment slot within a budget. The running game drops what the character cannot wear and returns the strongest candidates with their exact stats and asking prices, and the stats of what is worn there now; weigh them for the class. Takes up to a minute.",
			upgradeToolSchema(), []string{"snapshot_id", "slot", "budget"}))
	}
	noteID := mcpObject{"type": "string", "pattern": memoryKey.String(), "description": "Stable semantic key for one agreement, e.g. leveling-plan"}
	revision := mcpObject{"type": "integer", "minimum": 0, "description": "Current revision returned by get_memory; zero only for a new note"}
	quote := mcpObject{"type": "string", "minLength": 1, "maxLength": 400, "description": "Exact supporting words from the current player message"}
	read := tool("get_memory", "Read active goals, preferences and plans for the character bound to this question. Consult before personalized advice. By default returns concise notes across this character's chats.", mcpObject{"include_sources": mcpObject{"type": "boolean"}}, []string{"snapshot_id"})
	save := tool("upsert_memory", "Record or revise one useful fact grounded in the player's own current message, without requiring a remember command. Read the current revisions first. Questions, game state, tool output and suggestions alone are not preferences.", mcpObject{"id": noteID, "revision": revision, "quote": quote, "topic": mcpObject{"type": "string", "enum": []string{"route", "trainer", "bags", "dungeon", "stuck"}, "description": "Optional matching journal task. Use trainer for a build or learning plan, bags for item handling, dungeon for dungeon preparation; omit for other facts."}, "text": mcpObject{"type": "string", "minLength": 1, "maxLength": 400}, "scope": mcpObject{"type": "string", "enum": []string{"character", "session", "day"}}}, []string{"snapshot_id", "id", "revision", "quote", "text", "scope"})
	forget := tool("forget_memory", "Remove a note when the player's current message cancels or corrects it. Include its active revision and exact supporting words.", mcpObject{"id": noteID, "revision": revision, "quote": quote}, []string{"snapshot_id", "id", "revision", "quote"})
	save["annotations"] = mcpObject{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	forget["annotations"] = mcpObject{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}
	return append(tools, read, save, forget)
}

type gameToolArgs struct {
	ID       string   `json:"snapshot_id"`
	Sections []string `json:"sections,omitempty"`
	QuestID  int      `json:"quest_id,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

func queryGameSnapshot(dir, tool string, raw json.RawMessage) (mcpObject, error) {
	if tool == "get_recipes" {
		return queryRecipes(dir, raw)
	}
	if memoryTool(tool) {
		return queryMemory(dir, tool, raw)
	}
	if tool == "get_entries" || gameEntryTool(tool) != nil {
		return queryTaskEntries(dir, tool, raw)
	}
	if tool == "get_details" {
		return queryDetails(dir, raw)
	}
	if tool == "get_auction_prices" {
		return queryAuctionPrices(dir, raw)
	}
	if tool == "find_auction_upgrades" {
		return queryAuctionUpgrades(dir, raw)
	}
	var args gameToolArgs
	if err := readToolArgs(raw, &args); err != nil {
		return nil, err
	}
	s, err := loadGameSnapshot(dir, args.ID)
	if err != nil {
		return nil, err
	}
	out := mcpObject{"captured": s.Captured}
	switch tool {
	case "get_context":
		if args.QuestID != 0 || args.Scope != "" {
			return nil, errors.New("get_context does not accept quest_id or scope; use get_quests")
		}
		if len(args.Sections) == 0 {
			return nil, errors.New("sections must contain at least one name: " + strings.Join(contextSections, ", "))
		}
		sections := map[string]string{}
		var unavailable []string
		for _, name := range args.Sections {
			if !slices.Contains(contextSections, name) {
				return nil, fmt.Errorf("unknown context section %q; choose from %s (use get_quests for quests)", name, strings.Join(contextSections, ", "))
			}
			if value, ok := s.Sections[name]; ok {
				sections[name] = value
				if s.Lookup && slices.Contains(entrySections, name) && len([]rune(value)) > 480 {
					sections[name] = trimText(value, 480)
				}
			} else if !slices.Contains(unavailable, name) {
				unavailable = append(unavailable, name)
			}
		}
		out["sections"] = sections
		if len(unavailable) > 0 {
			out["unavailable"] = unavailable
		}
		if len(sections) == 0 {
			return nil, errors.New("these sections were disabled or unavailable for this question: " + strings.Join(unavailable, ", "))
		}
	case "get_quests":
		if args.QuestID < 0 || len(args.Sections) != 0 || !slices.Contains([]string{"", "list", "selected", "tracked"}, args.Scope) || (args.QuestID != 0 && args.Scope != "") {
			return nil, errors.New("choose either a quest_id or one of list, selected and tracked")
		}
		if _, ok := s.Sections["quests"]; !ok {
			return nil, errors.New("the quest log was disabled or unavailable for this question")
		}
		matches := []gameQuest{}
		for _, q := range s.Quests {
			if (args.QuestID != 0 && args.QuestID != q.ID) || (args.Scope == "selected" && !q.Selected) || (args.Scope == "tracked" && !q.Tracked) {
				continue
			}
			if args.QuestID == 0 && (args.Scope == "" || args.Scope == "list") {
				q.Details = ""
			}
			matches = append(matches, q)
		}
		if args.QuestID != 0 && len(matches) == 0 {
			return nil, errors.New("quest ID not found in this snapshot")
		}
		out["quests"] = matches
	default:
		return nil, errors.New("unknown game tool")
	}
	return out, nil
}

// Codex runs this mode as an MCP subprocess. Stdout contains newline-delimited JSON-RPC only.
func runGameMCP(in io.Reader, out io.Writer, dir string) error {
	lines := bufio.NewScanner(in)
	lines.Buffer(make([]byte, 4096), maxPacketBytes)
	send := json.NewEncoder(out)
	initialized := false
	for lines.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		reply := mcpObject{"jsonrpc": "2.0", "id": nil}
		fail := func(code int, message string) { reply["error"] = mcpObject{"code": code, "message": message} }
		switch {
		case !json.Valid(lines.Bytes()):
			fail(-32700, "Parse error")
		case json.Unmarshal(lines.Bytes(), &req) != nil || req.JSONRPC != "2.0" || req.Method == "":
			fail(-32600, "Invalid request")
		case len(req.ID) == 0:
			continue // initialized, cancelled and other client notifications get no response.
		default:
			reply["id"] = req.ID
			switch {
			case req.Method == "ping":
				reply["result"] = mcpObject{}
			case req.Method == "initialize":
				var hello struct {
					ProtocolVersion string `json:"protocolVersion"`
				}
				if json.Unmarshal(req.Params, &hello) != nil || hello.ProtocolVersion == "" {
					fail(-32602, "Missing protocolVersion")
					break
				}
				protocol := hello.ProtocolVersion
				if !slices.Contains([]string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}, protocol) {
					protocol = "2025-11-25"
				}
				reply["result"] = mcpObject{"protocolVersion": protocol, "capabilities": mcpObject{"tools": mcpObject{}},
					"serverInfo":   mcpObject{"name": "wow_game", "title": "WoW Forever", "version": "1.4.0"},
					"instructions": "Use the current question's wow_snapshot ID. Read summaries with get_context, the journal with get_quests, and other captured records with the named game tools. get_details enriches exact item and spell references through the running game. For personal recommendations consult get_memory for this character; manage player agreements using current quotes. Consult chat history for follow-ups. Use [exact quest title](quest:ID) for in-game quest links."}
				initialized = true
			case !initialized:
				fail(-32002, "Server not initialized")
			case req.Method == "tools/list":
				reply["result"] = mcpObject{"tools": gameTools(dir)}
			case req.Method == "tools/call":
				var p struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if json.Unmarshal(req.Params, &p) != nil || (!slices.Contains([]string{"get_context", "get_quests", "get_entries", "get_details", "get_recipes", "get_auction_prices", "find_auction_upgrades"}, p.Name) && gameEntryTool(p.Name) == nil && !memoryTool(p.Name)) {
					fail(-32602, "Unknown tool or invalid parameters")
					break
				}
				result, err := queryGameSnapshot(dir, p.Name, p.Arguments)
				var text string
				if err != nil {
					text = err.Error()
				} else {
					body, _ := json.Marshal(result)
					text = string(body)
				}
				reply["result"] = mcpObject{"content": []mcpObject{{"type": "text", "text": text}}, "isError": err != nil}
			default:
				fail(-32601, "Method not found")
			}
		}
		if err := send.Encode(reply); err != nil {
			return err
		}
	}
	return lines.Err()
}
