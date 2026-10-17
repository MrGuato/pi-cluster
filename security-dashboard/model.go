package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Event is one normalized security event from any source.
type Event struct {
	ID        string            `json:"id"`
	Time      time.Time         `json:"time"`
	Source    string            `json:"source"` // pisentry | falco
	Severity  string            `json:"severity"`
	Host      string            `json:"host,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Category  string            `json:"category,omitempty"` // pisentry PS01..PS10, falco source
	Rule      string            `json:"rule"`               // pisentry category name or Falco rule
	Summary   string            `json:"summary"`
	MITRE     []string          `json:"mitre,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// Severity order, lowest first. Unknown values sort as info.
var sevOrder = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}

func sevRank(s string) int { return sevOrder[strings.ToLower(s)] }

// falcoSeverity maps Falco priorities onto the pisentry scale so the two
// sources sort together.
func falcoSeverity(p string) string {
	switch strings.ToLower(p) {
	case "emergency", "alert", "critical":
		return "critical"
	case "error":
		return "high"
	case "warning":
		return "medium"
	case "notice":
		return "low"
	default:
		return "info"
	}
}

func hashID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:8])
}

// Ack records that a person looked at an item and decided it is fine for now.
type Ack struct {
	Time time.Time `json:"time"`
	Note string    `json:"note,omitempty"`
}

// Attention is one row of the triage queue, built server side so every
// client sees the same list.
type Attention struct {
	ID       string    `json:"id"`
	Source   string    `json:"source"` // pisentry | falco | trivy | posture | alert
	Severity string    `json:"severity"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
	Where    string    `json:"where,omitempty"` // host, namespace/workload or image
	Count    int       `json:"count,omitempty"`
	Time     time.Time `json:"time"`
	Link     string    `json:"link,omitempty"`
	Fix      string    `json:"fix,omitempty"`
}

// HostState is what pisentry reports for one node.
type HostState struct {
	Host       string        `json:"host"`
	Node       string        `json:"node"`
	PodIP      string        `json:"-"`
	Up         bool          `json:"up"`
	Error      string        `json:"error,omitempty"`
	Polled     time.Time     `json:"polled"`
	Generated  time.Time     `json:"generated"`
	AuditTime  time.Time     `json:"audit_time"`
	StateAge   string        `json:"state_age,omitempty"`
	Top10      []TopRow      `json:"top10"`
	Failing    []Finding     `json:"posture_failing"`
	Events24h  int           `json:"events_24h"`
	Worst24h   string        `json:"worst_24h"`
	Categories []CategoryHit `json:"categories_24h"`
}

type TopRow struct {
	Rank     int        `json:"rank"`
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Events   int        `json:"events"`
	AllTime  int        `json:"all_time"`
	Worst    string     `json:"worst"`
	Score    int        `json:"score"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	MITRE    []string   `json:"mitre"`
}

type Finding struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

type CategoryHit struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Worst string `json:"worst"`
}

// FalcoRule is one rule aggregated over the window.
type FalcoRule struct {
	Rule       string    `json:"rule"`
	Priority   string    `json:"priority"`
	Severity   string    `json:"severity"`
	Count      int       `json:"count"`
	Last       time.Time `json:"last"`
	Namespaces []string  `json:"namespaces"`
	Pods       int       `json:"pods"`
	Hosts      []string  `json:"hosts"`
	Sample     string    `json:"sample"`
}

// ImageVulns is the Trivy result for one running image.
type ImageVulns struct {
	Image     string    `json:"image"`
	Short     string    `json:"short"`
	ScannedAt time.Time `json:"scanned_at"`
	OS        string    `json:"os,omitempty"`
	Critical  int       `json:"critical"`
	High      int       `json:"high"`
	Medium    int       `json:"medium"`
	Low       int       `json:"low"`
	Fixable   int       `json:"fixable"`
	KEV       []Vuln    `json:"kev"`
	Top       []Vuln    `json:"top"`
	Workloads []string  `json:"workloads"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
}

type Vuln struct {
	ID        string  `json:"id"`
	Pkg       string  `json:"pkg"`
	Installed string  `json:"installed"`
	Fixed     string  `json:"fixed,omitempty"`
	Severity  string  `json:"severity"`
	Title     string  `json:"title,omitempty"`
	URL       string  `json:"url,omitempty"`
	CVSS      float64 `json:"cvss,omitempty"`
	KEV       bool    `json:"kev,omitempty"`
	KEVDue    string  `json:"kev_due,omitempty"`
	Ransom    bool    `json:"ransomware,omitempty"`
}

// PostureFinding is a risky pod spec setting seen in the cluster.
type PostureFinding struct {
	ID        string `json:"id"`
	Check     string `json:"check"`
	Severity  string `json:"severity"`
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Detail    string `json:"detail"`
	Pods      int    `json:"pods"`
}

// Alert is one active Alertmanager alert.
type Alert struct {
	Name      string            `json:"name"`
	Severity  string            `json:"severity"`
	Namespace string            `json:"namespace,omitempty"`
	Summary   string            `json:"summary"`
	StartsAt  time.Time         `json:"starts_at"`
	Labels    map[string]string `json:"labels"`
}
