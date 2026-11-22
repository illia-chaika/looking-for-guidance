package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveWorld downloads the world dataset from the project's server, with the Battle.net sign-in key stored by the
// menu's Sign in with Battle.net, and reads it through the adviser's tools:
//
//	FOREVER_WORLD=1 go test -run TestLiveWorld -v ./helper
func TestLiveWorld(t *testing.T) {
	if os.Getenv("FOREVER_WORLD") == "" {
		t.Skip("set FOREVER_WORLD=1 to download the real dataset")
	}
	if backendToken() == "" {
		t.Fatal("choose Sign in with Battle.net in the companion's menu first: the server needs its key")
	}
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	w := &worldWatcher{dir: worldDir(snapshots), http: &http.Client{Timeout: 5 * time.Minute},
		base: func() string { return defaultWorldMirror }, now: time.Now}
	start := time.Now()
	if err := w.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, err := readWorldManifest(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	var size int64
	records := map[string]int{}
	for _, f := range m.Files {
		size += f.Bytes
		records[f.Kind] += f.Records
	}
	t.Logf("dataset built %s, Battle.net data from %q: %d files, %d KB, downloaded in %s; records %v",
		m.Built, m.Sources.Battlenet.Oldest, len(m.Files), size>>10, time.Since(start).Round(time.Millisecond), records)

	start = time.Now()
	for _, query := range []string{"Kobold Camp Cleanup", "Marshal McBride", "Слабое лечебное зелье", "Fireball", "Shadowfang Keep"} {
		if query == "Слабое лечебное зелье" && m.Sources.Battlenet.Oldest == "" {
			t.Log("no Battle.net data yet, so no names in other languages")
			continue
		}
		out, err := queryWorldSearch(snapshots, []byte(fmt.Sprintf(`{"query":%q,"max_results":3}`, query)))
		if err != nil {
			t.Fatal(err)
		}
		results := out["results"].([]mcpObject)
		if len(results) == 0 {
			t.Fatalf("nothing found for %q", query)
		}
		args, _ := json.Marshal(map[string]any{"refs": []any{map[string]any{"kind": results[0]["kind"], "id": results[0]["id"]}}})
		rec, err := queryWorldGet(snapshots, args)
		if err != nil {
			t.Fatal(err)
		}
		records := rec["records"].([]mcpObject)
		if len(records) != 1 {
			t.Fatalf("%q: %v", query, rec)
		}
		raw, _ := json.Marshal(records[0]["record"])
		body := string(raw)
		t.Logf("%q → %v; %s…", query, results, body[:min(len(body), 300)])
		if query == "Kobold Camp Cleanup" {
			t.Logf("first search with loading took %s", time.Since(start).Round(time.Millisecond))
		}
	}
}
