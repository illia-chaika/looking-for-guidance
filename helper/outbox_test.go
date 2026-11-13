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
	"math/rand"
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
	return encodeStream(z.Bytes(), counter)
}

// encodeStream frames a packet body the way the add-on does: blocks of data frames, each followed by check frames.
func encodeStream(body []byte, counter byte) [][frameBytes]byte {
	stream := append([]byte{byte(len(body) >> 8), byte(len(body))}, body...)
	var frames [][frameBytes]byte
	add := func(data [frameData]byte) {
		counter = (counter + 1) & frameCount
		f := [frameBytes]byte{counter}
		copy(f[1:], data[:])
		frames = append(frames, f)
	}
	for len(stream) > 0 {
		var sums [frameChecks][frameData]byte
		count := 0
		for ; count < frameBlock && len(stream) > 0; count++ {
			var data [frameData]byte
			stream = stream[copy(data[:], stream):]
			for i, b := range data {
				sums[count%frameChecks][i] ^= b
			}
			add(data)
		}
		for offset := count; offset < count+frameChecks; offset++ {
			add(sums[offset%frameChecks])
		}
	}
	frames[0][0] |= frameStart
	frames[len(frames)-1][0] |= frameEnd
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
	body := func(size int, salt byte) []byte {
		out := make([]byte, size)
		for i := range out {
			out[i] = byte(i*31+7) ^ salt
		}
		return out
	}
	feed := func(d *frameDecoder, frames [][frameBytes]byte) (got [][]byte) {
		for _, f := range frames {
			if data := d.add(f); data != nil {
				got = append(got, append([]byte(nil), data...))
			}
		}
		return got
	}
	without := func(frames [][frameBytes]byte, from, count int) [][frameBytes]byte {
		return append(append([][frameBytes]byte{}, frames[:from]...), frames[from+count:]...)
	}
	before := body(100, 1)
	lead := encodeStream(before, 20)
	after := func(frames [][frameBytes]byte) byte { return frames[len(frames)-1][0] & frameCount }

	// Any run of up to six missed frames is rebuilt, wherever it falls: in the first frames, across the data and
	// check frames of a block, across two blocks, in the last frames. Sizes cover one short block, exactly one
	// block, a block and one frame, and several blocks.
	for _, size := range []int{1, 13, 14, 88, 700, 717, 718, 719, 1400, 2200} {
		want := body(size, 2)
		frames := encodeStream(want, after(lead))
		for run := 0; run <= frameChecks; run++ {
			for from := 0; from+run <= len(frames); from++ {
				var d frameDecoder
				got := feed(&d, append(append([][frameBytes]byte{}, lead...), without(frames, from, run)...))
				if len(got) != 2 || !bytes.Equal(got[0], before) || !bytes.Equal(got[1], want) || d.lost != 0 {
					t.Fatalf("%d bytes (%d frames) without frames %d..%d: %d packets, %d lost", size, len(frames), from, from+run-1, len(got), d.lost)
				}
				if run > 0 && from+run < len(frames) && d.missed != run {
					t.Fatalf("%d bytes without frames %d..%d: counted %d missed", size, from, from+run-1, d.missed)
				}
				if run == 0 || from+run >= len(frames) {
					continue
				}
				// A second packet follows and is read too, whatever was missed in the one before.
				next := body(40, 3)
				if got := feed(&d, encodeStream(next, after(frames))); len(got) != 1 || !bytes.Equal(got[0], next) {
					t.Fatalf("%d bytes without frames %d..%d: the next packet was not read", size, from, from+run-1)
				}
			}
		}
	}

	// Seven in a row are too many for six check frames. The packet is lost and counted once; the next one is read.
	want := body(700, 4)
	frames := encodeStream(want, after(lead))
	var d frameDecoder
	if got := feed(&d, append(append([][frameBytes]byte{}, lead...), without(frames, 10, frameChecks+1)...)); len(got) != 1 {
		t.Fatalf("a packet with seven frames missed in a row was read: %d packets", len(got))
	}
	next := body(40, 5)
	if got := feed(&d, encodeStream(next, after(frames))); len(got) != 1 || !bytes.Equal(got[0], next) || d.lost != 1 {
		t.Fatalf("after a lost packet: %d packets, %d lost", len(got), d.lost)
	}

	// Two runs in different blocks are both rebuilt. Two frames of one group in the same block are not.
	frames = encodeStream(body(2200, 6), 0)
	d = frameDecoder{}
	// Frames 20..25 are six data frames of the first block; 100..104 are its neighbour's last two data frames and
	// three check frames, which nobody needs back.
	if got := feed(&d, without(without(frames, 100, 5), 20, 6)); len(got) != 1 || d.restored != 8 {
		t.Fatalf("two runs in different blocks: %d packets, %d frames rebuilt", len(got), d.restored)
	}
	d = frameDecoder{}
	if got := feed(&d, without(without(frames, 9, 1), 3, 1)); len(got) != 0 {
		t.Fatal("two missed frames of the same group were rebuilt from one check frame")
	}

	// A frame read twice counts once. What was on disk when the app started is an old packet's frame.
	d = frameDecoder{}
	var twice [][frameBytes]byte
	for _, f := range frames {
		twice = append(twice, f, f)
	}
	if got := feed(&d, twice); len(got) != 1 || d.frames != len(frames) {
		t.Fatalf("frames read twice: %d packets, %d frames counted", len(got), d.frames)
	}
	d = frameDecoder{}
	d.start(lead[len(lead)-1])
	if got := feed(&d, frames[2:]); len(got) != 0 {
		t.Fatal("read a packet whose first frames came before the app knew where packets begin")
	}
	if got := feed(&d, encodeStream(next, after(frames))); len(got) != 1 {
		t.Fatal("the first whole packet after the app started was not read")
	}

	// An interface reload starts the counter anywhere. The first frame of its packet says where it begins.
	d = frameDecoder{}
	feed(&d, lead)
	if got := feed(&d, encodeStream(next, after(lead)+17)); len(got) != 1 || !bytes.Equal(got[0], next) {
		t.Fatal("the packet after a reload was not read")
	}

	// Whatever is missed, a packet is either rebuilt exactly or not delivered.
	rnd := rand.New(rand.NewSource(1))
	for range 2000 {
		want := body(1+rnd.Intn(1800), byte(rnd.Intn(256)))
		frames := encodeStream(want, byte(rnd.Intn(64)))
		var kept [][frameBytes]byte
		for _, f := range frames {
			if rnd.Intn(100) >= 4 {
				kept = append(kept, f)
			}
		}
		d := frameDecoder{}
		for _, got := range feed(&d, kept) {
			if !bytes.Equal(got, want) {
				t.Fatalf("a damaged packet of %d bytes was delivered changed", len(want))
			}
		}
	}
}

