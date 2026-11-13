//go:build !windows

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutboxStopsAfterPermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read chmod-protected test files")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "AddOns.txt")
	if err := os.WriteFile(path, []byte("state"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("read", func(t *testing.T) {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0600) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		o := newOutbox(dir)
		if err := o.watch(ctx, nil, nil, make(chan packet)); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("outbox kept watching after a denied read: %v", err)
		}
	})
	t.Run("stat", func(t *testing.T) {
		o := newOutbox(dir)
		if err := o.scan(time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0700) })
		if _, err := o.poll(time.Now()); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("stat returned %v", err)
		}
	})
}
