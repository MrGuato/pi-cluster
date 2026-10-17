package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	historyFile = "history.json"
	historyKeep = 30 // daily snapshots kept
	historyDays = 14 // days of per-source event counts the page shows
	dayFormat   = "2006-01-02"
)

// DayCount is how many events each source produced on one UTC day.
type DayCount struct {
	Day      string `json:"day"`
	Pisentry int    `json:"pisentry"`
	Falco    int    `json:"falco"`
}

// Snapshot is the open state at one moment. The collector takes one every
// few minutes and the history keeps the last of each UTC day, so the page
// can show how the queue and the image set moved over a month.
type Snapshot struct {
	Day         string         `json:"day"`
	Time        time.Time      `json:"time"`
	Open        int            `json:"open"`
	OpenBySev   map[string]int `json:"open_by_sev"`
	KEVHits     int            `json:"kev_hits"`
	CritFixable int            `json:"critical_fixable"`
	CritImages  []string       `json:"crit_images"` // images with critical CVEs that have a fix
}

// History holds the daily snapshots, newest last, and persists them as one
// JSON file next to the event store. Safe for concurrent use.
type History struct {
	mu   sync.RWMutex
	path string
	log  *slog.Logger
	days []Snapshot
}

func OpenHistory(dir string, log *slog.Logger) *History {
	h := &History{path: filepath.Join(dir, historyFile), log: log}
	b, err := os.ReadFile(h.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn("history: read failed, starting over", "err", err)
		}
		return h
	}
	if err := json.Unmarshal(b, &h.days); err != nil {
		log.Warn("history: unreadable, starting over", "err", err)
		h.days = nil
		return h
	}
	sort.SliceStable(h.days, func(i, j int) bool { return h.days[i].Day < h.days[j].Day })
	h.trim()
	log.Info("history open", "days", len(h.days))
	return h
}

func (h *History) trim() {
	if len(h.days) > historyKeep {
		h.days = append([]Snapshot(nil), h.days[len(h.days)-historyKeep:]...)
	}
}

// Record stores a snapshot. It replaces an earlier one from the same UTC
// day, so the file holds one point per day with the day's last state.
func (h *History) Record(s Snapshot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.Time.IsZero() {
		s.Time = time.Now()
	}
	s.Time = s.Time.UTC()
	s.Day = s.Time.Format(dayFormat)
	if s.OpenBySev == nil {
		s.OpenBySev = map[string]int{}
	}
	if s.CritImages == nil {
		s.CritImages = []string{}
	}
	sort.Strings(s.CritImages)
	switch n := len(h.days); {
	case n > 0 && h.days[n-1].Day == s.Day:
		h.days[n-1] = s
	case n > 0 && h.days[n-1].Day > s.Day:
		return nil // clock went backwards; keep the newer point
	default:
		h.days = append(h.days, s)
	}
	h.trim()
	b, err := json.MarshalIndent(h.days, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(h.path+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(h.path+".tmp", h.path)
}

// Snapshots returns a copy of the stored points, oldest first.
func (h *History) Snapshots() []Snapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Snapshot, len(h.days))
	copy(out, h.days)
	return out
}

// RedDays counts, per image, the run of daily snapshots ending with the
// newest one in which the image had critical fixable CVEs. It also returns
// the day of the newest snapshot so the caller can tell whether today is
// already in the count.
func (h *History) RedDays() (streak map[string]int, newest string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	streak = map[string]int{}
	last := len(h.days) - 1
	for i := last; i >= 0; i-- {
		for _, img := range h.days[i].CritImages {
			if streak[img] == last-i { // unbroken from the newest point down to i
				streak[img]++
			}
		}
	}
	if last >= 0 {
		newest = h.days[last].Day
	}
	return streak, newest
}

// Daily counts events per source for the last n UTC days, today included,
// oldest first. Days with nothing stored read as zero.
func (s *Store) Daily(n int) []DayCount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	day := 24 * time.Hour
	start := time.Now().UTC().Truncate(day).Add(-time.Duration(n-1) * day)
	out := make([]DayCount, n)
	for i := range out {
		out[i].Day = start.Add(time.Duration(i) * day).Format(dayFormat)
	}
	i := sort.Search(len(s.events), func(i int) bool { return !s.events[i].Time.Before(start) })
	for _, e := range s.events[i:] {
		d := int(e.Time.UTC().Truncate(day).Sub(start) / day)
		if d < 0 || d >= n {
			continue
		}
		switch e.Source {
		case "pisentry":
			out[d].Pisentry++
		case "falco":
			out[d].Falco++
		}
	}
	return out
}

// openCounts is the triage queue size the header shows: items at medium or
// above that nobody has marked reviewed.
func openCounts(att []Attention, acks map[string]Ack) (open int, bySev map[string]int) {
	bySev = map[string]int{}
	for _, it := range att {
		if _, ok := acks[it.ID]; ok || sevRank(it.Severity) < sevRank("medium") {
			continue
		}
		open++
		bySev[it.Severity]++
	}
	return open, bySev
}

// snapshot records the open state for the history. It waits for the first
// scan read so an empty image list at start does not clear today's point.
func (c *Collector) snapshot(ctx context.Context) {
	if c.History == nil {
		return
	}
	scan := c.Scan()
	if scan.ReadAt.IsZero() {
		return
	}
	hosts := c.Hosts()
	rules, _ := c.FalcoRules()
	posture, _, _ := c.Posture()
	alerts, _ := c.Alerts()
	att := c.Attention(hosts, rules, scan.Images, posture, alerts)
	open, bySev := openCounts(att, c.Store.Acks())
	s := Snapshot{Time: time.Now().UTC(), Open: open, OpenBySev: bySev, CritImages: []string{}}
	for _, img := range scan.Images {
		s.KEVHits += len(img.KEV)
		if n := countFixable(img.Top, "critical"); n > 0 {
			s.CritFixable += n
			s.CritImages = append(s.CritImages, img.Image)
		}
	}
	if err := c.History.Record(s); err != nil {
		c.Log.Error("history record", "err", err)
	}
}
