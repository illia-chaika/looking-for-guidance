package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func mustPoll(t *testing.T, o *outbox, now time.Time) []packet {
	t.Helper()
	packets, err := o.poll(now)
	if err != nil {
		t.Fatal(err)
	}
	return packets
}

func encodeFrames(json string, counter byte) [][frameBytes]byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte(json))
	w.Close()
	data := append([]byte{byte(z.Len() >> 8), byte(z.Len())}, z.Bytes()...)
	var frames [][frameBytes]byte
	for first := true; len(data) > 0; first = false {
		counter = (counter + 1) & 0x7f
		var f [frameBytes]byte
		f[0] = counter
		if first {
			f[0] |= 0x80
		}
		data = data[copy(f[1:], data):]
		frames = append(frames, f)
	}
	return frames
}

func addOnsText(frame [frameBytes]byte) string {
	var b strings.Builder
	b.WriteString("Blizzard_Other: enabled\r\n")
	for n := 1; n <= bitCount; n++ {
		state := "disabled"
		if frame[(n-1)/8]&(0x80>>((n-1)%8)) != 0 {
			state = "enabled"
		}
		fmt.Fprintf(&b, "%s: %s\r\n", bitName(n), state)
	}
	b.WriteString("looking-for-guidance: enabled\r\n")
	return b.String()
}

const replyJSON = `{"type":"reply","session":"s1790000000","seq":7,"at":1790000000,"thread":"t1",` +
	`"text":"Where should I quest next? Anything worth buying with the three gold I have?",` +
	`"contextText":"WoW Forever 1.60.1.70009 · interface 16001\nCharacter: level 12 Night Elf Druid (race 4, class 11), Alliance"}`

func TestParseFrame(t *testing.T) {
	want := [frameBytes]byte{0x80, 0x01, 0xff, 0x00, 0x5a}
	got, ok := parseFrame([]byte(addOnsText(want)))
	if !ok || got != want {
		t.Fatalf("got %x %v", got, ok)
	}
	partial := strings.Replace(addOnsText(want), "looking-for-guidance-bit-077: disabled\r\n", "", 1)
	if _, ok := parseFrame([]byte(partial)); ok {
		t.Fatal("accepted a file missing a bit")
	}
}

func TestFrameDecoder(t *testing.T) {
	frames := encodeFrames(replyJSON, 126)
	if len(frames) < 4 {
		t.Fatalf("want a multi-frame packet, got %d frames", len(frames))
	}
	decode := func(d *frameDecoder, frames [][frameBytes]byte) (packet, bool) {
		for _, f := range frames {
			if data := d.add(f); data != nil {
				p, err := decodePacket(data)
				return p, err == nil
			}
		}
		return packet{}, false
	}

	var d frameDecoder
	d.start([frameBytes]byte{126})

	var twice [][frameBytes]byte
	for _, f := range frames {
		twice = append(twice, f, f)
	}
	p, ok := decode(&d, twice)
	if !ok || p.Type != "reply" || p.Session != "s1790000000" || p.Seq != 7 || p.Thread != "t1" || !strings.HasPrefix(p.ContextText, "WoW Forever") {
		t.Fatalf("decoded %+v %v", p, ok)
	}

	d = frameDecoder{}
	gap := append(append([][frameBytes]byte{}, frames[:2]...), frames[3:]...)
	if _, ok := decode(&d, gap); ok {
		t.Fatal("decoded a packet with a missing frame")
	}
	if d.lost != 1 {
		t.Fatalf("a packet lost to a missed frame was counted %d times", d.lost)
	}
	next := encodeFrames(strings.Replace(replyJSON, `"seq":7`, `"seq":8`, 1), frames[len(frames)-1][0]&0x7f)
	if p, ok := decode(&d, next); !ok || p.Seq != 8 {
		t.Fatalf("packet after a gap: %+v %v", p, ok)
	}

	d = frameDecoder{}
	if _, ok := decode(&d, frames[1:]); ok {
		t.Fatal("decoded a packet without its first frame")
	}

	d = frameDecoder{}
	decode(&d, frames)
	last := frames[len(frames)-1][0] & 0x7f
	reload := encodeFrames(strings.Replace(replyJSON, `"s1790000000"`, `"s1790000100"`, 1), (last-1)&0x7f)
	if reload[0][0]&0x7f != last {
		t.Fatal("test setup: the first frame should repeat the last counter")
	}
	if p, ok := decode(&d, reload); !ok || p.Session != "s1790000100" {
		t.Fatalf("packet after a reload: %+v %v", p, ok)
	}
}

