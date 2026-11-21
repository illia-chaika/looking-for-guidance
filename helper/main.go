package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"
)

var version = "dev"

const fromWatchEnv = "LOOKING_FOR_GUIDANCE_FROM_WATCH"

func main() {
	if err := useGameProfile(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--game-mcp" {
		if err := runGameMCP(os.Stdin, os.Stdout, snapshotDir()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	dir := configDir()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "Looking for Guidance couldn't find your user settings folder. Set up a user profile before starting it.")
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Looking for Guidance couldn't open your settings folder: %v\n", err)
		return
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Looking for Guidance couldn't protect your settings folder: %v\n", err)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--watch" {
		runWatcher()
		return
	}
	release, ok := lockFile(filepath.Join(configDir(), "tray.lock"), os.Getenv(handoffEnv) == "1")
	if !ok {
		return
	}
	defer release()
	openLog("")
	movedFrom := os.Getenv(movedFromEnv)
	os.Unsetenv(handoffEnv)
	os.Unsetenv(movedFromEnv)
	s := loadSettings()
	if first := firstAdviser(s.Provider, findInstalled); first != s.Provider {
		s.Provider = first
		if err := saveSettings(s); err != nil {
			logf("Couldn't save settings: %v", err)
		}
		logf("No adviser was chosen and only Claude Code is installed: using it")
	}
	if exe, from, err := placeApp(&s); err != nil {
		logf("Couldn't move Looking for Guidance to the folder for apps: %v", err)
	} else if exe != "" {
		err := launchApp(exe, movedFromEnv+"="+from)
		if err == nil {
			logf("Moved Looking for Guidance to %s; opening it there", exe)
			return
		}
		logf("Couldn't open %s: %v", exe, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{
		ctx: ctx, cancel: cancel, loopDone: make(chan struct{}),
		fromWatch: os.Getenv(fromWatchEnv) == "1", settings: s, leftovers: leftovers(movedFrom),
		rescan: make(chan struct{}, 1), checkNow: make(chan struct{}, 1), origins: map[string]string{},
	}
	a.exit = a.requestQuit
	os.Unsetenv(fromWatchEnv)
	logf("Looking for Guidance %s started (%s)", version, map[bool]string{true: "opened with WoW", false: "opened by hand"}[a.fromWatch])
	// Shut down after the tray loop returns, not in an exit callback. On macOS the tray library does not call the
	// exit callback for Quit. On Windows it calls it while the process exits.
	systray.Run(a.trayReady, nil)
	a.shutdown()
}

type app struct {
	ctx       context.Context
	cancel    context.CancelFunc
	loopDone  chan struct{}
	exit      func()
	fromWatch bool
	rescan    chan struct{}
	checkNow  chan struct{}
	origins   map[string]string
	// removeErrs is used only by removeLeftovers, on the loop goroutine.
	removeErrs map[string]string

	mu                  sync.Mutex
	settings            settings
	wowDir              string
	running             bool
	denied, retryVolume bool
	restart             bool
	sess                *session
	stopSess            func()
	problem             string
	updateVer           string
	updateURL           string
	updateStage         updateStage
	staged              string
	stageDir            string
	leftovers           []string
	checking            updateCheck
	auctions            bool // a Battle.net API client is stored
	show                func(trayView)
	shown               trayView

	// While access is refused the tray still watches the process list, so that a new launch of the game is one
	// more attempt: a refusal can be a passing disk error at the moment the game closed.
	deniedWatched, deniedPlaying bool
}

type trayView struct {
	status, detail, update string
	provider, installName  string
	alert, signIn          bool
	installOther           bool
	choose, runWithWoW     bool
	auctions               bool
	install, retryAccess   bool
	check                  updateCheck
	updating               updateStage
}

func (a *app) loop() {
	defer close(a.loopDone)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		a.poll()
		a.removeLeftovers()
		a.installWhenIdle()
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
		case <-a.rescan:
		}
	}
}

func (a *app) requestVolumeRetry() {
	a.mu.Lock()
	a.retryVolume = true
	a.mu.Unlock()
	select {
	case a.rescan <- struct{}{}:
	default:
	}
}