func TestDecodePacketChecksNonsense(t *testing.T) {
	frames := encodeFrames(replyJSON, 0)
	var d frameDecoder
	var data []byte
	for _, f := range frames {
		if out := d.add(f); out != nil {
			data = out
		}
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
	var frames [][frameBytes]byte
	for lines.Scan() {
		raw, err := hex.DecodeString(strings.TrimSpace(lines.Text()))
		if err != nil || len(raw) != frameBytes {
			t.Fatalf("bad frame line %q", lines.Text())
		}
		frame, ok := parseFrame([]byte(addOnsText([frameBytes]byte(raw))))
		if !ok {
			t.Fatal("frame did not survive AddOns.txt")
		}
		frames = append(frames, frame)
	}
	if len(frames) <= frameBlock+frameChecks {
		t.Fatalf("the recorded packet should span more than one block, got %d frames", len(frames))
	}
	var wantJSON any
	json.Unmarshal([]byte(want), &wantJSON)
	// A packet before it tells the decoder where this one begins, as in the game after the first packet.
	lead := encodeStream([]byte("before"), 0)
	lead = encodeStream([]byte("before"), (frames[0][0]-1-byte(len(lead)))&frameCount)
	// The add-on's frames are read whole and with any six in a row missing.
	for run := 0; run <= frameChecks; run++ {
		for from := 0; from+run <= len(frames); from++ {
			var d frameDecoder
			for _, frame := range lead {
				d.add(frame)
			}
			var data []byte
			for i, frame := range frames {
				if i >= from && i < from+run {
					continue
				}
				if out := d.add(frame); out != nil {
					data = out
				}
			}
			if data == nil {
				t.Fatalf("the add-on's frames without %d..%d did not complete a packet", from, from+run-1)
			}
			r, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			var gotJSON any
			json.Unmarshal(body, &gotJSON)
			if !reflect.DeepEqual(gotJSON, wantJSON) {
				t.Fatalf("decoded %s\nwant %s", body, want)
			}
			if p, err := decodePacket(data); err != nil || p.Type == "" || p.Seq < 1 {
				t.Fatalf("decodePacket: %+v %v", p, err)
			}
		}
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
	var got []packet
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
		got = append(append(got, mustPoll(t, o, now)...), mustPoll(t, o, now.Add(settleFor))...)
	}
	// The packet is whole before its last check frame, so the frames read tell whether the final one was seen.
	if read := o.files[path].frames.frames; read != len(frames)-1 {
		t.Fatalf("read %d of %d frames before rescanning the missed final frame", read, len(frames))
	}
	now = now.Add(rescanEvery)
	if err := o.scan(now); err != nil {
		t.Fatal(err)
	}
	got = append(append(got, mustPoll(t, o, now)...), mustPoll(t, o, now.Add(settleFor))...)
	if read := o.files[path].frames.frames; read != len(frames) {
		t.Fatalf("the rescan did not read the missed final frame: %d of %d", read, len(frames))
	}
	if len(got) != 1 || got[0].Seq != 7 {
		t.Fatalf("the packet did not arrive exactly once: %+v", got)
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
			counter = f[0] & frameCount
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

// A rescan walks the game's folders and reads nothing meanwhile. Beside a game that rewrites AddOns.txt every 40 ms
// the walk took 126 ms and cost the arriving packet two or three frames, so it waits until the packet is in.
func TestOutboxDoesNotRescanWhileAPacketArrives(t *testing.T) {
	old := rescanEvery
	rescanEvery = 15 * time.Millisecond
	t.Cleanup(func() { rescanEvery = old })
	dir := t.TempDir()
	path := filepath.Join(dir, "AddOns.txt")
	os.WriteFile(path, []byte(addOnsText([frameBytes]byte{})), 0o600)
	o := newOutbox(dir)
	events, errs, out := make(chan fsnotify.Event), make(chan error), make(chan packet, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- o.watch(ctx, events, errs, out) }()
	time.Sleep(10 * settleFor)
	for _, frame := range encodeFrames(replyJSON, 0) {
		if err := os.WriteFile(path, []byte(addOnsText(frame)), 0o600); err != nil {
			t.Fatal(err)
		}
		events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
		time.Sleep(40 * time.Millisecond)
	}
	select {
	case p := <-out:
		if p.Type != "reply" || p.Seq != 7 {
			t.Fatalf("wrong packet: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the packet did not arrive")
	}
	// The postponed rescan runs once the file has been quiet for a while.
	time.Sleep(activeFor + 20*settleFor)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if o.busyScans != 0 {
		t.Fatalf("%d rescans ran while the packet was arriving", o.busyScans)
	}
	if !o.scanned || o.receiving(time.Now()) {
		t.Fatal("the watcher did not go back to rescanning after the packet")
	}
}
