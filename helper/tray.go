package main

import (
	_ "embed"

	"fyne.io/systray"
)

var (
	//go:embed assets/tray-template.png
	trayTemplate []byte
	//go:embed assets/tray.ico
	trayIcon []byte
	//go:embed assets/warning-template.png
	warningTemplate []byte
	//go:embed assets/warning.ico
	warningIcon []byte
)

func (a *app) trayReady() {
	systray.SetTemplateIcon(trayTemplate, trayIcon)
	systray.SetTooltip("Looking for Guidance")

	// The tray library cannot remove an item's icon, so a status that needs the player has its own item.
	status := systray.AddMenuItem(tr("Waiting for WoW Forever to start"), "")
	status.Disable()
	alert := systray.AddMenuItem("", "")
	alert.SetTemplateIcon(warningTemplate, warningIcon)
	alert.Disable()
	alert.Hide()
	detail := systray.AddMenuItem("", "")
	detail.Disable()
	detail.Hide()
	installAgent := systray.AddMenuItem(tr("Install %s…", "Codex CLI"), "")
	installAgent.Hide()
	// Shown beside the first one only when no adviser is installed and none was chosen.
	installClaude := systray.AddMenuItem(tr("Install %s…", "Claude Code"), "")
	installClaude.Hide()
	signIn := systray.AddMenuItem(tr("Sign in to ChatGPT…"), "")
	signIn.Hide()
	systray.AddSeparator()
	adviser := systray.AddMenuItem(tr("Adviser"), "")
	useCodex := adviser.AddSubMenuItemCheckbox("Codex", "", a.view().provider != "claude")
	useClaude := adviser.AddSubMenuItemCheckbox("Claude Code", "", a.view().provider == "claude")
	runWith := systray.AddMenuItemCheckbox(tr("Start with WoW"), tr("Start the companion together with the game"), a.view().runWithWoW)
	choose := systray.AddMenuItem(tr("Choose the WoW folder…"), "")
	choose.Hide()
	retryAccess := systray.AddMenuItem(tr("Retry access to the WoW folder"), "")
	retryAccess.Hide()
	systray.AddSeparator()
	quit := systray.AddMenuItem(tr("Quit Looking for Guidance"), "")

	a.mu.Lock()
	a.show = func(v trayView) {
		status.SetTitle(v.status)
		alert.SetTitle(v.status)
		visible(status, !v.alert)
		visible(alert, v.alert)
		detail.SetTitle(v.detail)
		visible(detail, v.detail != "")
		visible(choose, v.choose)
		visible(retryAccess, v.retryAccess)
		installAgent.SetTitle(tr("Install %s…", v.installName))
		visible(installAgent, v.install)
		visible(installClaude, v.installOther)
		visible(signIn, v.signIn)
		check(useCodex, v.provider != "claude")
		check(useClaude, v.provider == "claude")
		if v.runWithWoW {
			runWith.Check()
		} else {
			runWith.Uncheck()
		}
	}
	runWithWoW := a.settings.RunWithWoW
	a.mu.Unlock()

	if runWithWoW && !a.fromWatch {
		a.startWatcher()
	}
	go a.loop()
	go a.watchAuctions()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-runWith.ClickedCh:
			a.setRunWithWoW(!a.view().runWithWoW)
		case <-retryAccess.ClickedCh:
			a.requestVolumeRetry()
		case <-choose.ClickedCh:
			go a.chooseFolder()
		case <-installAgent.ClickedCh:
			if a.view().provider == "claude" {
				openURL(claudeURL)
			} else {
				openURL(codexURL)
			}
		case <-installClaude.ClickedCh:
			openURL(claudeURL)
		case <-signIn.ClickedCh:
			a.signIn()
		case <-useCodex.ClickedCh:
			a.setProvider("")
		case <-useClaude.ClickedCh:
			a.setProvider("claude")
		case <-quit.ClickedCh:
			a.exit()
			return
		}
	}
}

func updateTitle(v trayView) string {
	switch v.updating {
	case updateDownloading:
		return "Downloading Looking for Guidance " + v.update + "…"
	case updateReady:
		return "Install " + v.update + " and Restart"
	case updateInstalling:
		return "Installing Looking for Guidance " + v.update + "…"
	default:
		return "Update Failed: Download " + v.update + " from GitHub…"
	}
}

func check(item *systray.MenuItem, on bool) {
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
}

func visible(item *systray.MenuItem, show bool) {
	if show {
		item.Show()
	} else {
		item.Hide()
	}
}
