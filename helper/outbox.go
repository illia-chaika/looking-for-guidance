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
	settleFor   = 8 * time.Millisecond
	rescanEvery = 30 * time.Second
	// While frames are arriving the file is also checked on a timer: a late notification would cost a frame.
	activeFor      = time.Second
	frameBytes     = bitCount / 8
	maxPacketBytes = 1 << 20
)

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

	// A journal card asks for researched advice: kind and key name it, facts describe the character.
	Kind       string          `json:"kind,omitempty"`
	Key        string          `json:"key,omitempty"`
	Locale     string          `json:"locale,omitempty"`
	GameLocale string          `json:"gameLocale,omitempty"`
	Force      bool            `json:"force,omitempty"`
	Facts      json.RawMessage `json:"facts,omitempty"`

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
	lost    int
	scanned bool
	// The longest time the watcher went without looking at the files since the last frame, and the last time it looked.
	pause      time.Duration
	looked     time.Time
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
			if err := o.scan(time.Now()); err != nil {
				return err
			}
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
				} else if err := o.scan(time.Now()); err != nil {
					return err
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
				if err := o.scan(time.Now()); err != nil {
					return err
				}
			}
		case now := <-tick.C:
			if err := o.scan(now); err != nil {
				return err
			}
		case <-settle.C:
		}
		now := time.Now()
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
	frames, lost, missed, run := 0, 0, 0, 0
	for _, f := range o.files {
		frames, lost, missed, run = frames+f.frames.frames, lost+f.frames.lost, missed+f.frames.missed, max(run, f.frames.run)
	}
	if lost != o.lost {
		logf("Game frames: %d read, %d packets lost to %d missed frames so far, at most %d in a row", frames, lost, missed, run)
		o.lost = lost
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
	read := f.frames.frames
	data := f.frames.add(frame)
	name, _ := filepath.Rel(o.dir, path)
	if f.frames.frames != read {
		// The numbers tell the causes apart: a long pause means this app was not running or was busy, a short one
		// that the frames were not in the file when it looked.
		if gap := f.frames.gap; gap > 0 {
			logf("Missed %d frames of a packet from %s: %d ms since the frame before, the watcher's longest pause in that time was %d ms",
				gap, name, now.Sub(f.frameAt).Milliseconds(), o.pause.Milliseconds())
		}
		f.frameAt, o.pause = now, 0
	}
	if data == nil {
		return packet{}, nil
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

type frameDecoder struct {
	last    [frameBytes]byte
	started bool
	buf     []byte
	// A frame missed in the middle of a packet loses that packet. The game rewrites AddOns.txt every 40 ms.
	// The counter in each frame tells how many were missed and how many of them in a row.
	frames, lost, missed, run int
	// How many frames were missed just before the latest one, inside a packet.
	gap int
}

func (d *frameDecoder) start(frame [frameBytes]byte) {
	d.last, d.started = frame, true
}

func (d *frameDecoder) add(frame [frameBytes]byte) []byte {

	if d.started && frame == d.last {
		return nil
	}
	d.frames++
	d.gap = 0
	if d.started && frame[0]&0x7f != (d.last[0]+1)&0x7f {
		if d.buf != nil {
			d.gap = int((frame[0] - d.last[0] - 1) & 0x7f)
			d.lost, d.missed, d.run = d.lost+1, d.missed+d.gap, max(d.run, d.gap)
		}
		d.buf = nil
	}
	d.last, d.started = frame, true
	switch {
	case frame[0]&0x80 != 0:
		d.buf = append([]byte(nil), frame[1:]...)
	case d.buf != nil:
		d.buf = append(d.buf, frame[1:]...)
	default:
		return nil
	}
	if len(d.buf) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(d.buf))
	if len(d.buf) < 2+n {
		return nil
	}
	data := d.buf[2 : 2+n]
	d.buf = nil
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
