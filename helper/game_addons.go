package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type addonInventoryRequest struct {
	ID     string `json:"id"`
	Offset int    `json:"offset"`
}

type addonMetadata struct {
	Name                       string   `json:"name"`
	Title                      string   `json:"title,omitempty"`
	Version                    string   `json:"version,omitempty"`
	Dependencies               []string `json:"dependencies,omitempty"`
	GroupedModules             int      `json:"groupedModules,omitempty"`
	BannedModules              int      `json:"bannedModules,omitempty"`
	UnknownSecurityModules     int      `json:"unknownSecurityModules,omitempty"`
	UnavailableSecurityModules int      `json:"unavailableSecurityModules,omitempty"`
	LoadErrorModules           int      `json:"loadErrorModules,omitempty"`
	UnknownLoadErrorModules    int      `json:"unknownLoadErrorModules,omitempty"`
}

type addonStatus struct {
	Enabled           *bool  `json:"enabled,omitempty"`
	Loaded            *bool  `json:"loaded,omitempty"`
	LoadError         *bool  `json:"loadError,omitempty"`
	LoadOnDemand      *bool  `json:"loadOnDemand,omitempty"`
	Loadable          *bool  `json:"loadable,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Security          string `json:"security,omitempty"`
	DependenciesKnown bool   `json:"dependenciesKnown"`
}

type addonRow struct {
	addonMetadata
	addonStatus
}

type addonPage struct {
	Offset int        `json:"offset"`
	Total  *int       `json:"total,omitempty"`
	Rows   []addonRow `json:"rows,omitempty"`
	Error  string     `json:"error,omitempty"`
	ReadAt string     `json:"read_at,omitempty"`
}

// An explicit failure also wakes the waiting tool immediately. The caller never
// interprets a missing page or an unavailable client API as an empty inventory.
func acceptAddonPage(base string, data json.RawMessage, at time.Time) error {
	var page addonPage
	if json.Unmarshal(data, &page) != nil {
		page = addonPage{Error: "The game returned an unreadable add-on inventory page."}
	}
	page.ReadAt = at.UTC().Format(time.RFC3339)
	body, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return writeFileAtomic(base+".out", body)
}

type addonStatusGroup struct {
	addonStatus
	Addons []addonMetadata `json:"addons"`
}

func queryInstalledAddons(dir string, raw json.RawMessage) (mcpObject, error) {
	var a struct {
		Snapshot string `json:"snapshot_id"`
	}
	if err := readToolArgs(raw, &a); err != nil {
		return nil, err
	}
	s, err := loadGameSnapshot(dir, a.Snapshot)
	if err != nil {
		return nil, err
	}
	unavailable := func(reason string) mcpObject {
		return mcpObject{"captured": s.Captured, "status": "unavailable", "reason": reason}
	}
	if !s.AddonLookup {
		return unavailable("This question did not grant add-on access. Check Settings → Character data → Add-ons and ask a new question with the current add-on and Companion."), nil
	}
	if !s.Lookup || !validIdentity(s.Character, s.Login) || s.Session == "" || s.RunID == "" {
		return unavailable("This question has no live add-on lookup for the current character. Ask again in the game."), nil
	}

	// Each page fits into one game response and has its own delivery acknowledgement.
	// The single MCP call covers all pages, including time spent waiting for the game's polling slots.
	deadline := time.Now().Add(170 * time.Second)
	var scanID string
	var readAt string
	var rows []addonRow
	total := -1
	for total < 0 || len(rows) < total {
		if err := verifyDetailBridge(dir, s, time.Now()); err != nil {
			return unavailable(err.Error()), nil
		}
		// A 5,500-byte page takes up to 415 wire frames. At 30 FPS, two
		// transmissions take about 56 seconds, plus the initial poll and ACK.
		const pageTimeout = 90 * time.Second
		wait := min(time.Until(deadline), pageTimeout)
		if wait <= 0 {
			return unavailable("The full add-on list did not arrive in time. Ask again while the game is open."), nil
		}
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			return nil, err
		}
		id := hex.EncodeToString(key[:])
		if scanID == "" {
			scanID = id
		}
		req := detailRequest{ID: id, Session: s.Session, Character: s.Character, Login: s.Login,
			Addons: &addonInventoryRequest{ID: scanID, Offset: len(rows)}}
		data, err := askGame(dir, s, req, wait)
		if err != nil {
			return unavailable(err.Error()), nil
		}
		var page addonPage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		if page.Error != "" {
			return unavailable(page.Error), nil
		}
		if page.Total == nil || page.Offset != len(rows) || *page.Total < page.Offset ||
			len(page.Rows) > *page.Total-page.Offset || (len(page.Rows) == 0 && *page.Total != 0) {
			return unavailable("The game returned an incomplete or invalid add-on inventory page."), nil
		}
		if total < 0 {
			total, readAt = *page.Total, page.ReadAt
		} else if *page.Total != total {
			return unavailable("The game returned different add-on inventories during this lookup. Ask again."), nil
		}
		rows = append(rows, page.Rows...)
	}
	groups, status, err := groupInstalledAddons(rows)
	if err != nil {
		return unavailable(err.Error()), nil
	}
	return mcpObject{"captured": s.Captured, "read_at": readAt, "total": total, "groups": groups, "status": status}, nil
}

// Large lists have many identical combinations of client flags. Return each
// combination once, leaving space in the model's context for the actual names.
func groupInstalledAddons(rows []addonRow) ([]addonStatusGroup, string, error) {
	groups := []addonStatusGroup{}
	index := make(map[string]int)
	names := make(map[string]bool, len(rows))
	status := "ok"
	for _, row := range rows {
		folder := strings.ToLower(row.Name)
		if folder == "" || names[folder] {
			return nil, "", errors.New("The game returned missing or duplicate add-on names. Ask again for a complete inventory.")
		}
		names[folder] = true
		if row.Title == row.Name {
			row.Title = ""
		}
		if len(row.Dependencies) > 0 {
			row.DependenciesKnown = true // Older clients sent names only after a successful read.
		}
		if row.Enabled == nil || row.Loaded == nil || row.LoadError == nil || row.Loadable == nil || row.LoadOnDemand == nil ||
			!row.DependenciesKnown || row.Security == "" || row.Security == "UNKNOWN" ||
			row.UnknownSecurityModules > 0 || row.UnknownLoadErrorModules > 0 {
			status = "partial"
		}
		key, err := json.Marshal(row.addonStatus)
		if err != nil {
			return nil, "", err
		}
		i, found := index[string(key)]
		if !found {
			i = len(groups)
			index[string(key)] = i
			groups = append(groups, addonStatusGroup{addonStatus: row.addonStatus})
		}
		groups[i].Addons = append(groups[i].Addons, row.addonMetadata)
	}
	return groups, status, nil
}
