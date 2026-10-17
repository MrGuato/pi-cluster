package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScanState is everything learned from the Trivy sidecar's report directory.
type ScanState struct {
	Images   []ImageVulns `json:"images"`
	ReadAt   time.Time    `json:"read_at"`
	Reports  int          `json:"reports"`
	Error    string       `json:"error,omitempty"`
	Scanning string       `json:"scanning,omitempty"` // image the sidecar is on now, from status file
	LastRun  time.Time    `json:"last_run"`
}

// KEV is the CISA Known Exploited Vulnerabilities catalog.
type KEV struct {
	Fetched time.Time           `json:"fetched"`
	Count   int                 `json:"count"`
	Entries map[string]KEVEntry `json:"entries"`
}

type KEVEntry struct {
	Vendor  string `json:"vendorProject"`
	Product string `json:"product"`
	Added   string `json:"dateAdded"`
	Due     string `json:"dueDate"`
	Ransom  string `json:"knownRansomwareCampaignUse"`
}

const staleScan = 48 * time.Hour

// trivyReport is the subset of `trivy image --format json` output used here.
type trivyReport struct {
	ArtifactName string    `json:"ArtifactName"`
	CreatedAt    time.Time `json:"CreatedAt"`
	Metadata     struct {
		OS struct {
			Family string `json:"Family"`
			Name   string `json:"Name"`
		} `json:"OS"`
	} `json:"Metadata"`
	Results []struct {
		Target          string `json:"Target"`
		Vulnerabilities []struct {
			ID        string `json:"VulnerabilityID"`
			Pkg       string `json:"PkgName"`
			Installed string `json:"InstalledVersion"`
			Fixed     string `json:"FixedVersion"`
			Severity  string `json:"Severity"`
			Title     string `json:"Title"`
			URL       string `json:"PrimaryURL"`
			CVSS      map[string]struct {
				V3 float64 `json:"V3Score"`
			} `json:"CVSS"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

// readScans parses every report the sidecar wrote and joins it with the
// running images so unused reports and unscanned images both show.
func (c *Collector) readScans(ctx context.Context) {
	reports := map[string]ImageVulns{}
	st := ScanState{ReadAt: time.Now().UTC()}
	entries, err := os.ReadDir(filepath.Join(c.ScanDir, "reports"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		st.Error = err.Error()
	}
	c.mu.RLock()
	kev := c.kev
	c.mu.RUnlock()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.ScanDir, "reports", name))
		if err != nil {
			continue
		}
		var r trivyReport
		if err := json.Unmarshal(b, &r); err != nil || r.ArtifactName == "" {
			// The sidecar writes "<hash>.err" next to a failed scan; a bad
			// JSON file is just skipped.
			continue
		}
		st.Reports++
		iv := summarize(r, kev)
		if info, err := e.Info(); err == nil {
			iv.ScannedAt = info.ModTime().UTC()
		}
		if !r.CreatedAt.IsZero() {
			iv.ScannedAt = r.CreatedAt.UTC()
		}
		reports[r.ArtifactName] = iv
		if iv.ScannedAt.After(st.LastRun) {
			st.LastRun = iv.ScannedAt
		}
	}
	errs := map[string]string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".err") {
			if b, err := os.ReadFile(filepath.Join(c.ScanDir, "reports", e.Name())); err == nil {
				// first line is the image ref, the rest is trivy's stderr tail
				parts := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
				if len(parts) == 2 {
					errs[parts[0]] = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(c.ScanDir, "status")); err == nil {
		st.Scanning = strings.TrimSpace(string(b))
	}

	c.mu.RLock()
	pods := c.pods
	c.mu.RUnlock()
	wl := map[string]map[string]struct{}{}
	for _, p := range pods {
		if p.Phase != "Running" {
			continue
		}
		for _, ct := range p.Containers {
			ref := scanRef(ct)
			if wl[ref] == nil {
				wl[ref] = map[string]struct{}{}
			}
			wl[ref][p.Namespace+"/"+p.Workload] = struct{}{}
		}
	}
	for ref, w := range wl {
		iv, ok := reports[ref]
		if !ok {
			iv = ImageVulns{Image: ref, KEV: []Vuln{}, Top: []Vuln{}}
			if msg, failed := errs[ref]; failed {
				iv.Error = msg
			}
		}
		iv.Short = shortImage(ref)
		iv.Workloads = keys(w)
		if !iv.ScannedAt.IsZero() && time.Since(iv.ScannedAt) > staleScan {
			iv.Stale = true
		}
		st.Images = append(st.Images, iv)
	}
	sort.Slice(st.Images, func(i, j int) bool {
		a, b := st.Images[i], st.Images[j]
		if len(a.KEV) != len(b.KEV) {
			return len(a.KEV) > len(b.KEV)
		}
		if a.Critical != b.Critical {
			return a.Critical > b.Critical
		}
		if a.High != b.High {
			return a.High > b.High
		}
		return a.Image < b.Image
	})
	if st.Images == nil {
		st.Images = []ImageVulns{}
	}
	c.mu.Lock()
	c.scan = st
	c.mu.Unlock()
}

func summarize(r trivyReport, kev *KEV) ImageVulns {
	iv := ImageVulns{Image: r.ArtifactName, KEV: []Vuln{}, Top: []Vuln{}}
	if r.Metadata.OS.Family != "" {
		iv.OS = r.Metadata.OS.Family + " " + r.Metadata.OS.Name
	}
	seen := map[string]struct{}{}
	var all []Vuln
	for _, res := range r.Results {
		for _, v := range res.Vulnerabilities {
			k := v.ID + "|" + v.Pkg + "|" + v.Installed
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			vv := Vuln{ID: v.ID, Pkg: v.Pkg, Installed: v.Installed, Fixed: v.Fixed, Severity: strings.ToLower(v.Severity), Title: v.Title, URL: v.URL}
			for src, s := range v.CVSS {
				if s.V3 > vv.CVSS || src == "nvd" && s.V3 > 0 {
					vv.CVSS = s.V3
				}
			}
			switch vv.Severity {
			case "critical":
				iv.Critical++
			case "high":
				iv.High++
			case "medium":
				iv.Medium++
			case "low":
				iv.Low++
			}
			if v.Fixed != "" {
				iv.Fixable++
			}
			if kev != nil {
				if ke, ok := kev.Entries[v.ID]; ok {
					vv.KEV, vv.KEVDue, vv.Ransom = true, ke.Due, strings.EqualFold(ke.Ransom, "Known")
					iv.KEV = append(iv.KEV, vv)
				}
			}
			all = append(all, vv)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if a, b := sevRank(all[i].Severity), sevRank(all[j].Severity); a != b {
			return a > b
		}
		if (all[i].Fixed != "") != (all[j].Fixed != "") {
			return all[i].Fixed != ""
		}
		if all[i].CVSS != all[j].CVSS {
			return all[i].CVSS > all[j].CVSS
		}
		return all[i].ID < all[j].ID
	})
	for _, v := range all {
		if sevRank(v.Severity) < sevRank("high") || len(iv.Top) >= 25 {
			break
		}
		iv.Top = append(iv.Top, v)
	}
	return iv
}

func shortImage(ref string) string {
	s := ref
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	for _, p := range []string{"docker.io/", "ghcr.io/", "quay.io/", "registry.k8s.io/", "code.forgejo.org/"} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

func (c *Collector) Scan() ScanState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := c.scan
	if st.Images == nil {
		st.Images = []ImageVulns{}
	}
	return st
}

func (c *Collector) KEVInfo() (count int, fetched time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.kev == nil {
		return 0, time.Time{}
	}
	return c.kev.Count, c.kev.Fetched
}

func (c *Collector) kevCachePath() string { return filepath.Join(filepath.Dir(c.ScanDir), "kev.json") }

func (c *Collector) loadKEVCache() {
	b, err := os.ReadFile(c.kevCachePath())
	if err != nil {
		return
	}
	var k KEV
	if json.Unmarshal(b, &k) == nil && len(k.Entries) > 0 {
		c.mu.Lock()
		c.kev = &k
		c.mu.Unlock()
		c.Log.Info("kev cache", "entries", k.Count, "fetched", k.Fetched)
	}
}

// fetchKEV downloads the CISA catalog once a day. ~3 MB, no auth.
func (c *Collector) fetchKEV(ctx context.Context) {
	if c.KEVURL == "" {
		return
	}
	c.mu.RLock()
	cur := c.kev
	c.mu.RUnlock()
	if cur != nil && time.Since(cur.Fetched) < 12*time.Hour {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.KEVURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "pi-cluster-security-dashboard/"+version)
	cl := &http.Client{Timeout: 60 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		c.Log.Warn("kev fetch", "err", err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		c.Log.Warn("kev fetch", "status", res.Status)
		return
	}
	var raw struct {
		Count           int `json:"count"`
		Vulnerabilities []struct {
			CVE string `json:"cveID"`
			KEVEntry
		} `json:"vulnerabilities"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		c.Log.Warn("kev decode", "err", err)
		return
	}
	k := &KEV{Fetched: time.Now().UTC(), Count: len(raw.Vulnerabilities), Entries: make(map[string]KEVEntry, len(raw.Vulnerabilities))}
	for _, v := range raw.Vulnerabilities {
		k.Entries[v.CVE] = v.KEVEntry
	}
	c.mu.Lock()
	c.kev = k
	c.mu.Unlock()
	if b, err := json.Marshal(k); err == nil {
		_ = os.WriteFile(c.kevCachePath(), b, 0o644)
	}
	c.Log.Info("kev updated", "entries", k.Count)
	c.readScans(ctx)
}
