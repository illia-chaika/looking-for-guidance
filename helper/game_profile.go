package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed game-adviser.md
var gameInstructions string

// A note for the agent in its working folder. The full role arrives with every conversation.
const gameFolderNote = `This is a player's personal World of Warcraft advice chat. Answer in the language of the player's message unless they ask for another. Keep game names in the language of the game client. The companion app sends the full instructions when a conversation opens or continues. For questions about the character's current state, use the wow_game tools with the snapshot of the current question.
`

// gameProjectDir is the folder the gameplay agent works in. The agent's conversations are tied to it, so it stays
// where it was first created; settings.json can name another one.
func gameProjectDir() string {
	dir := loadSettings().GameDir
	if dir == "" {
		if configDir() == "" {
			return ""
		}
		dir = filepath.Join(configDir(), "game")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if os.MkdirAll(dir, 0o700) != nil {
			return ""
		}
		os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(gameFolderNote), 0o600)
	}
	return dir
}

// The adviser's Codex profile: what a game chat may do, apart from the player's own Codex setup. The path of this
// app as the game data server is also given at every start (codexArgs), so the file's copy only serves a Codex
// someone runs by hand in this profile.
const codexProfileTemplate = `model = "gpt-6.1-sol"
model_reasoning_effort = "medium"
web_search = "live"
sandbox_mode = "read-only"

[features]
shell_tool = false
unified_exec = false
computer_use = false
apps = false

[mcp_servers.wow_game]
command = %s
args = ["--game-mcp"]
`

// codexProfileDir is where Codex keeps the adviser's profile and sign-in, or "" without a user settings folder.
func codexProfileDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "Looking for Guidance Codex")
}

// codexProfileExists tells a setup that has used Codex from one that never has.
func codexProfileExists() bool {
	dir := codexProfileDir()
	return dir != "" && isFile(filepath.Join(dir, "config.toml"))
}

// ensureCodexProfile writes the profile on first use of Codex and leaves an existing one as the player has it.
func ensureCodexProfile() error {
	dir := codexProfileDir()
	if dir == "" {
		return fmt.Errorf("no user settings folder")
	}
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(codexProfileTemplate, tomlString(exe))), 0o600); err != nil {
		return err
	}
	logf("Created the adviser's Codex profile in %s", dir)
	return nil
}

// tomlString quotes a path for TOML: literally where it can, so Windows paths keep their backslashes.
func tomlString(s string) string {
	if !strings.ContainsAny(s, "'\n\r") {
		return "'" + s + "'"
	}
	return strconv.Quote(s)
}

// codexArgs starts the app server with this app as the game data server, wherever the app is now: a profile
// written before the app was moved would otherwise point at a file that is gone.
func codexArgs() []string {
	args := []string{"app-server"}
	if exe, err := os.Executable(); err == nil {
		args = append(args, "-c", "mcp_servers.wow_game.command="+tomlString(exe), "-c", `mcp_servers.wow_game.args=["--game-mcp"]`)
	}
	return args
}

// Both Finder and the login item must open the same isolated gaming profile.
func useGameProfile() error {
	profile := codexProfileDir()
	if profile == "" {
		_, err := os.UserConfigDir()
		return err
	}
	if err := os.Setenv("CODEX_HOME", profile); err != nil {
		return err
	}
	for _, key := range []string{"CODEX_SQLITE_HOME", "OPENAI_API_KEY", "CODEX_API_KEY"} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	if gameProjectDir() == "" {
		return fmt.Errorf("Looking for Guidance couldn't set up the adviser's game folder")
	}
	return nil
}
