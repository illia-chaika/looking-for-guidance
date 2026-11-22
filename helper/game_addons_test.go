package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const addonContext = "Game: WoW Forever 1.60.1.71037\nInstalled add-ons: available on request\nDetail lookup: supported"

func addonQuestion(t *testing.T, dir, context string) (packet, json.RawMessage) {
	t.Helper()
	p := packet{ContextText: context, Session: "ui", RunID: "app", Character: "Player-1-AC", Login: "login", Text: "Which addons did I install?"}
	prompt, err := gamePrompt(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	tag := snapshotEnvelope.FindStringSubmatch(prompt)
	if len(tag) != 2 {
		t.Fatal(prompt)
	}
	return p, json.RawMessage(`{"snapshot_id":"` + tag[1] + `"}`)
}

func startAddonCall(t *testing.T, dir string, args json.RawMessage) <-chan mcpObject {
	t.Helper()
	out := make(chan mcpObject, 1)
	go func() {
		result, err := queryGameSnapshot(dir, "get_installed_addons", args)
		if err != nil {
			out <- mcpObject{"error": err.Error()}
		} else {
			out <- result
		}
	}()
	return out
}

func nextAddonPage(t *testing.T, dir string, previous string) *detailRequest {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		req := nextDetailRequest(dir, "app", "", time.Now())
		if req != nil && req.ID != previous {
			return req
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("missing add-on page request")
	return nil
}

func addonAnswer(t *testing.T, out <-chan mcpObject) mcpObject {
	t.Helper()
	select {
	case result := <-out:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("add-on call did not finish")
		return nil
	}
}

func TestInstalledAddonPagesAllowTransportRetryWithinScanDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		p, args := addonQuestion(t, dir, addonContext)
		tray := &session{runID: p.RunID}
		start := time.Now()
		tray.pulseDetailBridge(dir, start)
		out := startAddonCall(t, dir, args)

		// Keep the running tray's usual heartbeat while the game sends its frames.
		advance := func(d time.Duration) {
			for end := time.Now().Add(d); time.Now().Before(end); {
				time.Sleep(min(10*time.Second, time.Until(end)))
				tray.pulseDetailBridge(dir, time.Now())
			}
		}
		// A 5,500-byte page at 30 FPS emits a frame every two render ticks.
		// Lose the first copy, wait for the ACK poll and deliver the second one.
		frames := len(encodeStream(make([]byte, 5500), 0))
		retryDelay := 13*time.Second + 2*time.Second + 2*time.Duration(frames)*time.Second/15
		for i := range 2 {
			req := nextAddonPage(t, dir, p.RequestID)
			advance(retryDelay)
			p.RequestID = req.ID
			p.Addons = json.RawMessage(fmt.Sprintf(`{"offset":%d,"total":3,"rows":[{"name":"Addon%d"}]}`, i, i))
			if err := acceptGameDetails(dir, p.RunID, p, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		pending := nextAddonPage(t, dir, p.RequestID)
		if pending.Addons.Offset != 2 {
			t.Fatal("repeat deliveries did not advance both pages", pending)
		}
		// Leave the last page unanswered. It only has the rest of this scan's budget.
		advance(time.Until(start.Add(171 * time.Second)))
		synctest.Wait()
		select {
		case result := <-out:
			if result["status"] != "unavailable" || result["groups"] != nil {
				t.Fatal(result)
			}
		default:
			t.Fatal("an outstanding page extended the 170-second scan deadline")
		}
	})
}

func TestInstalledAddonsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p, args := addonQuestion(t, dir, addonContext)
	tray := &session{runID: p.RunID}
	tray.pulseDetailBridge(dir, time.Now())
	defer closeDetailBridge(dir, p.RunID)
	out := startAddonCall(t, dir, args)
	const count = 520
	scanID, previous := "", ""
	for offset := 0; offset < count; offset += 200 {
		req := nextAddonPage(t, dir, previous)
		if req.Addons == nil || req.Addons.Offset != offset || req.Character != p.Character || req.Login != p.Login || req.Session != p.Session {
			t.Fatal(req)
		}
		if offset == 0 {
			scanID = req.Addons.ID
		}
		if req.Addons.ID != scanID || !snapshotIDPattern.MatchString(scanID) {
			t.Fatal("changed scan between pages", req)
		}
		var rows []mcpObject
		for i := offset; i < min(count, offset+200); i++ {
			name := fmt.Sprintf("Example%03d", i)
			rows = append(rows, mcpObject{"name": name, "title": name, "version": "1.0", "enabled": i%2 == 0,
				"loaded": true, "loadError": false, "loadOnDemand": false, "loadable": true, "security": "INSECURE", "dependenciesKnown": true})
		}
		reply := packet{RequestID: req.ID, RunID: p.RunID, Session: p.Session, Character: p.Character, Login: p.Login, At: float64(1790000000 + offset)}
		reply.Addons, _ = json.Marshal(mcpObject{"offset": offset, "total": count, "rows": rows})
		if offset == 0 {
			for _, foreign := range []packet{
				{RequestID: req.ID, RunID: p.RunID, Session: p.Session, Character: "Player-1-B", Login: p.Login, Addons: reply.Addons},
				{RequestID: req.ID, RunID: p.RunID, Session: p.Session, Character: p.Character, Login: "other", Addons: reply.Addons},
				{RequestID: req.ID, RunID: p.RunID, Session: "other", Character: p.Character, Login: p.Login, Addons: reply.Addons},
			} {
				if err := acceptGameDetails(dir, p.RunID, foreign, time.Now()); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(detailDir(dir), req.ID+".out")); !os.IsNotExist(err) {
					t.Fatal("accepted foreign inventory")
				}
			}
		}
		if err := acceptGameDetails(dir, p.RunID, reply, time.Now()); err != nil {
			t.Fatal(err)
		}
		// An ACK lost in transit repeats an already-completed page.
		if err := acceptGameDetails(dir, p.RunID, reply, time.Now()); err != nil {
			t.Fatal(err)
		}
		previous = req.ID
	}
	result := addonAnswer(t, out)
	if result["status"] != "ok" || result["total"] != count || result["read_at"] != time.Unix(1790000000, 0).UTC().Format(time.RFC3339) {
		t.Fatal(result)
	}
	groups := result["groups"].([]addonStatusGroup)
	if len(groups) != 2 || len(groups[0].Addons)+len(groups[1].Addons) != count ||
		groups[1].Addons[count/2-1].Name != "Example519" || groups[1].Addons[0].Title != "" || *groups[1].Enabled {
		t.Fatal(groups)
	}
}

