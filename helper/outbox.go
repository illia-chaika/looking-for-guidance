package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	settleFor = 8 * time.Millisecond
	// While frames are arriving the file is also checked on a timer: a late notification would cost a frame.
	activeFor      = time.Second
	frameBytes     = bitCount / 8
	maxPacketBytes = 1 << 20
)

// A missed notification is caught by reading every AddOns.txt again this often.
var rescanEvery = 30 * time.Second

type packet struct {
	Journal       *journalCard `json:"journal,omitempty"`
	RetrySnapshot string       `json:"retrySnapshot,omitempty"`
	Character     string       `json:"character,omitempty"`
	Login         string       `json:"login,omitempty"`
	MemoryAction  string       `json:"memoryAction,omitempty"`
	NoteID        string       `json:"noteId,omitempty"`
	NoteText      string       `json:"noteText,omitempty"`
	Revision      int          `json:"revision,omitempty"`
	Scope         string       `json:"scope,omitempty"`
	Type          string       `json:"type"`
	Session       string       `json:"session"`

	Seq         float64     `json:"seq"`
	At          float64     `json:"at"`
	Addon       string      `json:"addon"`
	Text        string      `json:"text"`
	Cwd         string      `json:"cwd"`
	Model       string      `json:"model"`
	Effort      string      `json:"effort"`
	Thread      string      `json:"thread"`
	Title       string      `json:"title"`
	Approval    string      `json:"approval"`
	ContextText string      `json:"contextText"`
	Plain       bool        `json:"plain"`
	RequestID   string      `json:"requestId,omitempty"`
	Cards       []gameEntry `json:"cards,omitempty"`
	// The add-on's verdict on auction upgrade candidates, as one JSON object.
	Upgrades json.RawMessage `json:"upgrades,omitempty"`
	Addons   json.RawMessage `json:"addons,omitempty"`

	// A journal card asks for researched advice: kind and key name it, facts describe the character.
	Kind       string          `json:"kind,omitempty"`
	Key        string          `json:"key,omitempty"`
	Locale     string          `json:"locale,omitempty"`
	GameLocale string          `json:"gameLocale,omitempty"`
	Force      bool            `json:"force,omitempty"`
	Cap        int             `json:"cap,omitempty"`
	Facts      json.RawMessage `json:"facts,omitempty"`
	// Where the character trades, as the add-on reads it from the client.
	Market *marketFacts `json:"market,omitempty"`

	RunID  string `json:"runId,omitempty"`
	File   string `json:"-"`
	Replay bool   `json:"-"`
	// The message as the agent receives it, when the app prepared it ahead of the turn.
	Prompt string `json:"-"`
}

type packetKey struct {
	session string
	seq     int64
}

type outbox struct {
	dir     string
	files   map[string]*addonList
	dirs    map[string]bool
	seen    map[packetKey]time.Time
	missed  int
	scanned bool
	// The longest time the watcher went without looking at the files since the last frame, and the last time it looked.
	pause  time.Duration
	looked time.Time
	// Rescans that ran while a packet was arriving. Walking the folders took 126 ms beside a game that was rewriting
	// AddOns.txt every 40 ms, and each such walk cost the packet two or three frames.
	busyScans  int
	watchDir   func(string) error
	unwatchDir func(string) error
}

type addonList struct {
	mod     time.Time
	size    int64
	changed time.Time
	pending bool
	based   bool
	frames  frameDecoder
	frameAt time.Time
}

func newOutbox(dir string) *outbox {
	return &outbox{dir: dir, files: map[string]*addonList{}, dirs: map[string]bool{}, seen: map[packetKey]time.Time{}}
}

func watchOutbox(ctx context.Context, dir string, out chan<- packet) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	o := newOutbox(dir)
	o.watchDir = w.Add
	o.unwatchDir = w.Remove
	return o.watch(ctx, w.Events, w.Errors, out)
}

