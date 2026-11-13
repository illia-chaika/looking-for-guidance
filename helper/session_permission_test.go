package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestWatchDenialCancelsSessionAndSuppressesOfflineWrite(t *testing.T) {
	for _, cause := range []error{syscall.EPERM, syscall.EACCES} {
		t.Run(cause.Error(), func(t *testing.T) {
			var notifications int
			s := newSession("", nil, func() { notifications++ })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.cancelOnDenial = cancel
			s.writer = &payloadWriter{path: filepath.Join(t.TempDir(), "Payload.lua")}
			err := fmt.Errorf("watch game files: %w", &os.PathError{Op: "open", Path: "WTF", Err: cause})
			if !s.stopOnDenial(err) || !s.accessDenied() || ctx.Err() == nil {
				t.Fatal("denied read did not latch and cancel the session")
			}
			s.write(time.Now())
			s.stopOnDenial(err)
			s.shutdown()
			if notifications != 1 {
				t.Fatalf("one denial produced %d notifications", notifications)
			}
			if _, err := os.Stat(s.writer.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrote to the volume after read access was refused: %v", err)
			}
		})
	}
}

func TestDeniedWriterResumesOnlyInNewSession(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX directory permissions for an unprivileged user")
	}
	dir := t.TempDir()
	s := newSession("", nil, func() {})
	s.writer = &payloadWriter{path: filepath.Join(dir, "Payload.lua")}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })
	s.write(time.Now())
	if !s.accessDenied() {
		t.Fatal("a denied payload write did not latch")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s.ack(packet{Session: "game", Seq: 1})
	s.write(time.Now())
	s.shutdown()
	if _, err := os.Stat(s.writer.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retried the denied payload on a tick or shutdown: %v", err)
	}
	next := newSession("", nil, func() {})
	next.writer = &payloadWriter{path: s.writer.path}
	next.write(time.Now())
	if next.accessDenied() || next.writer.rev == 0 {
		t.Fatal("restarted session could not retry the newly granted path")
	}
}
