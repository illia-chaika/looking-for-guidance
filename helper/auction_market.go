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
// the first is PvP and the second Normal (matched against prices scanned in the game, 2026-11-13); the third is
// empty, and which of the last two is Roleplay and which Roleplay PvP is not known yet.
var factionHouses = map[string][5]int{
	"Alliance": {110, 111, 112, 119, 123},
	"Horde":    {113, 114, 115, 120, 124},
}

var rulesetGroups = map[string]int{"PvP": 0, "PvE": 1}

// The ruleset ids in the API's paths. The houses answer the same under every id; the player's own is used anyway.
var rulesetIDs = map[string]int{"PvP": 1, "PvE": 2, "RP": 3, "RP-PvP": 15}

var marketRegions = map[string]bool{"US": true, "EU": true, "KR": true, "TW": true}

func (f marketFacts) market() (auctionMarket, error) {
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
	houses, ok := factionHouses[f.Faction]
	if !ok {
		return auctionMarket{}, fmt.Errorf("no faction auction house for %q", f.Faction)
	}
	group, ok := rulesetGroups[f.Ruleset]
	if !ok {
		return auctionMarket{}, fmt.Errorf("which auction house serves the %s ruleset is not known yet", f.Ruleset)
	}
	return auctionMarket{Region: strings.ToLower(f.Region), Ruleset: f.Ruleset, Faction: f.Faction, House: houses[group]}, nil
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