func (o *outbox) watch(ctx context.Context, events <-chan fsnotify.Event, errs <-chan error, out chan<- packet) error {
	if err := o.scan(time.Now()); err != nil {
		return err
	}
	// A rescan blocks reading, so it waits until no packet is arriving. The timer reads an active file meanwhile.
	rescan := false
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	settle := time.NewTimer(settleFor)
	defer settle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if !ok {
				return errors.New("filesystem notifications stopped")
			}
			// An overflow means the system dropped notifications because its queue was full. A rescan reads every
			// AddOns.txt again.
			if !errors.Is(err, fsnotify.ErrEventOverflow) {
				return err
			}
			rescan = true
		case event, ok := <-events:
			if !ok {
				return errors.New("filesystem notifications stopped")
			}
			if !event.Has(fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename) {
				continue
			}
			if strings.EqualFold(filepath.Base(event.Name), "AddOns.txt") {
				if f := o.files[event.Name]; f != nil {
					f.pending = true
				} else {
					rescan = true
				}
			} else {
				changedDir := o.dirs[event.Name]
				if changedDir && event.Has(fsnotify.Remove|fsnotify.Rename) {
					o.unwatchDir(event.Name)
					delete(o.dirs, event.Name)
				}
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if errors.Is(err, fs.ErrPermission) {
						return err
					}
					changedDir = err == nil && info.IsDir()
				}
				if !changedDir {
					continue
				}
				rescan = true
			}
		case <-tick.C:
			rescan = true
		case <-settle.C:
		}
		now := time.Now()
		if rescan && !o.receiving(now) {
			rescan = false
			if err := o.scan(now); err != nil {
				return err
			}
			now = time.Now()
		}
		if !o.looked.IsZero() {
			o.pause = max(o.pause, now.Sub(o.looked))
		}
		o.looked = now
		packets, err := o.poll(now)
		if err != nil {
			return err
		}
		for _, p := range packets {
			select {
			case out <- p:
			case <-ctx.Done():
				return nil
			}
		}
		for _, f := range o.files {
			if f.pending || now.Sub(f.frameAt) < activeFor {
				settle.Reset(settleFor)
				break
			}
		}
	}
}

// receiving reports whether a frame arrived a moment ago, so more are probably on the way.
func (o *outbox) receiving(now time.Time) bool {
	for _, f := range o.files {
		if now.Sub(f.frameAt) < activeFor {
			return true
		}
	}
	return false
}

