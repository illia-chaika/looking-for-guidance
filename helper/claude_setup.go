package main

import (
	"runtime"
	"strings"
)

// The installer commands are the ones Claude Code's setup page gives (claudeURL). The companion runs them in a terminal
// window it opens, so a player who never uses a terminal types nothing, and whatever the installer or the sign-in asks
// (a code from the browser, say) has a place to be answered.
const (
	claudeInstallSh  = "curl -fsSL https://claude.ai/install.sh | bash"
	claudeInstallPs1 = "irm https://claude.ai/install.ps1 | iex"
)

// setUpClaude installs Claude Code and signs in, or with Claude Code already installed only signs in. The game
// session checks Claude Code every 30 seconds, so the menu follows once the player finishes in the window.
func (a *app) setUpClaude() {
	path := findInstalled("claude")
	name := "Install Claude Code"
	if path != "" {
		name = "Sign in to Claude Code"
	}
	script := claudeShellScript(path)
	if runtime.GOOS == "windows" {
		script = claudePowerShell(path)
	}
	if err := openTerminal(name, script); err != nil {
		logf("Couldn't open a terminal for %s, so opened the setup page instead: %v", name, err)
		openURL(claudeURL)
		return
	}
	logf("Opened a terminal for %s", name)
}

// claudeShellScript is the script Terminal runs on macOS: the official installer, then the sign-in of the claude it
// installs, or with path only the sign-in of that claude.
func claudeShellScript(path string) string {
	if path != "" {
		return "#!/bin/zsh\n" + shellQuote(path) + " auth login\n"
	}
	// With pipefail an installer that could not be downloaded is not followed by a sign-in that cannot work.
	return "#!/bin/zsh\nsetopt pipefail\n" + claudeInstallSh + ` && "$HOME/.local/bin/claude" auth login` + "\n"
}

// claudePowerShell is the same for a PowerShell window on Windows. The installer runs in a PowerShell of its own: on a
// failure it calls exit, which would otherwise close the window before the player could read why.
func claudePowerShell(path string) string {
	if path != "" {
		return "& " + psQuote(path) + " auth login"
	}
	return "powershell.exe -NoProfile -Command '" + claudeInstallPs1 + "'; " +
		`if ($LASTEXITCODE -eq 0) { & (Join-Path $env:USERPROFILE '.local\bin\claude.exe') auth login }`
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// PowerShell ends a single-quoted string at any of these quotation marks, and reads a doubled one as itself.
var psQuotes = strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛")

func psQuote(s string) string { return "'" + psQuotes.Replace(s) + "'" }