func TestDecodePacketChecksNonsense(t *testing.T) {
	frames := encodeFrames(replyJSON, 0)
	var d frameDecoder
	var data []byte
	for _, f := range frames {
		data = d.add(f)
	}
	data[len(data)-1] ^= 0xff
	if _, err := decodePacket(data); err == nil {
		t.Fatal("accepted a bad checksum")
	}
	for _, bad := range []string{`{"type":"hello","session":"s1"}`, `{"type":"hello","seq":1}`, `{"session":"s1","seq":1}`, `{"type":"x","session":"s1","seq":1.5}`, `{"type":"hello","session":"s1","seq":1e40}`, `{"type":"hello","session":"s1","seq":1,"at":-1}`} {
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		w.Write([]byte(bad))
		w.Close()
		if _, err := decodePacket(z.Bytes()); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestOutboxWatchesAddOnsFiles(t *testing.T) {
	dir := t.TempDir()
	account := filepath.Join(dir, "ACCOUNT")
	character := filepath.Join(account, "Realm", "Char")
	os.MkdirAll(character, 0o755)
	os.MkdirAll(filepath.Join(account, "SavedVariables"), 0o755)
	files := []string{filepath.Join(account, "AddOns.txt"), filepath.Join(character, "AddOns.txt")}

	o := newOutbox(dir)
	now := time.Unix(1790000000, 0)
	mtime := now
	save := func(text string, paths ...string) []packet {
		mtime = mtime.Add(40 * time.Millisecond)
		for _, p := range paths {
			os.WriteFile(p, []byte(text), 0o644)
			os.Chtimes(p, mtime, mtime)
			if f := o.files[p]; f != nil {
				f.pending = true
			}
		}
		if err := o.scan(now); err != nil {
			t.Fatal(err)
		}
		got := mustPoll(t, o, now)
		if now = now.Add(settleFor - time.Millisecond); len(mustPoll(t, o, now)) > 0 {
			t.Fatal("read a file before it settled")
		}
		now = now.Add(time.Millisecond)
		got = append(got, mustPoll(t, o, now)...)
		now = now.Add(40 * time.Millisecond)
		return got
	}

	if got := save(addOnsText([frameBytes]byte{40}), files...); len(got) != 0 {
		t.Fatalf("starting state read as a packet: %+v", got)
	}
	frames := encodeFrames(replyJSON, 40)
	var got []packet
	for i, f := range frames {
		text := addOnsText(f)
		if i == 1 {

			save(text[:len(text)/2], files...)
		}
		got = append(got, save(text, files...)...)
	}
	if len(got) != 1 || got[0].Seq != 7 || got[0].File != filepath.Join("ACCOUNT", "AddOns.txt") && got[0].File != filepath.Join("ACCOUNT", "Realm", "Char", "AddOns.txt") {
		t.Fatalf("got %+v", got)
	}

	if len(o.files) != 2 {
		t.Fatalf("watching %d files", len(o.files))
	}
	now = now.Add(3 * time.Second)
	var repeated []packet
	for _, frame := range encodeFrames(replyJSON, 80) {
		repeated = append(repeated, save(addOnsText(frame), files...)...)
	}
	if len(repeated) != 1 || !repeated[0].Replay || repeated[0].Seq != got[0].Seq {
		t.Fatalf("retried frames should trigger an acknowledgement only: %+v", repeated)
	}
}

func TestDeliveryRetriesAreAcknowledgedWithoutAnotherTurn(t *testing.T) {
	s := newSession("", nil, func() {})
	s.writer = &payloadWriter{path: filepath.Join(t.TempDir(), "Payload.lua")}
	raw := make(chan packet, 4)
	out := make(chan packet, 4)
	now := float64(time.Now().Unix())
	raw <- packet{Type: "reply", Session: "s1", Seq: 1, At: now, RunID: "previous-helper-run"}
	raw <- packet{Type: "reply", Session: "s1", Seq: 2, At: now - 3601, RunID: s.runID}
	raw <- packet{Type: "reply", Session: "s1", Seq: 3, At: now, RunID: s.runID, Replay: true}
	raw <- packet{Type: "reply", Session: "s1", Seq: 4, At: now, RunID: s.runID}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.deliver(ctx, raw, out)
		close(done)
	}()
	defer func() { cancel(); <-done }()
	select {
	case p := <-out:
		if p.Seq != 4 || !slices.Equal(readPayloadFile(t, s.writer.path).Acks["s1"], []int64{3, 4}) {
			t.Fatalf("incorrect forwarding or acknowledgements: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new prompt did not reach the agent")
	}
}

func TestRealEncoderFrames(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "frames.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	lines.Buffer(nil, 1<<20)
	lines.Scan()
	want, ok := strings.CutPrefix(lines.Text(), "# ")
	if !ok {
		t.Fatal("first line is not the expected JSON")
	}
	var d frameDecoder
	var data []byte
	for lines.Scan() {
		raw, err := hex.DecodeString(strings.TrimSpace(lines.Text()))
		if err != nil || len(raw) != frameBytes {
			t.Fatalf("bad frame line %q", lines.Text())
		}
		frame, ok := parseFrame([]byte(addOnsText([frameBytes]byte(raw))))
		if !ok {
			t.Fatal("frame did not survive AddOns.txt")
		}
		if out := d.add(frame); out != nil {
			data = out
		}
	}
	if data == nil {
		t.Fatal("frames did not complete a packet")
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	json.Unmarshal(body, &gotJSON)
	json.Unmarshal([]byte(want), &wantJSON)
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("decoded %s\nwant %s", body, want)
	}
	p, err := decodePacket(data)
	if err != nil || p.Type == "" || p.Seq < 1 {
		t.Fatalf("decodePacket: %+v %v", p, err)
	}
}

func TestOutboxRescanRecoversAMissedNotification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AddOns.txt")
	now := time.Now()
	o := newOutbox(dir)
	os.WriteFile(path, []byte(addOnsText([frameBytes]byte{})), 0o600)
	if err := o.scan(now); err != nil {
		t.Fatal(err)
	}
	mustPoll(t, o, now)
	mustPoll(t, o, now.Add(settleFor))
	frames := encodeFrames(replyJSON, 0)
	for i, frame := range frames {
		// Longer apart than the time a file is checked without a notification after its last frame.
		now = now.Add(2 * activeFor)
		if err := os.WriteFile(path, []byte(addOnsText(frame)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now, now); err != nil {
			t.Fatal(err)
		}
		if i < len(frames)-1 {
			o.files[path].pending = true
		}
		if len(mustPoll(t, o, now))+len(mustPoll(t, o, now.Add(settleFor))) != 0 {
			t.Fatal("received a packet before rescanning the missed final frame")
		}
	}
	now = now.Add(rescanEvery)
	if err := o.scan(now); err != nil {
		t.Fatal(err)
	}
	mustPoll(t, o, now)
	if got := mustPoll(t, o, now.Add(settleFor)); len(got) != 1 || got[0].Seq != 7 {
		t.Fatalf("rescan lost the packet: %+v", got)
	}
}

func TestOutboxRescansAfterAnOverflow(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errs, done, watched := make(chan error), make(chan error, 1), make(chan string, 16)
	o := newOutbox(dir)
	o.watchDir = func(path string) error { watched <- path; return nil }
	// With no events channel, the new folder is found only by the rescan that an overflow starts.
	go func() { done <- o.watch(ctx, nil, errs, make(chan packet)) }()
	overflow := func() {
		select {
		case errs <- fsnotify.ErrEventOverflow:
		case err := <-done:
			t.Fatalf("watcher stopped: %v", err)
		}
	}
	overflow()
	added := filepath.Join(dir, "Character")
	if err := os.Mkdir(added, 0o700); err != nil {
		t.Fatal(err)
	}
	overflow()
	for {
		select {
		case path := <-watched:
			if path == added {
				return
			}
		case err := <-done:
			t.Fatalf("watcher stopped: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("the overflow didn't rescan")
		}
	}
}

func TestFilesystemNotificationsDeliverFramesAndCancel(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		atomic, create, reset, gap bool
	}{
		{name: "in-place"},
		{name: "rename", atomic: true},
		{name: "replace-with-gap", gap: true},
		{name: "new-character", atomic: true, create: true},
		{name: "recreated-folder", reset: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "Account")
			os.MkdirAll(dir, 0o700)
			path := filepath.Join(dir, "AddOns.txt")
			os.WriteFile(path, []byte(addOnsText([frameBytes]byte{})), 0o600)
			s := newSession("", map[string]string{}, func() {})
			s.writer = &payloadWriter{path: filepath.Join(root, "Payload.lua")}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			raw, out := make(chan packet, 1), make(chan packet, 1)
			go func() { done <- watchOutbox(ctx, dir, raw) }()
			go s.deliver(ctx, raw, out)
			t.Cleanup(cancel)
			time.Sleep(10 * settleFor)
			if tc.reset {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			}
			if tc.create {
				path = filepath.Join(dir, "Realm", "Character", "AddOns.txt")
			}
			if tc.create || tc.reset {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(addOnsText([frameBytes]byte{})), 0o600); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * settleFor)
			}
			for _, frame := range encodeFrames(replyJSON, 0) {
				data := []byte(addOnsText(frame))
				var err error
				if tc.gap {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					time.Sleep(3 * settleFor)
				}
				if tc.atomic {
					err = writeFileAtomic(path, data)
				} else {
					err = os.WriteFile(path, data, 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(40 * time.Millisecond)
			}
			select {
			case p := <-out:
				if p.Type != "reply" || p.Seq != 7 || p.Thread != "t1" {
					t.Fatalf("wrong packet: %+v", p)
				}
				if got := readPayloadFile(t, s.writer.path); !slices.Contains(got.Acks[p.Session], 7) {
					t.Fatalf("packet wasn't acknowledged: %+v", got.Acks)
				}
			case err := <-done:
				t.Fatalf("watcher stopped: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("filesystem notifications lost the packet")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("watcher did not stop")
			}
		})
	}
}

// On a first run, AddOns.txt has no bit lines yet, or WoW writes it only after the app starts. The addon's first
// packet, its hello, is then new and the app must read it.
func TestOutboxReadsTheFirstPacketOfAFirstRun(t *testing.T) {
	dir := t.TempDir()
	before := filepath.Join(dir, "ACCOUNT", "AddOns.txt")
	after := filepath.Join(dir, "OTHER", "AddOns.txt")
	o := newOutbox(dir)
	now := time.Unix(1790000000, 0)
	read := func(path string, text string) []packet {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(text), 0o644)
		mtime := now.Add(-time.Second)
		os.Chtimes(path, mtime, mtime)
		if err := o.scan(now); err != nil {
			t.Fatal(err)
		}
		if f := o.files[path]; f != nil {
			f.pending = true
		}
		mustPoll(t, o, now)
		now = now.Add(settleFor)
		got := mustPoll(t, o, now)
		now = now.Add(40 * time.Millisecond)
		return got
	}
	if got := read(before, "Blizzard_Other: enabled\r\n"); len(got) != 0 {
		t.Fatalf("a file without bit lines read as a packet: %+v", got)
	}
	for i, path := range []string{before, after} {
		seq := i + 1
		var got []packet
		for _, f := range encodeFrames(strings.Replace(replyJSON, `"seq":7`, fmt.Sprintf(`"seq":%d`, seq), 1), 90) {
			got = append(got, read(path, addOnsText(f))...)
		}
		if len(got) != 1 || got[0].Seq != float64(seq) {
			t.Fatalf("%s: the first packet was not read: %+v", path, got)
		}
	}
}

