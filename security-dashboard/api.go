package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type API struct {
	Store     *Store
	Collector *Collector
	Log       *slog.Logger
}

func (a *API) Routes() http.Handler {
	mx := http.NewServeMux()
	mx.HandleFunc("GET /{$}", a.index)
	mx.HandleFunc("GET /api/state", a.state)
	mx.HandleFunc("GET /api/events", a.events)
	mx.HandleFunc("POST /api/ack", a.ack)
	return mx
}

func (a *API) index(w http.ResponseWriter, r *http.Request) {
	b, err := web.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "page missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'; base-uri 'none'; form-action 'none'")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// state is the single document the page polls every 30 seconds.
func (a *API) state(w http.ResponseWriter, r *http.Request) {
	c := a.Collector
	hosts := c.Hosts()
	rules, falcoTotal := c.FalcoRules()
	scan := c.Scan()
	posture, postureErr, postureAt := c.Posture()
	alerts, alertErr := c.Alerts()
	nodes, pisUp, falcoUp, amd64 := c.Coverage()
	kevCount, kevAt := c.KEVInfo()
	att := c.Attention(hosts, rules, scan.Images, posture, alerts)
	acks := a.Store.Acks()

	pisEvents, pisWorst := 0, ""
	for _, h := range hosts {
		pisEvents += h.Events24h
		if sevRank(h.Worst24h) > sevRank(pisWorst) {
			pisWorst = h.Worst24h
		}
	}
	red, redNewest := c.History.RedDays()
	today := time.Now().UTC().Format(dayFormat)
	kevHits, critFix, scanned, redImages := 0, 0, 0, 0
	for i := range scan.Images {
		img := &scan.Images[i]
		kevHits += len(img.KEV)
		if n := countFixable(img.Top, "critical"); n > 0 {
			critFix += n
			redImages++
			img.RedDays = red[img.Image]
			if redNewest != today {
				img.RedDays++ // no snapshot yet today; it is red now
			}
		}
		if !img.ScannedAt.IsZero() {
			scanned++
		}
	}
	open, openBySev := openCounts(att, acks)
	writeJSON(w, map[string]any{
		"timestamp": time.Now().UTC(),
		"version":   version,
		"window":    "24h",
		"summary": map[string]any{
			"open":             open,
			"open_by_sev":      openBySev,
			"pisentry_events":  pisEvents,
			"pisentry_worst":   pisWorst,
			"falco_events":     falcoTotal,
			"falco_rules":      len(rules),
			"kev_hits":         kevHits,
			"critical_fixable": critFix,
			"red_images":       redImages,
			"images":           len(scan.Images),
			"images_scanned":   scanned,
			"stored_events":    a.Store.Len(),
		},
		"agents": map[string]any{
			"nodes":       nodes,
			"pisentry_up": pisUp,
			"falco_up":    falcoUp,
			"falco_max":   amd64,
		},
		"hosts":     hosts,
		"falco":     map[string]any{"total": falcoTotal, "rules": rules},
		"vulns":     map[string]any{"images": scan.Images, "reports": scan.Reports, "read_at": scan.ReadAt, "last_run": scan.LastRun, "scanning": scan.Scanning, "error": scan.Error, "kev_count": kevCount, "kev_fetched": kevAt},
		"posture":   map[string]any{"findings": posture, "error": postureErr, "at": postureAt},
		"alerts":    map[string]any{"items": alerts, "error": alertErr},
		"attention": att,
		"acks":      acks,
		"history":   map[string]any{"days": historyDays, "events": a.Store.Daily(historyDays), "snapshots": c.History.Snapshots()},
	})
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	since := time.Now().Add(-window)
	if v := q.Get("since"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			since = time.Now().Add(-d)
		}
	}
	writeJSON(w, a.Store.Query(q.Get("source"), q.Get("severity"), since, q.Get("host"), q.Get("namespace"), limit))
}

func (a *API) ack(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string `json:"id"`
		Note string `json:"note"`
		Undo bool   `json:"undo"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil || in.ID == "" || len(in.ID) > 64 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(in.Note) > 300 {
		in.Note = in.Note[:300]
	}
	if err := a.Store.SetAck(in.ID, in.Note, in.Undo); err != nil {
		a.Log.Error("ack", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	a.Log.Info("ack", "id", in.ID, "undo", in.Undo, "user", r.Header.Get("X-Dashboard-User"), "ip", clientIP(r))
	writeJSON(w, map[string]any{"ok": true})
}

// IngestFalco accepts Falco http_output: one JSON object per request, or
// several separated by newlines.
func (a *API) IngestFalco(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var batch []Event
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	bad := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var fe FalcoEvent
		if err := json.Unmarshal(line, &fe); err != nil || fe.Rule == "" {
			bad++
			continue
		}
		if fe.Time.IsZero() {
			fe.Time = time.Now()
		}
		ev := fe.Event()
		if ev.Fields == nil {
			ev.Fields = map[string]string{}
		}
		ev.Fields["priority"] = fe.Priority
		batch = append(batch, ev)
	}
	if len(batch) == 0 && bad > 0 {
		// Falco posts a single object; try that shape before giving up.
		var fe FalcoEvent
		if err := json.Unmarshal(body, &fe); err == nil && fe.Rule != "" {
			ev := fe.Event()
			if ev.Fields == nil {
				ev.Fields = map[string]string{}
			}
			ev.Fields["priority"] = fe.Priority
			batch = append(batch, ev)
			bad = 0
		}
	}
	n := a.Store.Add(batch...)
	if bad > 0 {
		a.Log.Warn("falco ingest: unreadable items", "bad", bad, "ip", clientIP(r))
	}
	writeJSON(w, map[string]any{"ok": true, "stored": n})
}

// IngestPisentry accepts the agent's generic webhook (the raw event JSON).
func (a *API) IngestPisentry(w http.ResponseWriter, r *http.Request) {
	var pe PisentryEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&pe); err != nil || pe.Category == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if pe.Time.IsZero() {
		pe.Time = time.Now()
	}
	n := a.Store.Add(pe.Event())
	writeJSON(w, map[string]any{"ok": true, "stored": n})
}
