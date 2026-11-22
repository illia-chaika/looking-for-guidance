package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type clientAPITransport func(*http.Request) (*http.Response, error)

func (f clientAPITransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestClientAPISource(t *testing.T) {
	const current = "1111111111111111111111111111111111111111"
	const earlier = "2222222222222222222222222222222222222222"
	const backfill = "3333333333333333333333333333333333333333"
	dir := t.TempDir()
	var calls int
	client := &http.Client{Transport: clientAPITransport(func(req *http.Request) (*http.Response, error) {
		calls++
		url := req.URL
		query := url.Query()
		if url.Scheme != "https" || url.Host != "api.github.com" || url.Path != "/repos/Gethe/wow-ui-source/commits" ||
			req.Method != http.MethodGet || req.Body != nil || len(query) != 3 || query.Get("per_page") != "100" {
			t.Fatalf("unexpected public request: %s %s", req.Method, url)
		}
		header := http.Header{}
		var body string
		switch query.Get("page") {
		case "1":
			if query.Get("sha") != "forever" {
				t.Fatal(url)
			}
			header.Set("Link", `<https://api.github.com/next>; rel="next"`)
			body = fmt.Sprintf(`[{"sha":%q,"commit":{"message":"1.60.1 (71037)"}}]`, current)
		case "2", "3", "4", "5":
			if query.Get("sha") != current {
				t.Fatal(url)
			}
			if query.Get("page") == "5" {
				body = fmt.Sprintf(`[{"sha":%q,"commit":{"message":"1.60.1 (70959)"}}]`, earlier)
			} else {
				header.Set("Link", `<https://api.github.com/next>; rel="next"`)
				body = fmt.Sprintf(`[{"sha":%q,"commit":{"message":"1.60.1 (69876)"}}]`, backfill)
			}
		default:
			t.Fatalf("unexpected page: %s", url)
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for _, test := range []struct{ build, sha string }{{"70959", earlier}, {"71037", current}, {"70000", ""}} {
		s := parseGameSnapshot("Game: WoW Forever 1.60.1."+test.build+" · EU · PvP\nCharacter: level 10 Orc Warrior", 1)
		out := clientAPISource(dir, s, client)
		if out["captured"] != s.Captured || out["build"] != test.build || out["version"] != "1.60.1" {
			t.Fatal(out)
		}
		if test.sha == "" {
			if out["available"] != false || out["reason"] != "build_not_found" || out["commit"] != nil {
				t.Fatal(out)
			}
			continue
		}
		if out["status"] != "ok" || out["available"] != true || out["commit"] != test.sha ||
			out["documentation_index_url"] != "https://raw.githubusercontent.com/Gethe/wow-ui-source/"+test.sha+"/Interface/AddOns/Blizzard_APIDocumentationGenerated/Blizzard_APIDocumentationGenerated.toc" ||
			!strings.Contains(out["documentation_url"].(string), test.sha+"/Interface/AddOns/Blizzard_APIDocumentationGenerated") ||
			!strings.Contains(out["raw_base_url"].(string), test.sha+"/Interface/AddOns/") {
			t.Fatal(out)
		}
		// A later tool call can use the verified match without another GitHub request.
		id, err := saveGameSnapshot(dir, s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := queryGameSnapshot(dir, "get_client_api_source", []byte(fmt.Sprintf(`{"snapshot_id":%q}`, id)))
		if err != nil || got["commit"] != test.sha {
			t.Fatal(got, err)
		}
	}
	if calls != 11 {
		t.Fatalf("commit pages fetched: %d", calls)
	}
}

func TestClientAPISourceUnavailable(t *testing.T) {
	var calls int
	client := &http.Client{Transport: clientAPITransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Status: "403 Forbidden", Body: http.NoBody}, nil
	})}
	dir := t.TempDir()
	for _, game := range []string{"", "WoW Forever 1.60.1", "WoW Classic 1.60.1.71037", "WoW Forever 1.60.1.71037extra"} {
		got := clientAPISource(dir, parseGameSnapshot("Game: "+game, 1), client)
		if got["status"] != "unavailable" || got["reason"] != "build_unavailable" || got["commit"] != nil || calls != 0 {
			t.Fatalf("%q: %+v, calls %d", game, got, calls)
		}
	}
	got := clientAPISource(dir, parseGameSnapshot("Game: WoW Forever 1.60.1.71037", 1), client)
	if got["reason"] != "source_lookup_failed" || got["commit"] != nil || calls != 1 {
		t.Fatal(got)
	}
}

// FOREVER_API_BUILD=1.60.1.71037 checks the actual read-only GitHub lookup using a synthetic snapshot.
func TestLiveClientAPISource(t *testing.T) {
	build := os.Getenv("FOREVER_API_BUILD")
	if build == "" {
		t.Skip("set FOREVER_API_BUILD to a complete Forever version and build")
	}
	dir := t.TempDir()
	id, err := saveGameSnapshot(dir, parseGameSnapshot("Game: WoW Forever "+build, 1))
	if err != nil {
		t.Fatal(err)
	}
	out, err := queryGameSnapshot(dir, "get_client_api_source", []byte(fmt.Sprintf(`{"snapshot_id":%q}`, id)))
	if err != nil || out["status"] != "ok" {
		t.Fatal(out, err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	read := func(address string) string {
		t.Helper()
		resp, err := client.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("read %s: %s, %v", address, resp.Status, err)
		}
		return string(body)
	}
	index, err := url.Parse(out["documentation_index_url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(read(index.String()), "\n") {
		if name := strings.TrimSpace(line); name == "AddOnsDocumentation.lua" {
			file := index.ResolveReference(&url.URL{Path: name})
			if !strings.Contains(read(file.String()), `Name = "GetAddOnSecurity"`) {
				t.Fatalf("missing GetAddOnSecurity in %s", file)
			}
			t.Logf("verified index and GetAddOnSecurity in %s at %s", build, out["commit"])
			return
		}
	}
	t.Fatalf("AddOnsDocumentation.lua is missing from %s", index)
}
