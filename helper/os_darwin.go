package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	wowCandidates = []string{"/Applications/World of Warcraft/" + foreverFolder, "/Applications/World of Warcraft/" + betaFolder}

	extraPath = []string{"/opt/homebrew/bin", "/usr/local/bin"}
)

type procKey struct {
	pid   int32
	start unix.Timeval
}

var procCache struct {
	sync.Mutex
	paths map[procKey]string
}

func processPaths() []string {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	uid := uint32(os.Getuid())
	procCache.Lock()
	defer procCache.Unlock()
	next := make(map[procKey]string, len(procs))
	var paths []string
	for i := range procs {
		p := &procs[i]
		if p.Eproc.Ucred.Uid != uid {
			continue
		}
		key := procKey{p.Proc.P_pid, p.Proc.P_starttime}
		path, ok := procCache.paths[key]
		if !ok {
			path = procPath(int(p.Proc.P_pid))
		}
		next[key] = path
		if path != "" {
			paths = append(paths, path)
		}
	}
	procCache.paths = next
	return paths
}

func procPath(pid int) string {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(buf) < 5 {
		return ""
	}
	path := buf[4:]
	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}
	return string(path)
}

func chooseFolder() (string, error) {
	out, err := exec.Command("osascript", "-e",
		`POSIX path of (choose folder with prompt "Choose your World of Warcraft folder")`).Output()
	return strings.TrimSpace(string(out)), err
}

// dialog shows an AppleScript dialog with the text as its argument, so nothing in it needs quoting. Cancel ends it with
// an error.
func dialog(text, script string) (string, error) {
	out, err := exec.Command("osascript", "-e", "on run argv", "-e", script, "-e", "end run", text).Output()
	return strings.TrimSpace(string(out)), err
}

// askText asks for one line of text; a hidden one is typed as dots. False means the player cancelled.
func askText(prompt string, hidden bool) (string, bool) {
	script := `text returned of (display dialog (item 1 of argv) default answer "" with title "Looking for Guidance"`
	if hidden {
		script += " with hidden answer"
	}
	text, err := dialog(prompt, script+")")
	return text, err == nil
}

func tell(message string) {
	dialog(message, `display dialog (item 1 of argv) with title "Looking for Guidance" buttons {"OK"} default button 1`)
}

func confirm(message string) bool {
	_, err := dialog(message, `display dialog (item 1 of argv) with title "Looking for Guidance" default button 2`)
	return err == nil
}

func openURL(url string) {
	cmd := exec.Command("open", url)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

// openTerminal has Terminal run the script in a new window. Terminal runs a .command file it is asked to open, and
// opening a file needs no permission to control Terminal, unlike AppleScript's do script.
func openTerminal(name, script string) error {
	dir := filepath.Join(os.TempDir(), "looking-for-guidance")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".command")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return err
	}
	// WriteFile keeps the mode of a file that is already there.
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return exec.Command("open", "-a", "Terminal", path).Run()
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "io.github.illia-chaika.looking-for-guidance.plist")
}

func setAutostart(on bool, exe string) error {
	path := launchAgentPath()
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	var escaped bytes.Buffer
	xml.EscapeText(&escaped, []byte(exe))
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.github.illia-chaika.looking-for-guidance</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + escaped.String() + `</string>
		<string>--watch</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(plist))
}

var installedCache = struct {
	sync.Mutex
	paths map[string]string
}{paths: map[string]string{}}

// An app opened from Finder does not get the PATH folders that Homebrew and npm add in the login shell. So it can miss
// an agent. Keep each found path while its file exists, so the login shell runs once per agent, not on every check.
func findInstalledOS(name string) string {
	installedCache.Lock()
	p := installedCache.paths[name]
	installedCache.Unlock()
	if p != "" && isFile(p) {
		return p
	}
	if p = lookUpInstalled(name); p != "" {
		installedCache.Lock()
		installedCache.paths[name] = p
		installedCache.Unlock()
	}
	return p
}

func lookUpInstalled(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/bin/zsh", "-lc", "command -v "+name).Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if p := strings.TrimSpace(lines[len(lines)-1]); filepath.IsAbs(p) && isFile(p) {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin")} {
		if p := filepath.Join(dir, name); isFile(p) {
			return p
		}
	}
	return ""
}

var quotedLanguage = regexp.MustCompile(`"([^"]+)"`)

// systemLanguages lists the languages the user prefers, most wanted first, as System Settings has them.
func systemLanguages() []string {
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return nil
	}
	var tags []string
	for _, m := range quotedLanguage.FindAllSubmatch(out, -1) {
		tags = append(tags, string(m[1]))
	}
	return tags
}
