package main

import (
	"errors"
	"slices"
	"strings"
	"sync"
)

type indexAck struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq"`
}

type savedIndex struct {
	ack  indexAck
	dir  string
	data gameSnapshot
}

type snapshotIndexes struct {
	mu     sync.Mutex
	recent []savedIndex
}

// Advertise a base only after the complete immutable snapshot exists on disk.
// Message delivery acknowledgements are earlier and cannot guarantee this.
func (idx *snapshotIndexes) remember(dir, id string, p packet, state gameSnapshot) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if len(idx.recent) == 8 {
		idx.recent = slices.Delete(idx.recent, 0, 1)
	}
	idx.recent = append(idx.recent, savedIndex{dir: dir, ack: indexAck{ID: id, Seq: int64(p.Seq)}, data: state})
}

func (idx *snapshotIndexes) confirmed() map[string]indexAck {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if len(idx.recent) == 0 {
		return nil
	}
	acks := make(map[string]indexAck)
	for _, b := range idx.recent {
		if acks[b.data.Session].Seq < b.ack.Seq {
			acks[b.data.Session] = b.ack
		}
	}
	return acks
}

func (idx *snapshotIndexes) forget(session string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.recent = slices.DeleteFunc(idx.recent, func(b savedIndex) bool { return b.data.Session == session })
}

func (idx *snapshotIndexes) load(dir, id string) (gameSnapshot, error) {
	// In-flight messages may still refer to earlier acknowledgements. Those
	// files stay valid after newer bases are advertised or the memory ring wraps.
	if idx != nil {
		idx.mu.Lock()
		for _, b := range idx.recent {
			if b.ack.ID == id && b.dir == dir {
				idx.mu.Unlock()
				return b.data, nil
			}
		}
		idx.mu.Unlock()
	}
	return loadGameSnapshot(dir, id)
}

var reusableSections = []string{"gear", "bags", "skills", "spells", "talents", "reputation", "quests"}
var errIndexBase = errors.New("Не удалось восстановить игровой снимок. После обновления окна отправь вопрос ещё раз, чтобы заново считать данные из игры.")

func (idx *snapshotIndexes) restore(dir string, p packet, s *gameSnapshot) error {
	var baseID string
	var sections []string
	for line := range strings.SplitSeq(p.ContextText, "\n") {
		if id, ok := strings.CutPrefix(line, "Index base: "); ok {
			if baseID != "" || !snapshotIDPattern.MatchString(id) {
				return errIndexBase
			}
			baseID = id
		}
		if groups, ok := strings.CutPrefix(line, "Reuse: "); ok {
			if len(sections) > 0 {
				return errIndexBase
			}
			sections = strings.Split(groups, ",")
		}
	}
	if baseID == "" && len(sections) == 0 {
		return nil
	}
	if baseID == "" || len(sections) == 0 || !s.Lookup || p.RunID == "" || p.Session == "" {
		return errIndexBase
	}
	base, err := idx.load(dir, baseID)
	if err != nil || !base.Lookup || base.Session != p.Session || base.RunID != p.RunID {
		return errIndexBase
	}
	for _, section := range sections {
		_, alreadyPresent := s.Sections[section]
		summary, available := base.Sections[section]
		if !slices.Contains(reusableSections, section) || alreadyPresent || !available || slices.ContainsFunc(s.Entries, func(e gameEntry) bool { return e.str("section") == section }) {
			return errIndexBase
		}
		s.Sections[section] = summary
		if section == "quests" {
			s.Quests = slices.Clone(base.Quests)
		}
		for _, entry := range base.Entries {
			if entry.str("section") == section {
				s.Entries = append(s.Entries, entry)
			}
		}
	}
	return nil
}
