package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	retention   = 30 * 24 * time.Hour
	maxEvents   = 50000
	eventsFile  = "events.ndjson"
	acksFile    = "acks.json"
	compactSlop = 2000 // rewrite the file once this many dead lines pile up
)

// Store keeps events in memory, appends them to an NDJSON file and holds
// acknowledgements. It is safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	dir    string
	log    *slog.Logger
	events []Event // sorted by Time ascending
	byID   map[string]struct{}
	acks   map[string]Ack
	f      *os.File
	dead   int // lines in the file that are no longer in memory
}

func OpenStore(dir string, log *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, log: log, byID: map[string]struct{}{}, acks: map[string]Ack{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	if err := s.compact(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, eventsFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	log.Info("store open", "events", len(s.events), "acks", len(s.acks))
	return s, nil
}

func (s *Store) load() error {
	f, err := os.Open(filepath.Join(s.dir, eventsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.loadAcks()
		}
		return err
	}
	defer f.Close()
	cut := time.Now().Add(-retention)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	lines, bad := 0, 0
	for sc.Scan() {
		lines++
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.ID == "" {
			bad++
			continue
		}
		if e.Time.Before(cut) {
			s.dead++
			continue
		}
		if _, dup := s.byID[e.ID]; dup {
			s.dead++
			continue
		}
		s.byID[e.ID] = struct{}{}
		s.events = append(s.events, e)
	}
	if bad > 0 {
		s.log.Warn("store: skipped unreadable lines", "lines", bad)
	}
	sort.SliceStable(s.events, func(i, j int) bool { return s.events[i].Time.Before(s.events[j].Time) })
	return s.loadAcks()
}

func (s *Store) loadAcks() error {
	b, err := os.ReadFile(filepath.Join(s.dir, acksFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.acks)
}

// compact rewrites the events file from memory. Called at start and when
// enough dead lines have accumulated. Caller holds no lock at start; later
// calls happen under mu.
func (s *Store) compact() error {
	path := filepath.Join(s.dir, eventsFile)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, e := range s.events {
		if err := enc.Encode(e); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if s.f != nil {
		s.f.Close()
		s.f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
	}
	s.dead = 0
	return nil
}

// Add stores events it has not seen. It returns how many were new.
func (s *Store) Add(evs ...Event) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	cut := time.Now().Add(-retention)
	for _, e := range evs {
		if e.ID == "" || e.Time.Before(cut) {
			continue
		}
		if _, dup := s.byID[e.ID]; dup {
			continue
		}
		s.byID[e.ID] = struct{}{}
		s.events = append(s.events, e)
		if s.f != nil {
			b, _ := json.Marshal(e)
			if _, err := s.f.Write(append(b, '\n')); err != nil {
				s.log.Error("store append", "err", err)
			}
		}
		n++
	}
	if n > 0 {
		sort.SliceStable(s.events, func(i, j int) bool { return s.events[i].Time.Before(s.events[j].Time) })
		s.prune()
	}
	return n
}

// prune drops expired events and keeps the slice bounded. Holds mu.
func (s *Store) prune() {
	cut := time.Now().Add(-retention)
	i := sort.Search(len(s.events), func(i int) bool { return !s.events[i].Time.Before(cut) })
	if over := len(s.events) - maxEvents; over > i {
		i = over
	}
	if i > 0 {
		for _, e := range s.events[:i] {
			delete(s.byID, e.ID)
		}
		s.events = append([]Event(nil), s.events[i:]...)
		s.dead += i
	}
	if s.dead >= compactSlop {
		if err := s.compact(); err != nil {
			s.log.Error("store compact", "err", err)
		}
	}
}

// Query returns events newest first that match the filter.
func (s *Store) Query(source, minSev string, since time.Time, host, ns string, limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	min := sevRank(minSev)
	out := make([]Event, 0, 64)
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.events[i]
		if e.Time.Before(since) {
			break
		}
		if source != "" && e.Source != source {
			continue
		}
		if sevRank(e.Severity) < min {
			continue
		}
		if host != "" && e.Host != host {
			continue
		}
		if ns != "" && e.Namespace != ns {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Since returns all events at or after t, oldest first.
func (s *Store) Since(t time.Time) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.events), func(i int) bool { return !s.events[i].Time.Before(t) })
	return append([]Event(nil), s.events[i:]...)
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

func (s *Store) Acks() map[string]Ack {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Ack, len(s.acks))
	for k, v := range s.acks {
		out[k] = v
	}
	return out
}

func (s *Store) SetAck(id, note string, undo bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if undo {
		delete(s.acks, id)
	} else {
		s.acks[id] = Ack{Time: time.Now().UTC(), Note: note}
	}
	b, err := json.MarshalIndent(s.acks, "", " ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, acksFile)
	if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			s.log.Error("store close", "err", fmt.Errorf("%w", err))
		}
		s.f = nil
	}
}
