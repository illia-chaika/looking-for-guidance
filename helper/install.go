package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	chat "github.com/illia-chaika/looking-for-guidance"
)

const (
	offlinePayload = `{"v":4,"rev":0,"codex":"offline","note":"Открой Looking for Guidance на Mac."}`
	payloadFolder  = "looking-for-guidance-payload"
	// One slot delivers one payload read per interface session. The add-on counts the installed slots itself.
	slotCount = 512
	bitCount  = 128
)

func slotName(n int) string { return fmt.Sprintf("looking-for-guidance-slot-%03d", n) }
func bitName(n int) string  { return fmt.Sprintf("looking-for-guidance-bit-%03d", n) }

func slotTOC(n int) string {
	return fmt.Sprintf(`## Interface: 16001
## Title: Looking for Guidance — канал %03d
## Notes: Канал сообщений игрового помощника.
## Group: looking-for-guidance
## DefaultState: enabled
## LoadOnDemand: 1
## Dependencies: looking-for-guidance

..\%s\Payload.lua
`, n, payloadFolder)
}

func bitTOC(n int) string {
	return fmt.Sprintf("## Interface: 16001\n## Title: Looking for Guidance — канал данных %03d\n## Group: looking-for-guidance\n## LoadOnDemand: 1\n", n)
}

func payloadPath(wowDir string) string {
	return filepath.Join(addOnsDir(wowDir), payloadFolder, "Payload.lua")
}

type channel struct{ name, toc string }

func channels() []channel {
	var list []channel
	for n := 1; n <= slotCount; n++ {
		list = append(list, channel{slotName(n), slotTOC(n)})
	}
	for n := 1; n <= bitCount; n++ {
		list = append(list, channel{bitName(n), bitTOC(n)})
	}
	return list
}

func tocVersion(toc []byte) string {
	for _, line := range strings.Split(string(toc), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "## Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func addOnsDir(wowDir string) string { return filepath.Join(wowDir, "Interface", "AddOns") }

// Missing files need setup. A refused read must reach the caller so it can
// stop without trying to replace files that the user did not grant access to.
func matchesFile(path string, expected []byte) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil && bytes.Equal(data, expected), err
}

func addonCurrent(addOns string) (bool, error) {
	complete := true
	err := fs.WalkDir(chat.Addon, "looking-for-guidance", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(chat.Addon, path)
		if err != nil {
			return err
		}
		ok, err := matchesFile(filepath.Join(addOns, filepath.FromSlash(path)), want)
		if err != nil {
			return err
		}
		if !ok {
			complete = false
			return fs.SkipAll
		}
		return nil
	})
	return complete && err == nil, err
}

type installPlan struct {
	addon, payload bool
	channels       []channel
}

func (p installPlan) needed() bool { return p.addon || p.payload || len(p.channels) != 0 }

func checkInstallation(dir string) (installPlan, error) {
	addOns := addOnsDir(dir)
	var p installPlan
	ok, err := addonCurrent(addOns)
	if err != nil {
		return p, err
	}
	p.addon = !ok
	info, err := os.Stat(payloadPath(dir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return p, err
	}
	p.payload = err != nil || !info.Mode().IsRegular()
	for _, c := range channels() {
		ok, err := matchesFile(filepath.Join(addOns, c.name, c.name+".toc"), []byte(c.toc))
		if err != nil {
			return p, err
		}
		if !ok {
			p.channels = append(p.channels, c)
		}
	}
	return p, nil
}

func install(wowDir string) ([]string, error) {
	addOns := addOnsDir(wowDir)
	var done []string
	p, err := checkInstallation(wowDir)
	if err != nil {
		return done, err
	}
	if err := os.MkdirAll(addOns, 0o755); err != nil {
		return done, err
	}
	if p.addon {
		if err := copyAddon(addOns); err != nil {
			return done, fmt.Errorf("installing the addon: %w", err)
		}
		want, _ := fs.ReadFile(chat.Addon, "looking-for-guidance/looking-for-guidance.toc")
		done = append(done, "installed Looking for Guidance "+tocVersion(want))
	}
	if p.needed() {
		path := payloadPath(wowDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return done, err
		}
		if err := os.WriteFile(path, payloadLua([]byte(offlinePayload)), 0o644); err != nil {
			return done, err
		}
		done = append(done, "wrote the offline payload")
	}
	updated := 0
	for _, c := range p.channels {
		dir := filepath.Join(addOns, c.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return done, err
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".toc"), []byte(c.toc), 0o644); err != nil {
			return done, err
		}
		updated++
	}
	if updated > 0 {
		done = append(done, fmt.Sprintf("prepared %d slot and bit addons", updated))
	}
	if removed, err := removeSurplusSlots(addOns); err != nil {
		return done, err
	} else if removed > 0 {
		done = append(done, fmt.Sprintf("removed %d slot addons above the current count", removed))
	}
	changed, err := collapseCategory(wowDir)
	if err != nil {
		return done, fmt.Errorf("collapsing the Looking for Guidance group in the AddOns list: %w", err)
	}
	if changed {
		done = append(done, "collapsed the Looking for Guidance group in the AddOns list")
	}
	return done, nil
}

func copyAddon(addOns string) error {
	target := filepath.Join(addOns, "looking-for-guidance")
	tmp := filepath.Join(addOns, ".looking-for-guidance.new")
	old := filepath.Join(addOns, ".looking-for-guidance.old")
	os.RemoveAll(tmp)
	os.RemoveAll(old)
	err := fs.WalkDir(chat.Addon, "looking-for-guidance", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(tmp, filepath.FromSlash(strings.TrimPrefix(path, "looking-for-guidance")))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := fs.ReadFile(chat.Addon, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(target, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Rename(old, target)
		return err
	}
	return os.RemoveAll(old)
}

// The add-on uses every consecutive slot it finds, so a lowered count must not leave older folders behind.
func removeSurplusSlots(addOns string) (int, error) {
	entries, err := os.ReadDir(addOns)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		digits, ok := strings.CutPrefix(e.Name(), "looking-for-guidance-slot-")
		n, numErr := strconv.Atoi(digits)
		if !ok || numErr != nil || n <= slotCount {
			continue
		}
		if err := os.RemoveAll(filepath.Join(addOns, e.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

var collapsedDeclaration = regexp.MustCompile(`g_addonCategoriesCollapsed\s*=\s*\{`)

func collapseCategory(wowDir string) (bool, error) {
	dir := filepath.Join(wowDir, "WTF", "SavedVariables")
	path := filepath.Join(dir, "Blizzard_AddOnList.lua")
	source, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	if bytes.Contains(source, []byte(`["looking-for-guidance"] =`)) {
		return false, nil
	}
	if !collapsedDeclaration.Match(source) {
		source = append(source, "g_addonCategoriesCollapsed = {\n}\n"...)
	}
	loc := collapsedDeclaration.FindIndex(source)
	updated := append(append(append([]byte{}, source[:loc[1]]...), "\n\t[\"looking-for-guidance\"] = true,"...), source[loc[1]:]...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	return true, writeFileAtomic(path, updated)
}
