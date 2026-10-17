package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const window = 24 * time.Hour

// Collector polls the sources that have to be pulled and keeps the derived
// state the page shows. Pushed sources (Falco, pisentry webhook) land in the
// store through the API and are aggregated here on read.
type Collector struct {
	Store        *Store
	Kube         *Kube
	Log          *slog.Logger
	PisentryNS   string
	PisentryPort string
	FalcoNS      string
	Alertmanager string
	ScanDir      string
	KEVURL       string

	http *http.Client

	mu       sync.RWMutex
	nodes    map[string]string // name -> arch
	pods     []Pod
	podsErr  string
	podsAt   time.Time
	hosts    map[string]*HostState
	falcoUp  int
	alerts   []Alert
	alertErr string
	scan     ScanState
	kev      *KEV
}

// PisentryEvent mirrors the agent's event JSON.
type PisentryEvent struct {
	Time         time.Time         `json:"time"`
	Host         string            `json:"host"`
	Category     string            `json:"category"`
	CategoryName string            `json:"category_name"`
	Severity     string            `json:"severity"`
	MITRE        []string          `json:"mitre"`
	Detector     string            `json:"detector"`
	Summary      string            `json:"summary"`
	Key          string            `json:"key"`
	Fields       map[string]string `json:"fields"`
}

func (e PisentryEvent) Event() Event {
	return Event{
		ID:       hashID("pisentry", e.Host, e.Time.UTC().Format(time.RFC3339Nano), e.Category, e.Key, e.Summary),
		Time:     e.Time.UTC(),
		Source:   "pisentry",
		Severity: strings.ToLower(e.Severity),
		Host:     e.Host,
		Category: e.Category,
		Rule:     e.CategoryName,
		Summary:  e.Summary,
		MITRE:    e.MITRE,
		Tags:     []string{e.Detector},
		Fields:   e.Fields,
	}
}

// FalcoEvent is what Falco's http_output posts (json_output: true).
type FalcoEvent struct {
	Hostname     string         `json:"hostname"`
	Output       string         `json:"output"`
	OutputFields map[string]any `json:"output_fields"`
	Priority     string         `json:"priority"`
	Rule         string         `json:"rule"`
	Source       string         `json:"source"`
	Tags         []string       `json:"tags"`
	Time         time.Time      `json:"time"`
}