func (a *app) volumeRefused(err error) bool {
	if !errors.Is(err, os.ErrPermission) {
		return false
	}
	a.mu.Lock()
	first := !a.denied
	a.denied = true
	a.mu.Unlock()
	if first {
		logf("Game folder permission denied; waiting for Retry access or the next launch of the game")
	}
	return true
}

// firstAdviser settles a setup where nobody chose an adviser and Codex was never used here: with Claude Code
// installed and no Codex CLI, Claude Code it is, and its owner never has to learn that there is a choice.
func firstAdviser(chosen string, find func(string) string) string {
	if chosen != "" || codexProfileExists() || find("codex") != "" || find("claude") == "" {
		return chosen
	}
	return "claude"
}

func (a *app) poll() {
	a.mu.Lock()
	dir, saved, session, retry, denied := a.wowDir, a.settings.WowDir, a.sess, a.retryVolume, a.denied
	chosen := a.settings.Provider
	a.retryVolume = false
	a.mu.Unlock()
	// Claude Code installed while the app waits with no adviser is picked up the same way as at the start.
	if first := firstAdviser(chosen, findInstalled); first != chosen {
		a.setProvider(first)
	}
	if session != nil && session.accessDenied() {
		denied = true
		a.volumeRefused(os.ErrPermission)
	}
	if denied {
		// Only the process list is read here, never the volume: one attempt per launch, not one per tick.
		playing := runningWowDir(firstOf(dir, saved), processPaths()) != ""
		a.mu.Lock()
		relaunched := playing && a.deniedWatched && !a.deniedPlaying
		a.deniedWatched, a.deniedPlaying = true, playing
		a.mu.Unlock()
		if relaunched {
			logf("World of Warcraft started again; trying its folder once more")
			retry = true
		}
		if !retry {
			a.publish()
			return
		}
		a.stopSession()
		a.mu.Lock()
		a.denied, a.running, a.restart, a.wowDir, a.problem, a.deniedWatched = false, false, false, "", "", false
		a.mu.Unlock()
		dir = ""
	}
	if dir == "" {
		candidate := saved
		if candidate == "" {
			candidate = runningWowDir("", processPaths())
		}
		if candidate != "" {
			// Probe a real file once, so a refused read is not interpreted as an
			// absent volume by the path search and repeated at every tick.
			_, err := os.ReadFile(filepath.Join(candidate, ".flavor.info"))
			if a.volumeRefused(err) {
				a.publish()
				return
			}
		}
		if dir = resolveWowDir(candidate); dir == "" {
			a.publish()
			return
		}
		logf("WoW folder: %s", dir)
		a.mu.Lock()
		a.wowDir = dir
		a.mu.Unlock()
		if !wowRunning(dir) {
			a.install(dir)
		}
	}
	running := wowRunning(dir)
	a.mu.Lock()
	was := a.running
	a.running = running
	a.mu.Unlock()
	switch {
	case running && !was:
		logf("World of Warcraft started")
		checked := time.Now()
		plan, err := checkInstallation(dir)
		logf("Checked the installed addon and %d channels in %s", slotCount+bitCount, time.Since(checked).Round(time.Millisecond))
		if err != nil {
			if !a.volumeRefused(err) {
				a.mu.Lock()
				a.problem = tr("Couldn't check the add-on: %s", oneLine(err.Error()))
				a.running = false // Try the check again on the next poll.
				a.mu.Unlock()
			}
			a.publish()
			return
		}
		if plan.needed() {
			logf("The addon, its payload file, or its slot and bit addons are missing or outdated; restart WoW to finish setup")
			a.mu.Lock()
			a.restart = true
			a.mu.Unlock()
			a.writeAppPayload(dir, "restart", restartNote)
		} else {
			a.startSession(dir)
		}
	case !running && was:
		logf("World of Warcraft exited")
		a.stopSession()
		if session != nil && session.accessDenied() {
			a.volumeRefused(os.ErrPermission)
			a.publish()
			return
		}
		a.mu.Lock()
		a.restart = false
		a.mu.Unlock()
		a.install(dir)
		a.mu.Lock()
		quitWithWoW := a.settings.RunWithWoW && !a.denied
		a.mu.Unlock()
		if quitWithWoW {
			logf("Quitting because Run with WoW is on")
			a.installUpdate(false)
			a.exit()
		}
	}
	a.publish()
}

