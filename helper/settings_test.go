package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAtomicWritesArePrivateAndConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writeFileAtomic(path, []byte("complete")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "complete" {
		t.Fatalf("incomplete write: %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("conversation permissions: %o", info.Mode().Perm())
	}
}

// The gameplay agent's folder lives inside the settings folder unless settings.json names another one, and it is
// created with a note for the agent the first time it is needed.
func TestGameFolderDefaultsToTheSettingsFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("AppData", filepath.Join(home, "config"))
	want := filepath.Join(configDir(), "game")
	if got := gameProjectDir(); got != want {
		t.Fatalf("game folder is %q, want %q", got, want)
	}
	if note, err := os.ReadFile(filepath.Join(want, "AGENTS.md")); err != nil || string(note) != gameFolderNote {
		t.Fatalf("the folder has no note for the agent: %v", err)
	}
	other := filepath.Join(home, "elsewhere")
	if err := saveSettings(settings{GameDir: other}); err != nil {
		t.Fatal(err)
	}
	if got := gameProjectDir(); got != other {
		t.Fatalf("settings.json did not move the game folder: %q", got)
	}
}