func (f FalcoEvent) Event() Event {
	fields := make(map[string]string, len(f.OutputFields))
	for k, v := range f.OutputFields {
		switch x := v.(type) {
		case string:
			fields[k] = x
		case float64:
			fields[k] = strings.TrimSuffix(fmt.Sprintf("%.0f", x), ".0")
		default:
			b, _ := json.Marshal(x)
			fields[k] = string(b)
		}
	}
	summary := f.Output
	if i := strings.Index(summary, ": "); i > 0 && i < 40 {
		summary = summary[i+2:] // drop the leading timestamp Falco repeats
	}
	summary = strings.ReplaceAll(summary, "\n", " ")
	ns, pod := fields["k8s.ns.name"], fields["k8s.pod.name"]
	return Event{
		ID:        hashID("falco", f.Hostname, f.Time.UTC().Format(time.RFC3339Nano), f.Rule, f.Output),
		Time:      f.Time.UTC(),
		Source:    "falco",
		Severity:  falcoSeverity(f.Priority),
		Host:      f.Hostname,
		Namespace: ns,
		Pod:       pod,
		Category:  f.Source,
		Rule:      f.Rule,
		Summary:   summary,
		Tags:      f.Tags,
		Fields:    fields,
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.http = &http.Client{Timeout: 15 * time.Second}
	c.hosts = map[string]*HostState{}
	c.loadKEVCache()
	c.pollPods(ctx) // first, so scans and posture have a pod list to join
	go c.loop(ctx, 30*time.Second, c.pollPisentry)
	go c.loop(ctx, 60*time.Second, c.pollPods)
	go c.loop(ctx, 60*time.Second, c.pollAlerts)
	go c.loop(ctx, 2*time.Minute, c.readScans)
	go c.loop(ctx, 24*time.Hour, c.fetchKEV)
	<-ctx.Done()
}

func (c *Collector) loop(ctx context.Context, every time.Duration, fn func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	fn(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
}

// pollPods lists all pods once a minute. It feeds agent coverage, posture
// checks and the image list for the Trivy sidecar.
func (c *Collector) pollPods(ctx context.Context) {
	if !c.Kube.Enabled() {
		return
	}
	pods, err := c.Kube.Pods(ctx, "", "")
	c.mu.Lock()
	if err != nil {
		c.podsErr = err.Error()
		c.mu.Unlock()
		c.Log.Warn("list pods", "err", err)
		return
	}
	c.pods, c.podsErr, c.podsAt = pods, "", time.Now()
	falco := 0
	for _, p := range pods {
		if p.Namespace == c.FalcoNS && p.Ready && p.Labels["app.kubernetes.io/name"] == "falco" {
			falco++
		}
	}
	c.falcoUp = falco
	c.mu.Unlock()
	if nodes, err := c.Kube.Nodes(ctx); err == nil {
		c.mu.Lock()
		c.nodes = nodes
		c.mu.Unlock()
	}
	c.writeImageList(pods)
}

// writeImageList hands the Trivy sidecar the set of images to scan.
func (c *Collector) writeImageList(pods []Pod) {
	seen := map[string]struct{}{}
	var imgs []string
	for _, p := range pods {
		if p.Phase != "Running" {
			continue
		}
		for _, ct := range p.Containers {
			ref := scanRef(ct)
			if _, ok := seen[ref]; ok || ref == "" {
				continue
			}
			seen[ref] = struct{}{}
			imgs = append(imgs, ref)
		}
	}
	sort.Strings(imgs)
	if err := os.MkdirAll(c.ScanDir, 0o755); err != nil {
		return
	}
	path := filepath.Join(c.ScanDir, "images.txt")
	if err := os.WriteFile(path+".tmp", []byte(strings.Join(imgs, "\n")+"\n"), 0o644); err == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

// scanRef is the image reference Trivy should scan: the spec image, which is
// what the user pinned in Git and what a re-pull would fetch.
func scanRef(ct Container) string {
	return strings.TrimPrefix(ct.Image, "docker.io/library/")
}

// pollPisentry pulls events and the Top 10 report from every agent pod.
func (c *Collector) pollPisentry(ctx context.Context) {
	if !c.Kube.Enabled() {
		return
	}
	pods, err := c.Kube.Pods(ctx, c.PisentryNS, "app.kubernetes.io/name=pisentry")
	if err != nil {
		c.Log.Warn("list pisentry pods", "err", err)
		return
	}
	seen := map[string]bool{}
	for _, p := range pods {
		if p.IP == "" || p.Node == "" {
			continue
		}
		seen[p.Node] = true
		hs := c.pollOneHost(ctx, p)
		c.mu.Lock()
		c.hosts[p.Node] = hs
		c.mu.Unlock()
	}
	c.mu.Lock()
	for n, h := range c.hosts {
		if !seen[n] {
			h.Up = false
			if h.Error == "" {
				h.Error = "agent pod gone"
			}
		}
	}
	c.mu.Unlock()
}

func (c *Collector) pollOneHost(ctx context.Context, p Pod) *HostState {
	base := fmt.Sprintf("http://%s:%s", p.IP, c.PisentryPort)
	hs := &HostState{Host: p.Node, Node: p.Node, PodIP: p.IP, Polled: time.Now().UTC()}
	if prev, ok := c.hostCopy(p.Node); ok {
		hs.Top10, hs.Failing, hs.AuditTime, hs.Generated = prev.Top10, prev.Failing, prev.AuditTime, prev.Generated
	}
	var evs []PisentryEvent
	if err := c.getJSON(ctx, base+"/api/events?limit=1000", &evs); err != nil {
		hs.Error = "events: " + err.Error()
		return hs
	}
	batch := make([]Event, 0, len(evs))
	for _, e := range evs {
		if e.Host == "" {
			e.Host = p.Node
		}
		batch = append(batch, e.Event())
	}
	if n := c.Store.Add(batch...); n > 0 {
		c.Log.Info("pisentry events", "host", p.Node, "new", n)
	}
	var rep struct {
		Host      string    `json:"host"`
		Generated time.Time `json:"generated"`
		Top10     []TopRow  `json:"top10"`
		Failing   []Finding `json:"posture_failing"`
		AuditTime time.Time `json:"audit_time"`
		StateAge  string    `json:"state_age"`
	}
	if err := c.getJSON(ctx, base+"/api/top10", &rep); err != nil {
		hs.Error = "top10: " + err.Error()
		return hs
	}
	if rep.Host != "" {
		hs.Host = rep.Host
	}
	hs.Up = true
	hs.Generated, hs.AuditTime, hs.StateAge = rep.Generated, rep.AuditTime, rep.StateAge
	hs.Top10, hs.Failing = rep.Top10, rep.Failing
	if hs.Failing == nil {
		hs.Failing = []Finding{}
	}
	return hs
}

func (c *Collector) hostCopy(node string) (HostState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, ok := c.hosts[node]
	if !ok {
		return HostState{}, false
	}
	return *h, true
}

func (c *Collector) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
}

// pollAlerts lists firing Alertmanager alerts that are not silenced or
// inhibited. Any failure is shown, not hidden.
func (c *Collector) pollAlerts(ctx context.Context) {
	if c.Alertmanager == "" {
		return
	}
	var raw []struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Status      struct {
			State string `json:"state"`
		} `json:"status"`
	}
	err := c.getJSON(ctx, c.Alertmanager+"/api/v2/alerts?active=true&silenced=false&inhibited=false", &raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.alertErr = err.Error()
		return
	}
	c.alertErr = ""
	out := make([]Alert, 0, len(raw))
	for _, a := range raw {
		if a.Status.State != "active" {
			continue
		}
		if n := a.Labels["alertname"]; n == "Watchdog" || n == "InfoInhibitor" {
			continue
		}
		sum := a.Annotations["summary"]
		if sum == "" {
			sum = a.Annotations["description"]
		}
		out = append(out, Alert{
			Name: a.Labels["alertname"], Severity: a.Labels["severity"], Namespace: a.Labels["namespace"],
			Summary: sum, StartsAt: a.StartsAt, Labels: a.Labels,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	c.alerts = out
}

// ---- derived views ----

// Hosts returns pisentry state per node with 24h event counts folded in.
func (c *Collector) Hosts() []HostState {
	evs := c.Store.Since(time.Now().Add(-window))
	c.mu.RLock()
	nodes := c.nodes
	out := make([]HostState, 0, len(c.hosts))
	for _, h := range c.hosts {
		out = append(out, *h)
	}
	c.mu.RUnlock()
	// Nodes with no agent still get a row so coverage gaps are visible.
	for n := range nodes {
		found := false
		for _, h := range out {
			if h.Node == n {
				found = true
				break
			}
		}
		if !found {
			out = append(out, HostState{Host: n, Node: n, Error: "no agent", Top10: []TopRow{}, Failing: []Finding{}})
		}
	}
	for i := range out {
		h := &out[i]
		cats := map[string]*CategoryHit{}
		for _, e := range evs {
			if e.Source != "pisentry" || (e.Host != h.Host && e.Host != h.Node) {
				continue
			}
			h.Events24h++
			if sevRank(e.Severity) > sevRank(h.Worst24h) {
				h.Worst24h = e.Severity
			}
			ch := cats[e.Category]
			if ch == nil {
				ch = &CategoryHit{ID: e.Category, Name: e.Rule}
				cats[e.Category] = ch
			}
			ch.Count++
			if sevRank(e.Severity) > sevRank(ch.Worst) {
				ch.Worst = e.Severity
			}
		}
		h.Categories = make([]CategoryHit, 0, len(cats))
		for _, ch := range cats {
			h.Categories = append(h.Categories, *ch)
		}
		sort.Slice(h.Categories, func(a, b int) bool {
			if h.Categories[a].Count != h.Categories[b].Count {
				return h.Categories[a].Count > h.Categories[b].Count
			}
			return h.Categories[a].ID < h.Categories[b].ID
		})
		if h.Top10 == nil {
			h.Top10 = []TopRow{}
		}
		if h.Failing == nil {
			h.Failing = []Finding{}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

// FalcoRules groups the last 24h of Falco events by rule.
func (c *Collector) FalcoRules() (rules []FalcoRule, total int) {
	evs := c.Store.Since(time.Now().Add(-window))
	byRule := map[string]*FalcoRule{}
	nsSet := map[string]map[string]struct{}{}
	podSet := map[string]map[string]struct{}{}
	hostSet := map[string]map[string]struct{}{}
	for _, e := range evs {
		if e.Source != "falco" {
			continue
		}
		total++
		r := byRule[e.Rule]
		if r == nil {
			r = &FalcoRule{Rule: e.Rule, Severity: e.Severity, Priority: e.Fields["priority"]}
			byRule[e.Rule] = r
			nsSet[e.Rule], podSet[e.Rule], hostSet[e.Rule] = map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
		}
		r.Count++
		if e.Time.After(r.Last) {
			r.Last, r.Sample = e.Time, e.Summary
		}
		if e.Namespace != "" {
			nsSet[e.Rule][e.Namespace] = struct{}{}
		}
		if e.Pod != "" {
			podSet[e.Rule][e.Pod] = struct{}{}
		}
		if e.Host != "" {
			hostSet[e.Rule][e.Host] = struct{}{}
		}
	}
	for name, r := range byRule {
		r.Namespaces = keys(nsSet[name])
		r.Hosts = keys(hostSet[name])
		r.Pods = len(podSet[name])
		rules = append(rules, *r)
	}
	sort.Slice(rules, func(i, j int) bool {
		if a, b := sevRank(rules[i].Severity), sevRank(rules[j].Severity); a != b {
			return a > b
		}
		return rules[i].Count > rules[j].Count
	})
	if rules == nil {
		rules = []FalcoRule{}
	}
	return rules, total
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Posture checks pod specs for settings worth a second look. Findings are
// grouped by workload so a DaemonSet shows once, not once per node.
func (c *Collector) Posture() (findings []PostureFinding, err string, at time.Time) {
	c.mu.RLock()
	pods, err, at := c.pods, c.podsErr, c.podsAt
	c.mu.RUnlock()
	type key struct{ check, ns, wl string }
	acc := map[key]*PostureFinding{}
	add := func(p Pod, check, sev, detail string) {
		k := key{check, p.Namespace, p.Workload}
		f := acc[k]
		if f == nil {
			f = &PostureFinding{ID: hashID("posture", check, p.Namespace, p.Workload), Check: check, Severity: sev,
				Namespace: p.Namespace, Workload: p.Kind + "/" + p.Workload, Detail: detail}
			acc[k] = f
		}
		f.Pods++
	}
	for _, p := range pods {
		if p.Phase != "Running" {
			continue
		}
		if p.HostPID {
			add(p, "hostPID", "medium", "shares the node PID namespace")
		}
		if p.HostNetwork {
			add(p, "hostNetwork", "medium", "uses the node network namespace")
		}
		if p.HostIPC {
			add(p, "hostIPC", "medium", "shares the node IPC namespace")
		}
		if len(p.HostPaths) > 0 {
			add(p, "hostPath", "low", strings.Join(uniq(p.HostPaths), ", "))
		}
		for _, ct := range p.Containers {
			if ct.Privileged {
				add(p, "privileged", "high", "container "+ct.Name+" runs privileged")
			}
			for _, cap := range ct.Caps {
				if cap == "SYS_ADMIN" || cap == "NET_ADMIN" || cap == "SYS_PTRACE" || cap == "ALL" {
					add(p, "capability "+cap, "medium", "container "+ct.Name+" adds "+cap)
				}
			}
			tag := imageTag(ct.Image)
			if tag == "latest" || tag == "" {
				add(p, "image tag latest", "low", ct.Image)
			}
		}
	}
	for _, f := range acc {
		findings = append(findings, *f)
	}
	sort.Slice(findings, func(i, j int) bool {
		if a, b := sevRank(findings[i].Severity), sevRank(findings[j].Severity); a != b {
			return a > b
		}
		if findings[i].Namespace != findings[j].Namespace {
			return findings[i].Namespace < findings[j].Namespace
		}
		return findings[i].Workload < findings[j].Workload
	})
	if findings == nil {
		findings = []PostureFinding{}
	}
	return findings, err, at
}

func imageTag(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		return "digest"
	}
	last := ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		last = ref[i+1:]
	}
	if i := strings.LastIndex(last, ":"); i >= 0 {
		return last[i+1:]
	}
	return ""
}

func uniq(in []string) []string {
	m := map[string]struct{}{}
	for _, s := range in {
		m[s] = struct{}{}
	}
	return keys(m)
}

// Coverage reports how many nodes each agent covers.
func (c *Collector) Coverage() (nodes int, pisentryUp int, falcoUp int, amd64 int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, arch := range c.nodes {
		if arch == "amd64" {
			amd64++
		}
	}
	for _, h := range c.hosts {
		if h.Up {
			pisentryUp++
		}
	}
	return len(c.nodes), pisentryUp, c.falcoUp, amd64
}

func (c *Collector) Alerts() ([]Alert, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := c.alerts
	if out == nil {
		out = []Alert{}
	}
	return out, c.alertErr
}

// Attention builds the triage queue from every source.
func (c *Collector) Attention(hosts []HostState, rules []FalcoRule, images []ImageVulns, posture []PostureFinding, alerts []Alert) []Attention {
	var out []Attention
	cut := time.Now().Add(-window)
	for _, e := range c.Store.Query("pisentry", "high", cut, "", "", 200) {
		out = append(out, Attention{ID: e.ID, Source: "pisentry", Severity: e.Severity, Title: e.Rule + ": " + e.Summary,
			Where: e.Host, Time: e.Time, Detail: fieldsLine(e.Fields)})
	}
	for _, h := range hosts {
		for _, f := range h.Failing {
			if sevRank(f.Severity) < sevRank("medium") {
				continue
			}
			out = append(out, Attention{ID: hashID("posture-host", h.Node, f.ID), Source: "pisentry", Severity: f.Severity,
				Title: f.ID + " " + f.Title, Detail: f.Detail, Where: h.Node, Time: h.AuditTime, Fix: f.Fix})
		}
		if !h.Up {
			out = append(out, Attention{ID: hashID("agent-down", h.Node), Source: "pisentry", Severity: "high",
				Title: "pisentry agent not reporting", Detail: h.Error, Where: h.Node, Time: h.Polled})
		}
	}
	for _, r := range rules {
		out = append(out, Attention{ID: hashID("falco-rule", r.Rule), Source: "falco", Severity: r.Severity,
			Title: r.Rule, Detail: r.Sample, Where: strings.Join(r.Namespaces, ", "), Count: r.Count, Time: r.Last})
	}
	for _, img := range images {
		for _, v := range img.KEV {
			out = append(out, Attention{ID: hashID("kev", img.Image, v.ID), Source: "trivy", Severity: "critical",
				Title: v.ID + " is in the CISA exploited list", Detail: v.Pkg + " " + v.Installed + fixedStr(v), Where: img.Short,
				Time: img.ScannedAt, Link: v.URL})
		}
		if n := countFixable(img.Top, "critical"); n > 0 {
			out = append(out, Attention{ID: hashID("crit-fix", img.Image), Source: "trivy", Severity: "high",
				Title: fmt.Sprintf("%d critical CVEs with a fix available", n), Where: img.Short, Count: n, Time: img.ScannedAt})
		}
		if img.Error != "" {
			out = append(out, Attention{ID: hashID("scan-err", img.Image), Source: "trivy", Severity: "low",
				Title: "image scan failed", Detail: img.Error, Where: img.Short, Time: img.ScannedAt})
		}
	}
	for _, p := range posture {
		out = append(out, Attention{ID: p.ID, Source: "posture", Severity: p.Severity, Title: p.Check,
			Detail: p.Detail, Where: p.Namespace + "/" + p.Workload, Count: p.Pods})
	}
	// Alertmanager alerts are operational, not security findings, so they
	// enter the queue one step down: critical shows as high, warning as medium.
	for _, a := range alerts {
		sev := "info"
		switch a.Severity {
		case "critical":
			sev = "high"
		case "warning":
			sev = "medium"
		}
		out = append(out, Attention{ID: hashID("alert", a.Name, a.Namespace, a.Labels["pod"], a.Labels["node"]), Source: "alert",
			Severity: sev, Title: a.Name, Detail: a.Summary, Where: a.Namespace, Time: a.StartsAt})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := sevRank(out[i].Severity), sevRank(out[j].Severity); a != b {
			return a > b
		}
		return out[i].Time.After(out[j].Time)
	})
	if out == nil {
		out = []Attention{}
	}
	return out
}

func fieldsLine(f map[string]string) string {
	if len(f) == 0 {
		return ""
	}
	ks := make([]string, 0, len(f))
	for k := range f {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	parts := make([]string, 0, len(ks))
	for _, k := range ks {
		parts = append(parts, k+"="+f[k])
	}
	return strings.Join(parts, " ")
}

func fixedStr(v Vuln) string {
	if v.Fixed == "" {
		return " (no fix yet)"
	}
	return ", fixed in " + v.Fixed
}

func countFixable(vs []Vuln, sev string) int {
	n := 0
	for _, v := range vs {
		if strings.EqualFold(v.Severity, sev) && v.Fixed != "" {
			n++
		}
	}
	return n
}