func TestInstalledAddonsRefusesIncompleteResults(t *testing.T) {
	for _, tc := range []struct{ name, last string }{
		{"stalled", `{"offset":1,"total":2}`}, {"different list", `{"offset":1,"total":0}`},
		{"skipped page", `{"offset":2,"total":2}`}, {"missing total", `{}`},
		{"revoked", `{"error":"Access turned off."}`}, {"duplicate folder", `{"offset":1,"total":2,"rows":[{"name":"FIRST"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p, args := addonQuestion(t, dir, addonContext)
			tray := &session{runID: p.RunID}
			tray.pulseDetailBridge(dir, time.Now())
			defer closeDetailBridge(dir, p.RunID)
			out := startAddonCall(t, dir, args)
			previous := ""
			for _, body := range []string{`{"offset":0,"total":2,"rows":[{"name":"First"}]}`, tc.last} {
				req := nextAddonPage(t, dir, previous)
				p.RequestID, p.Addons = req.ID, json.RawMessage(body)
				if err := acceptGameDetails(dir, p.RunID, p, time.Now()); err != nil {
					t.Fatal(err)
				}
				previous = req.ID
			}
			got := addonAnswer(t, out)
			if got["status"] != "unavailable" || got["groups"] != nil || got["total"] != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestInstalledAddonsAccessAndUnknown(t *testing.T) {
	dir := t.TempDir()
	_, old := addonQuestion(t, dir, "Game: WoW Forever\nDetail lookup: supported")
	_, allowed := addonQuestion(t, dir, addonContext)
	for _, args := range []json.RawMessage{old, allowed} {
		result, err := queryGameSnapshot(dir, "get_installed_addons", args)
		if err != nil || result["status"] != "unavailable" || result["reason"] == "" {
			t.Fatal(result, err)
		}
	}
	if _, err := os.Stat(detailDir(dir)); !os.IsNotExist(err) {
		t.Fatal("queued a scan without permission or connection")
	}
	empty, status, err := groupInstalledAddons(nil)
	if err != nil || status != "ok" || empty == nil || len(empty) != 0 {
		t.Fatal(empty, status, err)
	}
	unknown, status, err := groupInstalledAddons([]addonRow{{addonMetadata: addonMetadata{Name: "Unknown"}, addonStatus: addonStatus{Security: "UNKNOWN"}}})
	body, _ := json.Marshal(unknown)
	if err != nil || status != "partial" || !strings.Contains(string(body), "UNKNOWN") || strings.Contains(string(body), "enabled") {
		t.Fatal(string(body), status, err)
	}
}

func TestInstalledAddonGroupedModuleStatus(t *testing.T) {
	var rows []addonRow
	err := json.Unmarshal([]byte(`[{"name":"looking-for-guidance","enabled":true,"loaded":true,"loadError":false,"loadable":true,"loadOnDemand":false,"security":"INSECURE","dependenciesKnown":true,"groupedModules":100,"bannedModules":2,"unknownSecurityModules":3,"unavailableSecurityModules":4,"loadErrorModules":2,"unknownLoadErrorModules":1}]`), &rows)
	if err != nil {
		t.Fatal(err)
	}
	groups, status, err := groupInstalledAddons(rows)
	body, _ := json.Marshal(groups)
	if err != nil || status != "partial" || len(groups) != 1 || groups[0].Security != "INSECURE" ||
		!strings.Contains(string(body), `"bannedModules":2`) || !strings.Contains(string(body), `"unknownSecurityModules":3`) {
		t.Fatal(status, string(body), err)
	}
	rows[0].UnknownSecurityModules = 0
	groups, status, err = groupInstalledAddons(rows)
	if err != nil || status != "partial" || *groups[0].LoadError || groups[0].Addons[0].UnknownLoadErrorModules != 1 {
		t.Fatal(groups, status, err)
	}
	rows[0].UnknownLoadErrorModules = 0
	groups, status, err = groupInstalledAddons(rows)
	body, _ = json.Marshal(groups)
	if err != nil || status != "ok" || strings.Contains(string(body), "unknownSecurityModules") ||
		!strings.Contains(string(body), `"unavailableSecurityModules":4`) || !strings.Contains(string(body), `"loadErrorModules":2`) {
		t.Fatal(status, string(body), err)
	}
}

func TestInstalledAddonReadings(t *testing.T) {
	complete := addonStatus{Enabled: new(true), Loaded: new(true), Loadable: new(true), LoadOnDemand: new(false),
		LoadError: new(false), DependenciesKnown: true, Security: "NOT_AVAILABLE"}
	var rows []addonRow
	for _, tc := range []struct {
		name       string
		loadError  *bool
		depsKnown  bool
		wantStatus string
	}{
		{"EmptyDependencies", new(false), true, "ok"},
		{"HadLoadError", new(true), true, "ok"},
		{"UnknownLoadError", nil, true, "partial"},
		{"UnknownDependencies", new(false), false, "partial"},
	} {
		row := addonRow{addonMetadata: addonMetadata{Name: tc.name}, addonStatus: complete}
		row.LoadError, row.DependenciesKnown = tc.loadError, tc.depsKnown
		rows = append(rows, row)
		groups, status, err := groupInstalledAddons([]addonRow{row})
		if err != nil || status != tc.wantStatus || groups[0].Security != "NOT_AVAILABLE" {
			t.Errorf("%s: status=%s, groups=%+v, err=%v", tc.name, status, groups, err)
		}
	}
	groups, status, err := groupInstalledAddons(rows)
	if err != nil || status != "partial" || len(groups) != len(rows) {
		t.Fatalf("different client readings were combined: %+v, %s, %v", groups, status, err)
	}
}