// The add-on repeats a packet the helper did not confirm. A repeat of one that did arrive must not act twice.
func TestOutboxRecognisesAnAutomaticRepeat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ACCOUNT", "Realm", "Char", "AddOns.txt")
	os.MkdirAll(filepath.Dir(path), 0o755)
	o := newOutbox(dir)
	now := time.Unix(1790000000, 0)
	mtime := now
	counter := byte(40)
	send := func(json string) []packet {
		var got []packet
		for _, f := range encodeFrames(json, counter) {
			counter = f[0] & 0x7f
			mtime = mtime.Add(40 * time.Millisecond)
			os.WriteFile(path, []byte(addOnsText(f)), 0o644)
			os.Chtimes(path, mtime, mtime)
			if known := o.files[path]; known != nil {
				known.pending = true
			}
			if err := o.scan(now); err != nil {
				t.Fatal(err)
			}
			mustPoll(t, o, now)
			now = now.Add(settleFor)
			got = append(got, mustPoll(t, o, now)...)
			now = now.Add(40 * time.Millisecond)
		}
		return got
	}
	os.WriteFile(path, []byte(addOnsText([frameBytes]byte{40})), 0o644)
	if err := o.scan(now); err != nil {
		t.Fatal(err)
	}
	mustPoll(t, o, now)
	now = now.Add(settleFor)
	mustPoll(t, o, now)

	create := `{"type":"create","session":"s1","seq":3,"at":1790000000,"text":"Where next?"}`
	insight := `{"type":"insight","session":"s1","seq":4,"at":1790000000,"kind":"digest","key":"me"}`
	if got := send(create); len(got) != 1 || got[0].Replay {
		t.Fatalf("first create: %+v", got)
	}
	if got := send(insight); len(got) != 1 || got[0].Kind != "digest" {
		t.Fatalf("first insight: %+v", got)
	}
	now = now.Add(2 * time.Second)
	if got := send(create); len(got) != 1 || !got[0].Replay {
		t.Fatalf("a repeated question is confirmed again but marked as a repeat, so no second turn starts: %+v", got)
	}
	if got := send(insight); len(got) != 0 {
		t.Fatalf("a repeated journal request that already arrived is dropped: %+v", got)
	}
	// A repeat of a packet that was lost the first time is simply its first arrival.
	if got := send(`{"type":"insight","session":"s1","seq":5,"at":1790000000,"kind":"brief","key":"x"}`); len(got) != 1 || got[0].Replay {
		t.Fatalf("first arrival after a loss: %+v", got)
	}
}

// While a packet is arriving the watcher looks at the file on a timer as well: a notification that comes late or
// not at all would otherwise cost a frame, and one missed frame loses the packet.
func TestOutboxReadsAnArrivingPacketWithoutNotifications(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AddOns.txt")
	os.WriteFile(path, []byte(addOnsText([frameBytes]byte{})), 0o600)
	o := newOutbox(dir)
	events, errs, out := make(chan fsnotify.Event), make(chan error), make(chan packet, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.watch(ctx, events, errs, out) }()
	time.Sleep(10 * settleFor)
	for i, frame := range encodeFrames(replyJSON, 0) {
		if err := os.WriteFile(path, []byte(addOnsText(frame)), 0o600); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
		}
		time.Sleep(40 * time.Millisecond)
	}
	select {
	case p := <-out:
		if p.Type != "reply" || p.Seq != 7 {
			t.Fatalf("wrong packet: %+v", p)
		}
	case err := <-done:
		t.Fatalf("watcher stopped: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("the packet needed a notification for every frame")
	}
}
