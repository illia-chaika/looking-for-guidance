package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
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

// Both Finder and the login item must open the same isolated gaming profile.
func useGameProfile() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	profile := filepath.Join(base, "Looking for Guidance Codex")
	if err := os.Setenv("CODEX_HOME", profile); err != nil {
		return err
	}
	for _, key := range []string{"CODEX_SQLITE_HOME", "OPENAI_API_KEY", "CODEX_API_KEY"} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	if gameProjectDir() == "" {
		return fmt.Errorf("не удалось определить папку игрового проекта")
	}
	return nil
}
