package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func emptyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	return home
}

// Someone who has Claude Code and no Codex gets Claude Code without choosing; nobody who chose or used Codex is moved.
func TestFirstAdviser(t *testing.T) {
	emptyHome(t)
	has := func(names ...string) func(string) string {
		return func(name string) string {
			if slices.Contains(names, name) {
				return "/bin/" + name
			}
			return ""
		}
	}
	for _, c := range []struct {
		name, chosen string
		find         func(string) string
		want         string
	}{
		{"only Claude Code", "", has("claude"), "claude"},
		{"both installed", "", has("claude", "codex"), ""},
		{"only Codex", "", has("codex"), ""},
		{"neither", "", has(), ""},
		{"Claude Code chosen", "claude", has("codex"), "claude"},
	} {
		if got := firstAdviser(c.chosen, c.find); got != c.want {
			t.Errorf("%s: adviser %q, want %q", c.name, got, c.want)
		}
	}
	if err := ensureCodexProfile(); err != nil {
		t.Fatal(err)
	}
	if got := firstAdviser("", has("claude")); got != "" {
		t.Errorf("a setup that used Codex was moved to %q when its CLI went missing", got)
	}
}

// The app writes the Codex profile itself on first use and never rewrites one the player has.
func TestCodexProfileIsCreatedOnce(t *testing.T) {
	emptyHome(t)
	if codexProfileExists() {
		t.Fatal("a profile exists in an empty home")
	}
	if err := ensureCodexProfile(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(codexProfileDir(), "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	for _, want := range []string{`sandbox_mode = "read-only"`, "shell_tool = false", "unified_exec = false", `web_search = "live"`,
		"[mcp_servers.wow_game]", "command = " + tomlString(exe), `args = ["--game-mcp"]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the profile lacks %q:\n%s", want, data)
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("the profile is readable by others: %v", info.Mode())
	}
	if err := os.WriteFile(path, []byte("model = \"mine\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureCodexProfile(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "model = \"mine\"\n" {
		t.Fatalf("the player's own profile was rewritten: %s", data)
	}

	// The game data server is named at every start, so a profile written before the app moved still works.
	args := codexArgs()
	if args[0] != "app-server" || !slices.Contains(args, "mcp_servers.wow_game.command="+tomlString(exe)) || !slices.Contains(args, `mcp_servers.wow_game.args=["--game-mcp"]`) {
		t.Fatalf("Codex is started with %q", args)
	}
	if got := tomlString(`C:\Users\me\LookingForGuidance.exe`); got != `'C:\Users\me\LookingForGuidance.exe'` {
		t.Errorf("a Windows path became %s", got)
	}
	if got := tomlString("/Users/o'neil/app"); got != `"/Users/o'neil/app"` {
		t.Errorf("a path with an apostrophe became %s", got)
	}
}

// With Claude Code as the adviser and no trace of Codex here, Codex is neither looked for nor given a profile.
func TestClaudeAloneNeverStartsCodex(t *testing.T) {
	emptyHome(t)
	s := newSession(t.TempDir(), map[string]string{}, func() {})
	s.projectDir, s.provider = t.TempDir(), "claude"
	if s.wantsCodex() {
		t.Fatal("Codex is wanted beside Claude Code though it was never used")
	}
	if codexProfileExists() {
		t.Fatal("a Codex profile appeared for someone who plays with Claude Code")
	}
	s.setProvider("")
	if !s.wantsCodex() {
		t.Fatal("Codex is not started after the player chose it")
	}
	if err := ensureCodexProfile(); err != nil {
		t.Fatal(err)
	}
	s.setProvider("claude")
	if !s.wantsCodex() {
		t.Fatal("older Codex chats are not listed after switching to Claude Code")
	}
}

// With no adviser installed and none chosen, the game is told so without singling out Codex.
func TestNoAdviserIsNamedNeutrally(t *testing.T) {
	emptyHome(t)
	s := newSession(t.TempDir(), map[string]string{}, func() {})
	s.projectDir = t.TempDir()
	s.findAgent = func(string) string { return "" }
	s.superviseCodex(time.Now())
	if p := s.payload(); p.Codex != "missing" || p.Note != noAdviserNote {
		t.Fatalf("payload says %q: %q", p.Codex, p.Note)
	}
	if !s.view().noAdviser {
		t.Fatal("the menu is not told that no adviser was found")
	}
	// Someone who used Codex before is told about Codex.
	if err := ensureCodexProfile(); err != nil {
		t.Fatal(err)
	}
	s.nextStart = time.Time{}
	s.superviseCodex(time.Now())
	if p := s.payload(); p.Note != codexNotes["missing"] || s.view().noAdviser {
		t.Fatalf("payload says %q", p.Note)
	}
}

// "Sign in to ChatGPT…" in the menu has Codex start its own browser sign-in; the app opens the page Codex names.
func TestSignInFromTheMenu(t *testing.T) {
	home, wow := emptyHome(t), t.TempDir()
	if _, err := install(wow); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wow, "WTF", "Account"), 0o700); err != nil {
		t.Fatal(err)
	}
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	signedOut := filepath.Join(t.TempDir(), "signed-out")
	if err := os.WriteFile(signedOut, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "codex.log")
	t.Setenv("LOOKING_FOR_GUIDANCE_TEST_MODE", "codex")
	t.Setenv("CODEX_LOG", log)
	t.Setenv("CODEX_HOLD", filepath.Join(t.TempDir(), "hold"))
	t.Setenv("CODEX_WORK", home)
	t.Setenv("CODEX_SIGNED_OUT", signedOut)
	s := newSession(wow, map[string]string{}, func() {})
	s.findAgent = func(name string) string {
		if name == "codex" {
			return fake
		}
		return ""
	}
	opened := make(chan string, 1)
	s.openURL = func(url string) { opened <- url }
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		s.run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	waitFor := func(what string, ok func(payload) bool) payload {
		t.Helper()
		var p payload
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if got, err := tryPayloadFile(t, payloadPath(wow)); err == nil {
				if p = got; ok(p) {
					return p
				}
			}
		}
		t.Fatalf("%s: %+v", what, p)
		return p
	}
	if p := waitFor("a signed-out Codex", func(p payload) bool { return p.Codex == "signed-out" }); p.Note != codexNotes["signed-out"] {
		t.Fatalf("the game is told %q", p.Note)
	}
	if data, _ := os.ReadFile(log); strings.Contains(string(data), "account/login/start") {
		t.Fatal("a sign-in started before the player asked for it")
	}
	s.signIn()
	select {
	case url := <-opened:
		if url != "https://auth.openai.com/oauth/authorize?state=test" {
			t.Fatalf("opened %q", url)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sign-in page was not opened")
	}
	waitFor("Codex signed in without waiting for the next slow check", func(p payload) bool { return p.Codex == "ready" })
}

// Windows are named by their length; one without a length, or an account that reports none, shows nothing.
func TestAllowanceWindows(t *testing.T) {
	mins, week, reset := int64(300), int64(10080), int64(1795000000)
	both := cxRateLimits{Primary: &cxRateWindow{UsedPercent: 29, Mins: &week, ResetsAt: &reset}, Secondary: &cxRateWindow{UsedPercent: 104, Mins: &mins}}
	if got, want := both.windows(), []payloadLimit{{Mins: 300, Left: 0}, {Mins: 10080, Left: 71, Resets: reset}}; !slices.Equal(got, want) {
		t.Fatalf("windows: %+v", got)
	}
	if got := (cxRateLimits{Primary: &cxRateWindow{UsedPercent: 10}}).windows(); len(got) != 0 {
		t.Fatalf("a window without a length is shown: %+v", got)
	}
	if got := (claudeRateLimit{}).windows(); len(got) != 0 {
		t.Fatalf("an event without windows shows %+v", got)
	}

	// A finished turn and Codex's own notice both ask for a fresh snapshot.
	s := newSession(t.TempDir(), map[string]string{}, func() {})
	s.notify("account/rateLimits/updated", []byte(`{"rateLimits":{"primary":{"usedPercent":30}}}`))
	if !s.limitsWanted {
		t.Fatal("Codex's notice about the allowance was ignored")
	}
}
