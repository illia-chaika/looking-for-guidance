package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A fake claude that writes down what it was asked to do.
const recordingClaude = "#!/bin/sh\necho \"$@\" > \"$HOME/claude-args\"\n"

// runShellScript runs a Terminal script the way Terminal does, through its #! line, with a home folder of its own and
// a fake curl that hands out the installer, or with no installer fails as curl does when it cannot download.
func runShellScript(t *testing.T, script, installer string, claude string) (home string, err error) {
	t.Helper()
	if !isFile("/bin/zsh") {
		t.Skip("the Terminal script is written for macOS's zsh")
	}
	home, bin := t.TempDir(), t.TempDir()
	curl := "#!/bin/sh\necho \"$@\" > \"$HOME/curl-args\"\n"
	if installer == "" {
		curl += "exit 22\n"
	} else {
		curl += "cat <<'EOF'\n" + installer + "EOF\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0o700); err != nil {
		t.Fatal(err)
	}
	if claude != "" {
		claude = strings.ReplaceAll(claude, "~", home)
		if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(claude, []byte(recordingClaude), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "Install Claude Code.command")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(script, "~", home)), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	return home, err
}

func readTrimmed(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

func TestInstallScriptRunsTheOfficialInstallerThenSignsIn(t *testing.T) {
	installer := "mkdir -p \"$HOME/.local/bin\"\nprintf '%s' '" + recordingClaude + "' > \"$HOME/.local/bin/claude\"\nchmod +x \"$HOME/.local/bin/claude\"\n"
	home, err := runShellScript(t, claudeShellScript(""), installer, "")
	if err != nil {
		t.Fatal(err)
	}
	curl, claude := readTrimmed(filepath.Join(home, "curl-args")), readTrimmed(filepath.Join(home, "claude-args"))
	if curl != "-fsSL https://claude.ai/install.sh" || claude != "auth login" {
		t.Fatalf("curl was given %q and claude %q", curl, claude)
	}
}

// An earlier claude in ~/.local/bin is not asked to sign in when the installer could not be downloaded: the window
// ends with curl's error instead.
func TestInstallScriptStopsWhenTheInstallerCannotBeDownloaded(t *testing.T) {
	home, err := runShellScript(t, claudeShellScript(""), "", "~/.local/bin/claude")
	if err == nil {
		t.Error("a failed download ended the script without an error")
	}
	if args := readTrimmed(filepath.Join(home, "claude-args")); args != "" {
		t.Errorf("claude was given %q after the installer failed to download", args)
	}
}

func TestSignInScriptRunsTheClaudeItFound(t *testing.T) {
	claude := "~/Claude's tools/claude"
	home, err := runShellScript(t, claudeShellScript(claude), "", claude)
	if err != nil {
		t.Fatal(err)
	}
	if args := readTrimmed(filepath.Join(home, "claude-args")); args != "auth login" {
		t.Fatalf("claude was given %q", args)
	}
	if curl := readTrimmed(filepath.Join(home, "curl-args")); curl != "" {
		t.Fatalf("the sign-in ran the installer: curl %q", curl)
	}
}

func TestPowerShellCommands(t *testing.T) {
	install := claudePowerShell("")
	// No double quotes: neither Go's Windows argument quoting nor PowerShell's passing to powershell.exe rewrites it.
	if !strings.Contains(install, "'"+claudeInstallPs1+"'") || strings.Contains(install, `"`) || !strings.HasSuffix(install, "auth login }") {
		t.Errorf("install command: %s", install)
	}
	if got := claudePowerShell(`C:\Users\O'Brien\.local\bin\claude.exe`); got != `& 'C:\Users\O''Brien\.local\bin\claude.exe' auth login` {
		t.Errorf("sign-in command: %s", got)
	}
	if got := psQuote("Bob’s"); got != "'Bob’’s'" {
		t.Errorf("a typographic apostrophe: %s", got)
	}
}
