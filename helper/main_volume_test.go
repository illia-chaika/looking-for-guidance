package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func requireVolumePermissionFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires an unprivileged user with POSIX file permissions")
	}
}

func TestVolumeRetryReopensFolderOnlyAfterExplicitAction(t *testing.T) {
	requireVolumePermissionFixture(t)
	wow := t.TempDir()
	flavor := filepath.Join(wow, ".flavor.info")
	if err := os.WriteFile(flavor, []byte("Product Flavor!STRING:0\nwowf\n"), 0000); err != nil {
		t.Fatal(err)
	}
	a := &app{settings: settings{WowDir: wow}, rescan: make(chan struct{}, 1)}
	a.poll()
	if v := a.view(); !v.alert || !v.retryAccess || v.choose || a.wowDir != "" {
		t.Fatalf("denied preflight did not offer an explicit retry: %+v", v)
	}
	a.requestVolumeRetry()
	<-a.rescan
	a.poll()
	if !a.denied || a.retryVolume {
		t.Fatal("a retry while access is still refused must return to the paused state")
	}
	if err := os.Chmod(flavor, 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		a.poll()
	}
	if !a.view().retryAccess || a.wowDir != "" {
		t.Fatal("background poll retried the volume when access changed")
	}
	if _, err := os.Stat(addOnsDir(wow)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected automatic install before Retry: %v", err)
	}
	a.requestVolumeRetry()
	a.requestVolumeRetry()
	if len(a.rescan) != 1 {
		t.Fatal("multiple clicks should wake the loop once")
	}
	<-a.rescan
	a.poll()
	if v := a.view(); v.retryAccess || v.alert || a.denied || a.wowDir != wow {
		t.Fatalf("granted access was not restored after an explicit retry: %+v", v)
	}
	if _, err := os.Stat(payloadPath(wow)); err != nil {
		t.Fatalf("retry never finished setting up the game folder: %v", err)
	}
}

func TestVolumeRefusalDuringSessionShutdownSkipsInstall(t *testing.T) {
	wow := t.TempDir()
	s := newSession(wow, nil, func() {})
	exited := false
	a := &app{
		wowDir: wow, running: true, sess: s, settings: settings{RunWithWoW: true},
		stopSess: func() { s.stopOnDenial(&os.PathError{Op: "rename", Path: payloadPath(wow), Err: syscall.EPERM}) },
		exit:     func() { exited = true },
	}
	a.poll()
	if v := a.view(); exited || a.sess != nil || !a.denied || !v.retryAccess {
		t.Fatalf("failed shutdown should pause the bridge and retain the retry button: %+v", v)
	}
	if _, err := os.Stat(addOnsDir(wow)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote setup files after offline payload was denied: %v", err)
	}
}

func TestVolumeRefusalDuringExitInstallKeepsRetryAvailable(t *testing.T) {
	requireVolumePermissionFixture(t)
	wow := t.TempDir()
	inter := filepath.Join(wow, "Interface")
	if err := os.Mkdir(inter, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(inter, 0700) })
	exited := false
	a := &app{wowDir: wow, running: true, settings: settings{RunWithWoW: true}, exit: func() { exited = true }}
	a.poll()
	if exited || !a.view().retryAccess {
		t.Fatal("automatic quit swallowed the request to retry access after a denied install")
	}
}

func TestVolumeRefusalReadingInstalledAddonDoesNotAskToRestartWoW(t *testing.T) {
	requireVolumePermissionFixture(t)
	if runtime.GOOS != "darwin" {
		t.Skip("drives the macOS process watcher")
	}
	wow := t.TempDir()
	if _, err := install(wow); err != nil {
		t.Fatal(err)
	}
	toc := filepath.Join(addOnsDir(wow), "looking-for-guidance", "looking-for-guidance.toc")
	if err := os.Chmod(toc, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(toc, 0600) })
	t.Setenv("LOOKING_FOR_GUIDANCE_TEST_MODE", "child")
	game := exec.Command(copyTestProcess(t, wow, "WowB.exe"))
	if err := game.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { game.Process.Kill(); game.Wait() })
	a := &app{wowDir: wow}
	a.poll()
	if v := a.view(); !v.retryAccess || a.restart || a.sess != nil {
		t.Fatalf("an unreadable installed addon should request access before starting a session: %+v", v)
	}
	if p := readPayloadFile(t, payloadPath(wow)); p.Codex != "offline" {
		t.Fatalf("left an incorrect restart notice on a volume with refused access: %+v", p)
	}
}

// A refusal at the moment the game closed once left the companion paused through the next session (20.11.2026).
func TestVolumeRefusalIsTriedOnceMoreWhenTheGameStartsAgain(t *testing.T) {
	requireVolumePermissionFixture(t)
	if runtime.GOOS != "darwin" {
		t.Skip("drives the macOS process watcher")
	}
	wow := t.TempDir()
	flavor := filepath.Join(wow, ".flavor.info")
	if err := os.WriteFile(flavor, []byte("Product Flavor!STRING:0\nwowf\n"), 0000); err != nil {
		t.Fatal(err)
	}
	a := &app{settings: settings{WowDir: wow}, rescan: make(chan struct{}, 1)}
	a.poll()
	a.poll()
	if !a.denied {
		t.Fatal("the unreadable folder is refused")
	}
	t.Setenv("LOOKING_FOR_GUIDANCE_TEST_MODE", "child")
	launch := func() *exec.Cmd {
		// A fresh file each time: macOS kills a process started from an executable rewritten in place.
		os.Remove(filepath.Join(wow, "WowB.exe"))
		game := exec.Command(copyTestProcess(t, wow, "WowB.exe"))
		if err := game.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { game.Process.Kill(); game.Wait() })
		return game
	}
	game := launch()
	a.poll()
	if !a.denied || !a.view().retryAccess {
		t.Fatal("a launch while access is still refused returns to the pause")
	}
	if err := os.Chmod(flavor, 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		a.poll()
	}
	if !a.denied || a.wowDir != "" {
		t.Fatal("one attempt per launch, not one per tick")
	}
	game.Process.Kill()
	game.Wait()
	a.poll()
	launch()
	a.poll()
	if a.denied || a.wowDir != wow {
		t.Fatalf("the next launch tries again and finds access granted: denied=%v dir=%q", a.denied, a.wowDir)
	}
}
