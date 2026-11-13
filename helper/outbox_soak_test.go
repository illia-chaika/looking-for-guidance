package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Writes AddOns.txt the way the game does, one frame per interval, and reads it with the app's own watcher.
// FOREVER_FRAMES names a folder on the disk to measure, for example one on the game's disk; the test leaves nothing
// there. FOREVER_FRAME_MS changes the 40 ms spacing and FOREVER_FRAME_COUNT the number of frames.
// FOREVER_FRAME_SKIP leaves three frames in a row unwritten once in that many frames, which the check frames must
// make up for; a block is 54 frames long and survives one such gap, so smaller numbers lose packets.
// FOREVER_FRAME_PAYLOAD_MS also rewrites a payload of the usual size beside it that often, as the app does while it
// answers.
func TestFrameSoak(t *testing.T) {
	base := os.Getenv("FOREVER_FRAMES")
	if base == "" {
		t.Skip("set FOREVER_FRAMES to a folder on the disk to measure")
	}
	spacing, count := 40*time.Millisecond, 1500
	if ms, err := strconv.Atoi(os.Getenv("FOREVER_FRAME_MS")); err == nil && ms > 0 {
		spacing = time.Duration(ms) * time.Millisecond
	}
	if n, err := strconv.Atoi(os.Getenv("FOREVER_FRAME_COUNT")); err == nil && n > 0 {
		count = n
	}
	root, err := os.MkdirTemp(base, "forever-frame-soak-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "Account")
	path := filepath.Join(dir, "1", "Realm", "Character", "AddOns.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var slots strings.Builder
	for n := 1; n <= 64; n++ {
		fmt.Fprintf(&slots, "looking-for-guidance-slot-%03d: enabled\r\n", n)
	}
	write := func(frame [frameBytes]byte) time.Duration {
		start := time.Now()
		if err := os.WriteFile(path, []byte(addOnsText(frame)+slots.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}
	write([frameBytes]byte{})

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	o := newOutbox(dir)
	o.watchDir, o.unwatchDir = w.Add, w.Remove
	ctx, cancel := context.WithCancel(context.Background())
	out, done := make(chan packet, 1024), make(chan error, 1)
	go func() { done <- o.watch(ctx, w.Events, w.Errors, out) }()
	time.Sleep(20 * settleFor)

	var frames [][frameBytes]byte
	sent, counter := 0, byte(0)
	for len(frames) < count {
		// About 200 frames, the size of a chat question with its character snapshot.
		var filler strings.Builder
		for block := 0; filler.Len() < 4000; block++ {
			fmt.Fprintf(&filler, "%x", sha256.Sum256([]byte(fmt.Sprint(sent, block))))
		}
		sent++
		packet := encodeFrames(fmt.Sprintf(`{"type":"read","session":"soak","seq":%d,"at":1790000000,"text":"%s"}`, sent, filler.String()), counter)
		counter = packet[len(packet)-1][0] & frameCount
		frames = append(frames, packet...)
	}
	var payloads int
	var slowestPayload time.Duration
	stopPayload, payloadDone := make(chan struct{}), make(chan struct{})
	if ms, err := strconv.Atoi(os.Getenv("FOREVER_FRAME_PAYLOAD_MS")); err == nil && ms > 0 {
		go func() {
			defer close(payloadDone)
			body := []byte(strings.Repeat("payload ", 68<<10/8))
			for {
				select {
				case <-stopPayload:
					return
				case <-time.After(time.Duration(ms) * time.Millisecond):
				}
				start := time.Now()
				if err := writeFileAtomic(filepath.Join(root, "Payload.lua"), body); err != nil {
					t.Error(err)
					return
				}
				payloads, slowestPayload = payloads+1, max(slowestPayload, time.Since(start))
			}
		}()
	} else {
		close(payloadDone)
	}
	// The pause follows each write, so two frames are never closer than the spacing, however late the writer runs.
	var slowest, longest time.Duration
	last := time.Now()
	skip, _ := strconv.Atoi(os.Getenv("FOREVER_FRAME_SKIP"))
	for i, frame := range frames {
		if skip > 6 && i%skip >= skip/2 && i%skip < skip/2+3 {
			time.Sleep(spacing)
			continue
		}
		slowest = max(slowest, write(frame))
		longest, last = max(longest, time.Since(last)), time.Now()
		time.Sleep(spacing)
	}
	close(stopPayload)
	<-payloadDone
	time.Sleep(20 * settleFor)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	read, missed, restored, lost := 0, 0, 0, 0
	for _, f := range o.files {
		read, missed, restored, lost = read+f.frames.frames, missed+f.frames.missed, restored+f.frames.restored, lost+f.frames.lost
	}
	t.Logf("%s every %s: %d frames written, %d read; %d packets sent, %d received; %d frames missed, %d rebuilt, %d packets lost; slowest write %s, longest step %s",
		base, spacing, len(frames), read, sent, len(out), missed, restored, lost, slowest.Round(100*time.Microsecond), longest.Round(time.Millisecond))
	if payloads > 0 {
		t.Logf("%d payload writes beside it, slowest %s", payloads, slowestPayload.Round(100*time.Microsecond))
	}
	if len(out) != sent {
		t.Errorf("%d of %d packets did not arrive", sent-len(out), sent)
	}
}
