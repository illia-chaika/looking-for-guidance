package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const clientAPIRepo = "Gethe/wow-ui-source"

var clientAPIBuild = regexp.MustCompile(`^WoW Forever ([0-9]+\.[0-9]+\.[0-9]+)\.([0-9]+)$`)
var clientAPICommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func queryClientAPISource(dir string, raw json.RawMessage) (mcpObject, error) {
	var args struct {
		ID string `json:"snapshot_id"`
	}
	if err := readToolArgs(raw, &args); err != nil {
		return nil, err
	}
	s, err := loadGameSnapshot(dir, args.ID)
	if err != nil {
		return nil, err
	}
	return clientAPISource(dir, s, http.DefaultClient), nil
}

func clientAPISource(dir string, s gameSnapshot, client *http.Client) mcpObject {
	out := mcpObject{"captured": s.Captured, "available": false, "status": "unavailable"}
	header, _, _ := strings.Cut(s.Sections["game"], " · ")
	match := clientAPIBuild.FindStringSubmatch(header)
	if match == nil {
		out["reason"] = "build_unavailable"
		return out
	}
	version, build := match[1], match[2]
	out["version"], out["build"] = version, build

	// A verified commit stays usable across sessions, including when GitHub is offline.
	cacheDir := filepath.Join(dir, "client-api-sources")
	path := filepath.Join(cacheDir, version+"."+build)
	cached, _ := os.ReadFile(path)
	sha := string(cached)
	if !clientAPICommit.MatchString(sha) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var err error
		sha, err = findClientAPICommit(ctx, client, version+" ("+build+")")
		if err != nil {
			out["reason"], out["detail"] = "source_lookup_failed", err.Error()
			return out
		}
		if sha == "" {
			out["reason"] = "build_not_found"
			return out
		}
		if os.MkdirAll(cacheDir, 0o700) == nil {
			_ = writeFileAtomic(path, []byte(sha))
		}
	}
	ui := "https://github.com/" + clientAPIRepo + "/tree/" + sha + "/Interface/AddOns/"
	rawBase := "https://raw.githubusercontent.com/" + clientAPIRepo + "/" + sha + "/Interface/AddOns/"
	out["available"], out["status"], out["commit"] = true, "ok", sha
	out["ui_source_url"] = ui
	out["documentation_url"] = ui + "Blizzard_APIDocumentationGenerated"
	out["documentation_index_url"] = rawBase + "Blizzard_APIDocumentationGenerated/Blizzard_APIDocumentationGenerated.toc"
	out["raw_base_url"] = rawBase
	return out
}

func findClientAPICommit(ctx context.Context, client *http.Client, title string) (string, error) {
	// Match locally: the history contains repeated, out-of-order builds.
	ref := "forever"
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/commits?sha=%s&per_page=100&page=%d", clientAPIRepo, ref, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("GitHub's public commit history could not be reached")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("GitHub answered %s", resp.Status)
		}
		var commits []struct {
			SHA    string
			Commit struct{ Message string }
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&commits)
		resp.Body.Close()
		if err != nil || commits == nil {
			return "", errors.New("GitHub returned an unreadable commit history")
		}
		for _, entry := range commits {
			firstLine, _, _ := strings.Cut(entry.Commit.Message, "\n")
			if firstLine == title {
				if !clientAPICommit.MatchString(entry.SHA) {
					return "", errors.New("GitHub returned an invalid commit for this build")
				}
				return entry.SHA, nil
			}
		}
		if !strings.Contains(resp.Header.Get("Link"), `rel="next"`) {
			return "", nil
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return "", errors.New("GitHub's rate limit interrupted the commit history lookup")
		}
		if page == 1 {
			// A new release between pages must not shift entries across page boundaries.
			if len(commits) == 0 || !clientAPICommit.MatchString(commits[0].SHA) {
				return "", errors.New("GitHub returned an invalid Forever branch head")
			}
			ref = commits[0].SHA
		}
	}
}