func (a *app) install(dir string) {
	done, err := install(dir)
	if a.volumeRefused(err) {
		return
	}
	for _, d := range done {
		logf("Setup: %s", d)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.problem = ""
	if err != nil {
		logf("Setup failed: %v", err)
		a.problem = "Setup failed: " + err.Error()
	}
}

// writeAppPayload writes the payload while no session runs. Only an installed addon reads the payload, so the app
// does not create a missing file.
func (a *app) writeAppPayload(dir, codex, note string) {
	path := payloadPath(dir)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		a.volumeRefused(err)
		return
	}
	w := &payloadWriter{path: path}
	if _, err := w.write(payload{V: protocolVersion, Helper: version, Codex: codex, Note: note}, time.Now()); err != nil {
		if a.volumeRefused(err) {
			return
		}
		logf("Couldn't write the %s payload: %v", codex, err)
		a.mu.Lock()
		a.problem = "Couldn't write the payload: " + oneLine(err.Error())
		a.mu.Unlock()
	}
}

func (a *app) startSession(dir string) {
	ctx, cancel := context.WithCancel(a.ctx)
	s := newSession(dir, a.origins, a.publish)
	s.projectDir = gameProjectDir()
	a.mu.Lock()
	s.provider = a.settings.Provider
	a.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()
	a.mu.Lock()
	a.sess, a.stopSess = s, func() { cancel(); <-done }
	a.mu.Unlock()
}

func (a *app) stopSession() {
	a.mu.Lock()
	stop := a.stopSess
	a.sess, a.stopSess = nil, nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (a *app) view() trayView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := trayView{update: a.updateVer, updating: a.updateStage, runWithWoW: a.settings.RunWithWoW, detail: a.problem, check: a.checking,
		provider: a.settings.Provider, installName: "Codex CLI", auctions: a.auctions}
	if v.provider == "claude" {
		v.installName = "Claude Code"
	}
	switch {
	case a.denied || (a.sess != nil && a.sess.accessDenied()):
		v.status, v.detail, v.alert, v.retryAccess = tr("No access to the WoW folder"), tr("Check the disk permission and choose “Retry access”."), true, true
	case a.wowDir == "":
		v.status, v.choose, v.alert = tr("Choose the WoW Forever folder"), true, true
	case a.restart:
		v.status, v.alert = tr("Restart WoW to load the add-on"), true
	case a.sess == nil:
		v.status = tr("Waiting for WoW Forever to start")
	default:
		sv := a.sess.view()
		switch {
		case sv.provider == "claude" && sv.claude:
			if sv.hello.IsZero() {
				v.status = tr("Waiting for a character to enter the game")
			} else {
				v.status = tr("Connected to the game")
			}
		case sv.provider == "claude" && sv.claudeChecked && !sv.claudeInstalled:
			v.status, v.install, v.alert = tr("%s was not found", v.installName), true, true
		case sv.provider == "claude" && sv.claudeChecked:
			v.status, v.alert, v.signIn = tr("Claude Code is not signed in"), true, true
		case sv.provider == "claude":
			v.status = tr("Waiting for a character to enter the game")
		case sv.noAdviser:
			v.status, v.install, v.installOther, v.alert = tr("No adviser was found"), true, true, true
		case sv.codex == "missing":
			v.status, v.install, v.alert = tr("%s was not found", "Codex CLI"), true, true
		case sv.codex == "signed-out":
			v.status, v.alert, v.signIn = tr("Codex is not signed in to ChatGPT"), true, true
		case sv.hello.IsZero():
			v.status = tr("Waiting for a character to enter the game")
		default:
			v.status = tr("Connected to the game")
		}
		v.detail = firstOf(a.problem, sv.problem)
		if v.detail == "" && !sv.hello.IsZero() {
			v.detail = tr("Chats: %d", sv.threads)
		}
	}
	return v
}

func (a *app) publish() {
	v := a.view()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.show != nil && v != a.shown {
		a.shown = v
		a.show(v)
	}
}

