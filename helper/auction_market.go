package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// auctionMarket is one auction house of WoW: Forever. Blizzard keeps one per region, ruleset and faction and serves
// each through its Game Data API as a numbered house.
type auctionMarket struct {
	Region  string `json:"region"`  // eu, us, kr, tw
	Ruleset string `json:"ruleset"` // PvE, PvP, RP or RP-PvP
	Faction string `json:"faction"` // Alliance or Horde
	House   int    `json:"house"`
}

// marketFacts is what the add-on reports: GetCurrentRegion, the ruleset game rules and UnitFactionGroup.
type marketFacts struct {
	Region   string `json:"region,omitempty"`
	Ruleset  string `json:"ruleset,omitempty"`
	Faction  string `json:"faction,omitempty"`
	Hardcore bool   `json:"hardcore,omitempty"`
}

// Blizzard lists five houses per faction. The n-th houses of the two factions and the neutral one form a market:
// the second is Normal (matched against prices players scanned in the game, 2026-11-13) and the first PvP (by
// elimination: Korea has only those two rulesets and only those two groups hold data); the third is empty, and
// which of the last two is Roleplay and which Roleplay PvP is not known yet.
var factionHouses = map[string][5]int{
	"Alliance": {110, 111, 112, 119, 123},
	"Horde":    {113, 114, 115, 120, 124},
}

// anyHouses adds the neutral Blackwater houses, which both factions reach. Only the auction tools' market argument
// names them: a character's own market is its faction's.
var anyHouses = map[string][5]int{
	"Alliance": factionHouses["Alliance"],
	"Horde":    factionHouses["Horde"],
	"Neutral":  {116, 117, 118, 121, 125},
}

var rulesetGroups = map[string]int{"PvP": 0, "PvE": 1}

// The ruleset ids in the API's paths. The houses answer the same under every id; the player's own is used anyway.
var rulesetIDs = map[string]int{"PvP": 1, "PvE": 2, "RP": 3, "RP-PvP": 15}

var marketRegions = map[string]bool{"US": true, "EU": true, "KR": true, "TW": true}

func (f marketFacts) market() (auctionMarket, error) { return f.marketIn(factionHouses) }

func (f marketFacts) marketIn(byFaction map[string][5]int) (auctionMarket, error) {
	switch {
	case f.Hardcore:
		return auctionMarket{}, errors.New("hardcore characters trade on a market the API does not list")
	case f.Region == "":
		return auctionMarket{}, errors.New("the game did not report its region")
	case !marketRegions[f.Region]:
		return auctionMarket{}, fmt.Errorf("the %s region has no auction data in the API this app reads", f.Region)
	case f.Ruleset == "":
		return auctionMarket{}, errors.New("the game did not report its ruleset")
	case rulesetIDs[f.Ruleset] == 0:
		return auctionMarket{}, fmt.Errorf("unknown ruleset %q", f.Ruleset)
	}
	houses, ok := byFaction[f.Faction]
	if !ok {
		return auctionMarket{}, fmt.Errorf("no faction auction house for %q", f.Faction)
	}
	group, ok := rulesetGroups[f.Ruleset]
	if !ok {
		return auctionMarket{}, fmt.Errorf("which auction house serves the %s ruleset is not known yet", f.Ruleset)
	}
	return auctionMarket{Region: strings.ToLower(f.Region), Ruleset: f.Ruleset, Faction: f.Faction, House: houses[group]}, nil
}

// marketArg is the auction tools' optional market: the fields it leaves out are the character's own.
type marketArg struct {
	Region  string `json:"region,omitempty"`
	Ruleset string `json:"ruleset,omitempty"`
	Faction string `json:"faction,omitempty"`
}

func marketArgSchema() mcpObject {
	return mcpObject{"type": "object", "additionalProperties": false,
		"description": "Optional: another auction house. Fields left out are the character's own; Neutral is the Blackwater house both factions reach. Prices of a house the character does not use are downloaded on demand, which can take up to a minute.",
		"properties": mcpObject{
			"region":  mcpObject{"type": "string", "enum": []string{"US", "EU", "KR", "TW"}},
			"ruleset": mcpObject{"type": "string", "enum": []string{"PvE", "PvP", "RP", "RP-PvP"}},
			"faction": mcpObject{"type": "string", "enum": []string{"Alliance", "Horde", "Neutral"}},
		}}
}

// apply turns the argument into a market, filling what it leaves out from the character's own market (own, or why
// there is none).
func (a marketArg) apply(own auctionMarket, ownErr error) (auctionMarket, error) {
	f := marketFacts{Region: strings.ToUpper(own.Region), Ruleset: own.Ruleset, Faction: own.Faction}
	if ownErr != nil {
		if a.Region == "" || a.Ruleset == "" || a.Faction == "" {
			return auctionMarket{}, ownErr
		}
		f = marketFacts{}
	}
	if a.Region != "" {
		f.Region = strings.ToUpper(a.Region)
	}
	if a.Ruleset != "" {
		f.Ruleset = a.Ruleset
	}
	if a.Faction != "" {
		f.Faction = a.Faction
	}
	return f.marketIn(anyHouses)
}

func (m auctionMarket) key() string { return fmt.Sprintf("%s-%d", m.Region, m.House) }

func (m auctionMarket) label() string {
	name := map[string]string{"PvE": "Normal", "PvP": "PvP", "RP": "Roleplay", "RP-PvP": "Roleplay PvP"}[m.Ruleset]
	return fmt.Sprintf("%s %s %s (house %d)", strings.ToUpper(m.Region), name, m.Faction, m.House)
}

var characterFaction = regexp.MustCompile(`, (Alliance|Horde|Neutral)(?: ·|$)`)

// snapshotMarket reads the market from a question's Game and Character lines, as Context.lua writes them:
// "WoW Forever 1.60.1.70938 · EU · PvE" and "level 15 Orc Warrior, Horde · …".
func snapshotMarket(s gameSnapshot) (auctionMarket, error) {
	game, ok := s.Sections["game"]
	if !ok {
		return auctionMarket{}, errors.New("this question carries no game data")
	}
	var f marketFacts
	for _, token := range strings.Split(game, " · ")[1:] {
		switch {
		case token == "hardcore":
			f.Hardcore = true
		case rulesetIDs[token] != 0:
			f.Ruleset = token
		case len(token) == 2 && strings.ToUpper(token) == token:
			f.Region = token
		}
	}
	if m := characterFaction.FindStringSubmatch(s.Sections["character"]); m != nil {
		f.Faction = m[1]
	} else {
		return auctionMarket{}, errors.New("the character section with the faction was not shared for this question")
	}
	return f.market()
}