func (o *outbox) poll(now time.Time) ([]packet, error) {
	var out []packet
	for path, f := range o.files {
		if f.based && !f.pending && now.Sub(f.frameAt) >= activeFor {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil, err
			}
			if errors.Is(err, fs.ErrNotExist) {
				f.pending = false
			}
			continue
		}
		if !info.ModTime().Equal(f.mod) || info.Size() != f.size {
			f.mod, f.size, f.changed, f.pending = info.ModTime(), info.Size(), now, true
			continue
		}
		if f.pending && now.Sub(f.changed) >= settleFor {
			f.pending = false
			p, err := o.read(path, f, now)
			if errors.Is(err, fs.ErrPermission) {
				return nil, err
			}
			if p.Type != "" {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (o *outbox) scan(now time.Time) error {
	started := time.Now()
	if o.receiving(now) {
		o.busyScans++
	}
	found := map[string]bool{}
	dirs := map[string]bool{}
	watch := func(path string) error {
		dirs[path] = true
		if o.watchDir != nil && !o.dirs[path] {
			if err := o.watchDir(path); err != nil {
				return err
			}
			o.dirs[path] = true
		}
		return nil
	}
	if err := watch(filepath.Dir(o.dir)); err != nil {
		return err
	}
	err := filepath.WalkDir(o.dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		case d.IsDir() && strings.EqualFold(d.Name(), "SavedVariables"):
			return filepath.SkipDir
		case d.IsDir():
			return watch(path)
		case d.Type().IsRegular() && strings.EqualFold(d.Name(), "AddOns.txt"):
			found[path] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	for path := range o.dirs {
		if !dirs[path] {
			if o.unwatchDir != nil {
				o.unwatchDir(path)
			}
			delete(o.dirs, path)
		}
	}
	for path := range found {
		if o.files[path] == nil {
			o.files[path] = &addonList{based: o.scanned}
		}
		// Recheck known files too, because a filesystem notification can be lost.
		o.files[path].pending = true
	}
	for path := range o.files {
		if !found[path] {
			// A save can remove the file for a short time. Keep its partial packet until the character folder is
			// gone.
			info, err := os.Stat(filepath.Dir(path))
			if errors.Is(err, fs.ErrPermission) {
				return err
			}
			if err != nil || !info.IsDir() {
				delete(o.files, path)
			}
		}
	}
	o.scanned = true
	if took := time.Since(started); took > 20*time.Millisecond {
		logf("Looking through the game's settings folders took %d ms", took.Milliseconds())
	}
	frames, missed, restored, lost := 0, 0, 0, 0
	for _, f := range o.files {
		frames, missed, restored, lost = frames+f.frames.frames, missed+f.frames.missed, restored+f.frames.restored, lost+f.frames.lost
	}
	if missed != o.missed {
		logf("Game frames: %d read, %d missed, %d rebuilt, %d packets lost so far", frames, missed, restored, lost)
		o.missed = missed
	}
	for key, at := range o.seen {
		if now.Sub(at) > time.Hour {
			delete(o.seen, key)
		}
	}
	return nil
}

func (o *outbox) read(path string, f *addonList, now time.Time) (packet, error) {
	file, err := openShared(path)
	if err != nil {
		return packet{}, err
	}
	defer file.Close()
	text, err := io.ReadAll(io.LimitReader(file, maxPacketBytes+1))
	if err != nil {
		return packet{}, err
	}
	if len(text) > maxPacketBytes {
		return packet{}, nil
	}
	frame, ok := parseFrame(text)
	if !f.based {
		// The frame on disk when the app starts is an old packet's. A file without one, or one that appears later,
		// has no old packet, so its first frame is new.
		f.based = true
		if ok {
			f.frames.start(frame)
		}
		return packet{}, nil
	}
	if !ok {
		return packet{}, nil
	}
	before := f.frames
	data := f.frames.add(frame)
	name, _ := filepath.Rel(o.dir, path)
	if f.frames.frames != before.frames {
		// The numbers tell the causes apart: a long pause means this app was not running or was busy, a short one
		// that the frames were not in the file when it looked.
		if gap := f.frames.gap; gap > 0 {
			logf("Missed %d frames from %s: %d ms since the frame before, the watcher's longest pause in that time was %d ms",
				gap, name, now.Sub(f.frameAt).Milliseconds(), o.pause.Milliseconds())
		}
		f.frameAt, o.pause = now, 0
	}
	if f.frames.lost != before.lost {
		logf("Lost a packet from %s: more frames were missed than its check frames can rebuild", name)
	}
	if data == nil {
		return packet{}, nil
	}
	if rebuilt := f.frames.restored - before.restored; rebuilt > 0 {
		logf("Rebuilt %d missed frames; the packet from %s arrived whole", rebuilt, name)
	}
	p, err := decodePacket(data)
	if err != nil {
		logf("Dropped a packet from %s: %v", name, err)
		return packet{}, nil
	}

	key := packetKey{p.Session, int64(p.Seq)}
	if last, dup := o.seen[key]; dup {
		// A second AddOns.txt commonly carries the same frame at the same time.
		// A later UI retry needs a new acknowledgement but never another turn.
		if now.Sub(last) < time.Second || (p.Type != "create" && p.Type != "reply") {
			return packet{}, nil
		}
		p.Replay = true
	}
	o.seen[key] = now
	p.File = name
	return p, nil
}

func parseFrame(text []byte) ([frameBytes]byte, bool) {
	var frame [frameBytes]byte
	var seen [bitCount]bool
	count := 0
	for line := range strings.SplitSeq(string(text), "\n") {
		name, state, ok := strings.Cut(strings.TrimSpace(line), ":")
		digits, isBit := strings.CutPrefix(name, "looking-for-guidance-bit-")
		n, err := strconv.Atoi(digits)
		if !ok || !isBit || len(digits) != 3 || err != nil || n < 1 || n > bitCount {
			continue
		}
		switch strings.TrimSpace(state) {
		case "enabled":
			frame[(n-1)/8] |= 0x80 >> ((n - 1) % 8)
		case "disabled":
		default:
			continue
		}
		if !seen[n-1] {
			seen[n-1] = true
			count++
		}
	}
	return frame, count == bitCount
}

// The add-on sends a packet as 16-byte frames: a head byte and 15 bytes of the stream (a two-byte length and the
// compressed packet). After every frameBlock data frames come frameChecks check frames, and after the last, shorter
// block too. Within a block, the frames whose offsets are equal modulo frameChecks XOR to zero, so any one frame is
// the XOR of the others in its group: a run of up to frameChecks missed frames is rebuilt. The game writes a frame
// every 40 ms, and this app has been seen away from the file for 214 ms beside a busy game.
const (
	frameBlock  = 48
	frameChecks = 6
	frameData   = frameBytes - 1
	frameStart  = 0x80 // head bit: the packet's first frame
	frameEnd    = 0x40 // head bit: the packet's last frame
	frameCount  = 0x3f // head bits: a counter that goes up by one with every frame, across packets
	maxFrames   = 6000 // more than the longest packet needs
)

type frameDecoder struct {
	last    [frameBytes]byte
	started bool
	// The packet being received. got holds each frame's data by position, nil where a frame was missed.
	synced bool // positions are known: the first frame was seen, or the end of the packet before
	done   bool // the packet was handed over already; the rest of its frames pass by
	pos    int  // position of the latest frame
	total  int  // the packet's frame count once it is known
	got    [][]byte
	// frames were read, missed did not arrive, restored were rebuilt, lost packets could not be; run is the longest
	// row of missed frames and gap the number missed just before the latest frame.
	frames, missed, restored, lost, run, gap int
}

func (d *frameDecoder) start(frame [frameBytes]byte) {
	d.last, d.started = frame, true
}

// add takes the next frame read from AddOns.txt and returns a packet's compressed bytes when one is complete.
func (d *frameDecoder) add(frame [frameBytes]byte) []byte {
	if d.started && frame == d.last {
		return nil
	}
	step := int((frame[0] - d.last[0]) & frameCount)
	known := d.started
	d.last, d.started = frame, true
	d.frames++
	d.gap = 0
	switch {
	case frame[0]&frameStart != 0:
		d.close()
		d.synced, d.pos = true, 0
	case !d.synced || !known || step == 0:
		d.close()
		d.synced = false
		return nil
	default:
		d.pos += step
		if step > 1 {
			d.gap = step - 1
			d.missed, d.run = d.missed+d.gap, max(d.run, d.gap)
		}
		if d.total > 0 && d.pos >= d.total {
			// The packet ended among the missed frames. This frame belongs to the next one, whose first frames
			// were missed with them.
			pos := d.pos - d.total
			d.close()
			d.pos = pos
		}
		if d.pos >= maxFrames {
			d.close()
			d.synced = false
			return nil
		}
	}
	if d.done {
		return nil
	}
	for len(d.got) < d.pos {
		d.got = append(d.got, nil)
	}
	d.got = append(d.got, append([]byte(nil), frame[1:]...))
	if frame[0]&frameEnd != 0 {
		if d.total != 0 && d.total != d.pos+1 {
			return d.drop()
		}
		d.total = d.pos + 1
	}
	return d.assemble()
}

// close ends the current packet, counting it as lost when it was never completed.
func (d *frameDecoder) close() {
	if d.synced && !d.done && d.got != nil {
		d.lost++
	}
	d.done, d.pos, d.total, d.got = false, 0, 0, nil
}

// drop gives up a packet whose frames contradict each other and waits for the next first frame.
func (d *frameDecoder) drop() []byte {
	d.close()
	d.synced = false
	return nil
}

// assemble returns the packet's bytes once every data frame is there or can be rebuilt.
func (d *frameDecoder) assemble() []byte {
	n := d.dataFrames()
	if n == 0 {
		return nil
	}
	total := n + frameChecks*((n+frameBlock-1)/frameBlock)
	if d.total != 0 && d.total != total || len(d.got) > total {
		return d.drop()
	}
	d.total = total
	stream := make([]byte, 0, n*frameData)
	for base, left := 0, n; left > 0; {
		count := min(left, frameBlock)
		for offset := range count {
			data := d.frame(base, count, offset)
			if data == nil {
				return nil
			}
			stream = append(stream, data...)
		}
		base, left = base+count+frameChecks, left-count
	}
	size := int(binary.BigEndian.Uint16(stream))
	if (2+size+frameData-1)/frameData != n {
		return d.drop()
	}
	d.done, d.got = true, nil
	return stream[2 : 2+size]
}

// dataFrames tells how many data frames the packet has: from the length in its first frame, or, when that frame was
// missed, from the position of the last frame or from the first block being a full one.
func (d *frameDecoder) dataFrames() int {
	if len(d.got) > 0 && d.got[0] == nil {
		switch {
		case d.total > 0:
			for blocks := 1; ; blocks++ {
				n := d.total - frameChecks*blocks
				if n < 1 {
					d.drop()
					return 0
				}
				if (n+frameBlock-1)/frameBlock == blocks {
					return n
				}
			}
		case len(d.got) >= frameBlock+frameChecks:
			d.frame(0, frameBlock, 0)
		}
	}
	if len(d.got) == 0 || d.got[0] == nil {
		return 0
	}
	return (2 + int(binary.BigEndian.Uint16(d.got[0])) + frameData - 1) / frameData
}

// frame returns the frame at an offset in the block of count data frames that starts at base, rebuilding it from
// the others of its group when it was missed. It is nil while that is not possible.
func (d *frameDecoder) frame(base, count, offset int) []byte {
	if base+offset < len(d.got) && d.got[base+offset] != nil {
		return d.got[base+offset]
	}
	data := make([]byte, frameData)
	for other := offset % frameChecks; other < count+frameChecks; other += frameChecks {
		if other == offset {
			continue
		}
		if base+other >= len(d.got) || d.got[base+other] == nil {
			return nil
		}
		for i, b := range d.got[base+other] {
			data[i] ^= b
		}
	}
	d.got[base+offset] = data
	d.restored++
	return data
}

func decodePacket(data []byte) (packet, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return packet{}, err
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(r, maxPacketBytes+1))
	if err != nil {
		return packet{}, err
	}
	if len(body) > maxPacketBytes {
		return packet{}, errors.New("packet too large")
	}
	var p packet
	if err := json.Unmarshal(body, &p); err != nil {
		return packet{}, err
	}
	if p.Type == "" || p.Session == "" || p.Seq < 1 || p.Seq >= math.MaxInt64 || p.Seq != math.Trunc(p.Seq) || p.At < 0 || p.At >= math.MaxInt64 || p.At != math.Trunc(p.At) {
		return packet{}, errors.New("packet has an invalid type, session, seq or timestamp")
	}
	return p, nil
}
