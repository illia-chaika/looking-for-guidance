package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLiveSearchOnGameStartAndResume(t *testing.T) {
	for _, game := range []bool{true, false} {
		name := "other project"
		if game {
			name = "Forever project"
		}
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			s := newSession("", map[string]string{}, func() {})
			if game {
				s.projectDir = folder
			}
			s.upsert(cxThread{ID: "stored", Cwd: folder, Status: cxStatus{Type: "notLoaded"}})
			var methods []string
			s.client = rpcClientFor(t, func(method string, raw json.RawMessage) (json.RawMessage, *rpcError) {
				methods = append(methods, method)
				if method == "turn/start" {
					return json.RawMessage(`{"turn":{"id":"example","status":"inProgress"}}`), nil
				}
				var params struct {
					ThreadID              string            `json:"threadId"`
					DeveloperInstructions string            `json:"developerInstructions"`
					Config                map[string]string `json:"config"`
				}
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
				if game && (params.Config["web_search"] != "live" || params.DeveloperInstructions != gameInstructions) {
					t.Fatalf("%s did not request live search with the game advisor", method)
				}
				if !game && (params.Config != nil || params.DeveloperInstructions != "") {
					t.Fatalf("%s applied game settings to an unrelated project", method)
				}
				id := "new"
				if method == "thread/resume" {
					id = params.ThreadID
				}
				resp, err := json.Marshal(map[string]any{"thread": cxThread{ID: id, Cwd: folder, Status: cxStatus{Type: "idle"}}})
				if err != nil {
					t.Fatal(err)
				}
				return resp, nil
			})
			s.create(packet{Cwd: folder, Text: "Quest question"})
			s.reply(packet{Thread: "stored", Text: "Dungeon question"})
			if !slices.Equal(methods, []string{"thread/start", "turn/start", "thread/resume", "turn/start"}) {
				t.Fatalf("unexpected calls: %v", methods)
			}
		})
	}
}

// Codex reports a resumed thread's folder with symbolic links resolved. The game project is reached through a link
// since the workspace was moved, and the first reply in an older chat failed until the folders were compared as places.
func TestResumeAcceptsTheGameProjectBehindALink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "game-workspace")
	link := filepath.Join(t.TempDir(), "wow-forever")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	real, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	s := newSession("", map[string]string{}, func() {})
	s.projectDir = link
	s.upsert(cxThread{ID: "stored", Cwd: link, Status: cxStatus{Type: "notLoaded"}})
	s.upsert(cxThread{ID: "elsewhere", Cwd: link, Status: cxStatus{Type: "notLoaded"}})
	var methods []string
	s.client = rpcClientFor(t, func(method string, raw json.RawMessage) (json.RawMessage, *rpcError) {
		methods = append(methods, method)
		if method == "turn/start" {
			return json.RawMessage(`{"turn":{"id":"example","status":"inProgress"}}`), nil
		}
		var params struct {
			ThreadID string `json:"threadId"`
		}
		json.Unmarshal(raw, &params)
		folder := real
		if params.ThreadID == "elsewhere" {
			folder = t.TempDir()
		}
		resp, _ := json.Marshal(map[string]any{"thread": cxThread{ID: params.ThreadID, Cwd: folder, Status: cxStatus{Type: "idle"}}})
		return resp, nil
	})
	s.reply(packet{Thread: "stored", Text: "Dungeon question"})
	if !slices.Equal(methods, []string{"thread/resume", "turn/start"}) {
		t.Fatalf("the reply did not reach Codex: %v", methods)
	}
	if got := s.threads["stored"].info.Cwd; got != link {
		t.Fatalf("the chat left the game project: %s", got)
	}
	s.reply(packet{Thread: "elsewhere", Text: "Another question"})
	if !slices.Equal(methods, []string{"thread/resume", "turn/start", "thread/resume"}) || s.threads["elsewhere"].err == "" {
		t.Fatalf("a chat resumed in another folder was accepted: %v", methods)
	}
}
