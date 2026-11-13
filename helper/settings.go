package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type settings struct {
	CodexPath    string `json:"codexPath,omitempty"`
	WowDir       string `json:"wowDir"`
	RunWithWoW   bool   `json:"runWithWoW"`
	DeclinedMove bool   `json:"declinedMove,omitempty"`
	// Blizzard's running hotfix article for WoW: Forever, when it moves to a new address.
	HotfixURL string `json:"hotfixUrl,omitempty"`
	// The gameplay agent's working folder, when it is not the one inside the settings folder. Chats belong to it.
	GameDir string `json:"gameDir,omitempty"`
	// The agent that answers in the game and prepares the journal: "codex" (the default) or "claude".
	Provider string `json:"provider,omitempty"`
	// The language of the companion's own menu, such as "ru" or "de", when it should differ from the system's.
	Language string `json:"language,omitempty"`
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "Looking for Guidance")
}

func loadSettings() settings {
	path := filepath.Join(configDir(), "settings.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return settings{}
	}
	var s settings
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		logf("Couldn't read %s, so Looking for Guidance is using default settings: %v. Choosing a WoW folder or turning on Run with WoW saves new ones.", path, err)
		return settings{}
	}
	return s
}

func saveSettings(s settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(configDir(), "settings.json"), append(data, '\n'))
}

func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}

const logLimit = 1 << 20

var logState struct {
	sync.Mutex
	file   *os.File
	prefix string
}

func openLog(prefix string) {
	logState.Lock()
	defer logState.Unlock()
	logState.prefix = prefix
	f, err := os.OpenFile(filepath.Join(configDir(), "helper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		if logState.file != nil {
			logState.file.Close()
		}
		f.Chmod(0o600)
		logState.file = f
	}
}

func logf(format string, args ...any) {
	logState.Lock()
	defer logState.Unlock()
	line := time.Now().Format("2006-01-02 15:04:05 ") + logState.prefix + fmt.Sprintf(format, args...) + "\n"
	f := logState.file
	if f == nil {
		os.Stderr.WriteString(line)
		return
	}
	if info, err := f.Stat(); err == nil && info.Size() > logLimit {
		f.Truncate(0)
	}
	f.WriteString(line)
}
