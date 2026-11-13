package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type approvalInput struct{ bytes.Buffer }

func (*approvalInput) Close() error { return nil }

func TestStoppedSessionDoesNotSendMoreCodexCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := &approvalInput{}
	c := &codexClient{ctx: ctx, agentProcess: agentProcess{stdin: input}}
	if err := c.call("thread/list", nil, nil); !errors.Is(err, context.Canceled) || input.Len() != 0 {
		t.Fatalf("cancelled session still sent a request: %v, %s", err, input.Bytes())
	}
}

func TestApprovalMustMatchTheThread(t *testing.T) {
	input := &approvalInput{}
	c := &codexClient{agentProcess: agentProcess{stdin: input}}
	s := newSession("", map[string]string{}, func() {})
	s.approvals = []*approval{{id: "a1", thread: "t1", rpcID: json.RawMessage(`1`), client: c}}
	s.answer(packet{Thread: "t2", Approval: "a1"}, true)
	if input.Len() != 0 || len(s.approvals) != 1 {
		t.Fatal("a packet from another thread answered the approval")
	}
	s.answer(packet{Thread: "t1", Approval: "a1"}, false)
	if len(s.approvals) != 0 || !bytes.Contains(input.Bytes(), []byte(`"decision":"decline"`)) {
		t.Fatalf("matching denial failed: %s", input.Bytes())
	}
}

func TestTrayStatus(t *testing.T) {
	s := newSession("/wow", nil, func() {})
	cases := []struct {
		name   string
		setup  func(*app)
		status string
		detail string
		alert  bool
	}{
		{"no folder", func(a *app) {}, "Выбери папку WoW Forever", "", true},
		{"closed", func(a *app) { a.wowDir = "/wow" }, "Ожидаю запуска WoW Forever", "", false},
		{"setup failed", func(a *app) { a.wowDir, a.problem = "/wow", "Setup failed: disk full" }, "Ожидаю запуска WoW Forever", "Setup failed: disk full", false},
		{"restart", func(a *app) { a.wowDir, a.running, a.restart = "/wow", true, true }, "Перезапусти WoW, чтобы загрузить аддон", "", true},
		{"no CLI", func(a *app) { a.wowDir, a.running, a.sess, s.codex = "/wow", true, s, "missing" }, "Не удалось найти Codex CLI", "", true},
		{"signed out", func(a *app) {
			a.wowDir, a.running, a.sess, s.codex, s.claudeInstalled = "/wow", true, s, "signed-out", false
		}, "Войди в ChatGPT через Codex CLI", "", true},
		{"no addon", func(a *app) { a.wowDir, a.running, a.sess, s.codex = "/wow", true, s, "ready" }, "Жду входа персонажа в игру", "", false},
		{"connected", func(a *app) {
			a.wowDir, a.running, a.sess = "/wow", true, s
			s.codex, s.hello, s.order = "ready", time.Now().Add(-12*time.Second), []string{"a", "b"}
		}, "Связь с игрой установлена", "Чатов: 2", false},
		{"write error", func(a *app) {
			a.wowDir, a.running, a.sess = "/wow", true, s
			s.writeErr = "Couldn't write the payload: denied"
		}, "Связь с игрой установлена", "Couldn't write the payload: denied", false},
	}
	for _, c := range cases {
		a := &app{}
		c.setup(a)
		v := a.view()
		if v.status != c.status || v.detail != c.detail || v.alert != c.alert || v.choose != (c.name == "no folder") || v.install != (c.name == "no CLI") {
			t.Errorf("%s: got %q / %q / alert %v / choose %v / install %v", c.name, v.status, v.detail, v.alert, v.choose, v.install)
		}
	}
}

// The app tells the addon in a running WoW to restart when that addon is out of date, and takes the notice back when
// it installs the addon or quits first.
func TestOutdatedAddonInRunningWoWAsksForARestart(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("the app finds WoW's process only on macOS and Windows")
	}
	wow := t.TempDir()
	if _, err := install(wow); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(addOnsDir(wow), "looking-for-guidance", "Core.lua"), []byte("older"), 0o600)
	t.Setenv("LOOKING_FOR_GUIDANCE_TEST_MODE", "child")
	game := exec.Command(copyTestProcess(t, wow, "WowB.exe"))
	if err := game.Start(); err != nil {
		t.Fatal(err)
	}
	quitGame := func() {
		game.Process.Kill()
		game.Wait()
	}
	defer quitGame()
	newApp := func() *app {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		close(done)
		return &app{ctx: ctx, cancel: cancel, loopDone: done, wowDir: wow, exit: func() {}}
	}

	a := newApp()
	a.poll()
	if p := readPayloadFile(t, payloadPath(wow)); a.view().status != "Перезапусти WoW, чтобы загрузить аддон" || p.V != protocolVersion || p.Codex != "restart" || p.Note != restartNote {
		t.Fatalf("WoW started with an outdated addon: menu %q, payload %+v", a.view().status, p)
	}
	a.shutdown()
	if p := readPayloadFile(t, payloadPath(wow)); p.Codex != "offline" || p.Note != offlineNote {
		t.Fatalf("the app quit and left the restart notice: %+v", p)
	}

	a = newApp()
	a.poll()
	if p := readPayloadFile(t, payloadPath(wow)); p.Codex != "restart" {
		t.Fatalf("a second start missed the outdated addon: %+v", p)
	}
	quitGame()
	a.poll()
	if p := readPayloadFile(t, payloadPath(wow)); installNeeded(wow) || p.Codex != "offline" || p.Note != offlineNote {
		t.Fatalf("WoW exited: install needed %v, payload %+v", installNeeded(wow), p)
	}
}

func TestQuitWithWoWFollowsTheCurrentOption(t *testing.T) {
	for _, on := range []bool{true, false} {
		for _, fromWatch := range []bool{true, false} {
			stopped, exited := false, false
			dir := t.TempDir()
			os.MkdirAll(filepath.Join(dir, "Interface"), 0o700)
			a := &app{
				wowDir: dir, running: true, fromWatch: fromWatch, settings: settings{RunWithWoW: on},
				stopSess: func() { stopped = true }, exit: func() { exited = true },
			}
			a.poll()
			if !stopped || exited != on {
				t.Fatalf("Run with WoW=%v, opened automatically=%v: stopped=%v, quit=%v", on, fromWatch, stopped, exited)
			}
		}
	}
}

func copyTestProcess(t *testing.T, dir, name string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
