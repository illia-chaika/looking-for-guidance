package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Live WoW Forever and its beta, which closed before launch. Live comes first wherever both are found.
const (
	foreverFolder = "_forever_"
	betaFolder    = "_classic_beta_"
)

var (
	foreverFolders  = []string{foreverFolder, betaFolder}
	foreverProducts = []string{"wowf", "wow_classic_beta"}
	foreverExes     = []string{"Wow.exe", "Wow-arm64.exe", "WowB.exe", "WowB-arm64.exe"}
)

// Battle.net records the game product in the folder's .flavor.info.
func flavorOf(dir string) (string, bool) {
	flavor, err := os.ReadFile(filepath.Join(dir, ".flavor.info"))
	if err != nil {
		return "", false
	}
	_, product, ok := strings.Cut(string(flavor), "\n")
	return strings.TrimSpace(product), ok
}

func hasForeverExe(dir string) bool {
	return slices.ContainsFunc(foreverExes, func(name string) bool { return isFile(filepath.Join(dir, name)) })
}

// Use Blizzard's own marks for a Forever folder: the flavor file, or else the executable.
func foreverInstall(dir string) bool {
	if product, ok := flavorOf(dir); ok {
		return slices.Contains(foreverProducts, product)
	}
	return hasForeverExe(dir)
}

// On macOS the game runs from an .app inside the Forever folder, so the executable's own folder is never the install.
func installOf(path string) string {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if slices.Contains(foreverFolders, filepath.Base(dir)) && foreverInstall(dir) {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func validWowDir(dir string) bool {
	if dir == "" {
		return false
	}
	if _, ok := flavorOf(dir); ok || hasForeverExe(dir) {
		return foreverInstall(dir)
	}
	for _, name := range []string{"Interface", "WTF", "Logs"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func resolveWowDir(saved string) string {
	// A player who saved the beta's folder plays live beside it now.
	if filepath.Base(saved) == betaFolder {
		if live := filepath.Join(filepath.Dir(saved), foreverFolder); validWowDir(live) {
			saved = live
		}
	}
	for _, dir := range append([]string{saved}, wowCandidates...) {
		if validWowDir(dir) {
			return filepath.Clean(dir)
		}
	}
	for _, path := range processPaths() {
		if dir := installOf(path); dir != "" {
			return dir
		}
	}
	return ""
}

func normalizeChoice(dir string) string {
	dir = filepath.Clean(strings.TrimSpace(dir))
	for _, folder := range foreverFolders {
		if inner := filepath.Join(dir, folder); validWowDir(inner) {
			return inner
		}
	}
	if validWowDir(dir) {
		return dir
	}
	return ""
}

// The login watcher only needs to know whether to launch the app. Never probe a
// removable volume to identify a process that macOS has already named.
func runningWowDir(saved string, paths []string) string {
	for _, path := range paths {
		name := filepath.Base(path)
		dir := filepath.Dir(path)
		switch {
		case slices.ContainsFunc(foreverExes, func(exe string) bool { return strings.EqualFold(exe, name) }):
			// Windows installs the executable directly in its game directory.
		case name == "World of Warcraft" || name == "World of Warcraft Beta":
			bundle := filepath.Dir(filepath.Dir(dir))
			if !strings.EqualFold(filepath.Base(bundle), name+".app") ||
				!strings.EqualFold(dir, filepath.Join(bundle, "Contents", "MacOS")) {
				continue
			}
			dir = filepath.Dir(bundle)
		default:
			continue
		}

		if saved == "" {
			if slices.ContainsFunc(foreverFolders, func(folder string) bool { return strings.EqualFold(filepath.Base(dir), folder) }) {
				return dir
			}
			continue
		}
		if strings.EqualFold(dir, filepath.Clean(saved)) {
			return dir
		}
		// A previously configured beta may have been upgraded to live next to it.
		if strings.EqualFold(filepath.Base(saved), betaFolder) &&
			strings.EqualFold(dir, filepath.Join(filepath.Dir(saved), foreverFolder)) {
			return dir
		}
	}
	return ""
}

func wowRunning(dir string) bool {
	return dir != "" && runningWowDir(dir, processPaths()) != ""
}
