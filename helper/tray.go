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
	// The first shows a newer version on its way in; the second asks GitHub now instead of at the daily check.
	update := systray.AddMenuItem("", "")
	update.Hide()
	checkUpdates := systray.AddMenuItem(tr("Check for Updates…"), "")
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
		if v.provider == "claude" {
			signIn.SetTitle(tr("Sign in to Claude Code…"))
		} else {
			signIn.SetTitle(tr("Sign in to ChatGPT…"))
		}
		visible(signIn, v.signIn)
		check(useCodex, v.provider != "claude")
		check(useClaude, v.provider == "claude")
		if v.runWithWoW {
			runWith.Check()
		} else {
			runWith.Uncheck()
		}
		update.SetTitle(updateTitle(v))
		visible(update, v.update != "")
		enabled(update, v.updating == updateReady || v.updating == updateFailed)
		checkUpdates.SetTitle(checkTitle(v))
		enabled(checkUpdates, v.check != checkRunning && v.updating != updateDownloading && v.updating != updateInstalling)
	}
	runWithWoW := a.settings.RunWithWoW
	a.mu.Unlock()

	if runWithWoW && !a.fromWatch {
		a.startWatcher()
	}
	go a.loop()
	go a.watchAuctions()
	go a.checkUpdates()

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
				go a.setUpClaude()
			} else {
				openURL(codexURL)
			}
		case <-installClaude.ClickedCh:
			go a.setUpClaude()
		case <-signIn.ClickedCh:
			if a.view().provider == "claude" {
				go a.setUpClaude()
			} else {
				a.signIn()
			}
		case <-update.ClickedCh:
			a.mu.Lock()
			stage, page := a.updateStage, a.updateURL
			a.mu.Unlock()
			switch stage {
			case updateReady:
				go a.installUpdate(true)
			case updateFailed:
				openURL(page)
			}
		case <-checkUpdates.ClickedCh:
			a.requestUpdateCheck()
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
		return tr("Downloading Looking for Guidance %s…", v.update)
	case updateReady:
		return tr("Install %s and Restart", v.update)
	case updateInstalling:
		return tr("Installing Looking for Guidance %s…", v.update)
	default:
		return tr("Update Failed: Download %s from GitHub…", v.update)
	}
}

func checkTitle(v trayView) string {
	switch v.check {
	case checkRunning:
		return tr("Checking for Updates…")
	case checkUpToDate:
		return tr("Looking for Guidance %s is up to date", version)
	default:
		return tr("Check for Updates…")
	}
}

func enabled(item *systray.MenuItem, on bool) {
	if on {
		item.Enable()
	} else {
		item.Disable()
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