// setProvider chooses the agent that answers in the game. A running game session switches at once.
func (a *app) setProvider(name string) {
	a.mu.Lock()
	was := a.settings.Provider
	a.settings.Provider = name
	s, sess := a.settings, a.sess
	a.mu.Unlock()
	if err := saveSettings(s); err != nil {
		logf("Couldn't save settings: %v", err)
		a.mu.Lock()
		a.settings.Provider = was
		a.problem = "Couldn't save the adviser choice: " + oneLine(err.Error())
		a.mu.Unlock()
	} else {
		logf("The adviser is now %s", firstOf(name, "codex"))
		if sess != nil {
			sess.setProvider(name)
		}
	}
	a.publish()
}

// signIn opens the ChatGPT sign-in page through the running Codex.
func (a *app) signIn() {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess != nil {
		sess.signIn()
	}
}

func (a *app) setRunWithWoW(on bool) {
	a.mu.Lock()
	a.settings.RunWithWoW = on
	s := a.settings
	a.mu.Unlock()
	if err := saveSettings(s); err != nil {
		logf("Couldn't save settings: %v", err)
		a.mu.Lock()
		a.settings.RunWithWoW = !on
		a.problem = "Couldn't save Run with WoW: " + oneLine(err.Error())
		a.mu.Unlock()
		a.publish()
		return
	}
	if on {
		a.startWatcher()
	} else if err := setAutostart(false, ""); err != nil {
		logf("Couldn't remove the login item: %v", err)
		a.mu.Lock()
		a.problem = "Couldn't remove the login item: " + oneLine(err.Error())
		a.mu.Unlock()
	} else {
		logf("Run with WoW is off")
	}
	a.publish()
}

func (a *app) startWatcher() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil {
		err = setAutostart(true, exe)
	}
	if err != nil {
		logf("Couldn't turn on Run with WoW: %v", err)
		a.mu.Lock()
		a.settings.RunWithWoW = false
		a.problem = "Couldn't turn on Run with WoW: " + oneLine(err.Error())
		s := a.settings
		a.mu.Unlock()
		if err := saveSettings(s); err != nil {
			logf("Couldn't save settings: %v", err)
		}
		return
	}
	cmd := exec.Command(exe, "--watch")
	if err := startDetached(cmd); err != nil {
		logf("Couldn't start the watcher: %v", err)
		a.mu.Lock()
		a.problem = "Couldn't start Run with WoW: " + oneLine(err.Error())
		a.mu.Unlock()
		return
	}
	go cmd.Wait()
	logf("Run with WoW is on: %s --watch starts at login", exe)
}

func (a *app) chooseFolder() {
	path, err := chooseFolder()
	if err != nil {
		logf("No folder chosen: %v", err)
		return
	}
	dir := normalizeChoice(path)
	if dir == "" {
		logf("%s isn't a World of Warcraft Forever folder", path)
		a.mu.Lock()
		a.problem = "That folder isn't World of Warcraft Forever."
		a.mu.Unlock()
		a.publish()
		return
	}
	logf("Player chose %s", dir)
	a.stopSession()
	a.leaveRestart()
	a.mu.Lock()
	a.settings.WowDir, a.wowDir, a.running, a.restart, a.problem, a.denied = dir, "", false, false, "", false
	s := a.settings
	a.mu.Unlock()
	if err := saveSettings(s); err != nil {
		logf("Couldn't save settings: %v", err)
	}
	select {
	case a.rescan <- struct{}{}:
	default:
	}
}

// Do not touch the tray after this. On macOS a menu call then blocks, because the main thread stopped.
func (a *app) requestQuit() {
	a.mu.Lock()
	a.show = nil
	a.mu.Unlock()
	systray.Quit()
}

func (a *app) shutdown() {
	logf("Quitting")
	a.cancel()
	<-a.loopDone
	a.stopSession()
	a.leaveRestart()
}

// leaveRestart replaces the restart notice when the app stops watching a running WoW. A WoW restart without the app
// does not update the addon.
func (a *app) leaveRestart() {
	a.mu.Lock()
	restart, dir := a.restart && !a.denied, a.wowDir
	a.mu.Unlock()
	if restart {
		a.writeAppPayload(dir, "offline", offlineNote)
	}
}
