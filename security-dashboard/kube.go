package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Kube is a minimal read-only client for the API server. In the cluster it
// uses the mounted service account; for a local run set KUBE_API (for example
// http://127.0.0.1:8001 from kubectl proxy) and optionally KUBE_TOKEN.
type Kube struct {
	base   string
	token  string
	client *http.Client
	log    *slog.Logger
}

func NewKube(log *slog.Logger) *Kube {
	k := &Kube{log: log, client: &http.Client{Timeout: 20 * time.Second}}
	if base := os.Getenv("KUBE_API"); base != "" {
		k.base = strings.TrimRight(base, "/")
		k.token = os.Getenv("KUBE_TOKEN")
		return k
	}
	const sa = "/var/run/secrets/kubernetes.io/serviceaccount"
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" {
		log.Warn("kube: not in cluster and KUBE_API unset; pod data disabled")
		return k
	}
	k.base = "https://" + host + ":" + port
	if b, err := os.ReadFile(sa + "/token"); err == nil {
		k.token = strings.TrimSpace(string(b))
	}
	pool := x509.NewCertPool()
	if b, err := os.ReadFile(sa + "/ca.crt"); err == nil {
		pool.AppendCertsFromPEM(b)
	}
	k.client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	return k
}

func (k *Kube) Enabled() bool { return k.base != "" }

func (k *Kube) get(ctx context.Context, path string, q url.Values, out any) error {
	if k.base == "" {
		return fmt.Errorf("kube api not configured")
	}
	u := k.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}
	req.Header.Set("Accept", "application/json")
	res, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s: %s", path, res.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Pod is the subset of a pod spec the dashboard looks at.
type Pod struct {
	Name        string
	Namespace   string
	Node        string
	IP          string
	Phase       string
	Ready       bool
	Workload    string // owner name with the replica hash removed
	Kind        string
	HostNetwork bool
	HostPID     bool
	HostIPC     bool
	HostPaths   []string
	Containers  []Container
	Labels      map[string]string
}

type Container struct {
	Name       string
	Image      string
	ImageID    string
	Privileged bool
	RunAsRoot  bool // runAsNonRoot not set true at pod or container level
	NoPrivEsc  bool // allowPrivilegeEscalation explicitly false
	Caps       []string
}

type podList struct {
	Items []struct {
		Metadata struct {
			Name            string            `json:"name"`
			Namespace       string            `json:"namespace"`
			Labels          map[string]string `json:"labels"`
			OwnerReferences []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"ownerReferences"`
		} `json:"metadata"`
		Spec struct {
			NodeName        string `json:"nodeName"`
			HostNetwork     bool   `json:"hostNetwork"`
			HostPID         bool   `json:"hostPID"`
			HostIPC         bool   `json:"hostIPC"`
			SecurityContext struct {
				RunAsNonRoot *bool `json:"runAsNonRoot"`
			} `json:"securityContext"`
			Volumes []struct {
				Name     string `json:"name"`
				HostPath *struct {
					Path string `json:"path"`
				} `json:"hostPath"`
			} `json:"volumes"`
			Containers []struct {
				Name            string `json:"name"`
				Image           string `json:"image"`
				SecurityContext *struct {
					Privileged               *bool `json:"privileged"`
					RunAsNonRoot             *bool `json:"runAsNonRoot"`
					AllowPrivilegeEscalation *bool `json:"allowPrivilegeEscalation"`
					Capabilities             *struct {
						Add []string `json:"add"`
					} `json:"capabilities"`
				} `json:"securityContext"`
			} `json:"containers"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			PodIP             string `json:"podIP"`
			ContainerStatuses []struct {
				Name    string `json:"name"`
				ImageID string `json:"imageID"`
				Ready   bool   `json:"ready"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// Pods lists pods, in one namespace or all when ns is empty.
func (k *Kube) Pods(ctx context.Context, ns, selector string) ([]Pod, error) {
	path := "/api/v1/pods"
	if ns != "" {
		path = "/api/v1/namespaces/" + url.PathEscape(ns) + "/pods"
	}
	q := url.Values{}
	if selector != "" {
		q.Set("labelSelector", selector)
	}
	var pl podList
	if err := k.get(ctx, path, q, &pl); err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(pl.Items))
	for _, it := range pl.Items {
		p := Pod{
			Name: it.Metadata.Name, Namespace: it.Metadata.Namespace, Node: it.Spec.NodeName,
			IP: it.Status.PodIP, Phase: it.Status.Phase, Labels: it.Metadata.Labels,
			HostNetwork: it.Spec.HostNetwork, HostPID: it.Spec.HostPID, HostIPC: it.Spec.HostIPC,
		}
		p.Kind, p.Workload = "Pod", it.Metadata.Name
		if len(it.Metadata.OwnerReferences) > 0 {
			o := it.Metadata.OwnerReferences[0]
			p.Kind, p.Workload = o.Kind, o.Name
			if o.Kind == "ReplicaSet" {
				p.Kind = "Deployment"
				if i := strings.LastIndex(o.Name, "-"); i > 0 {
					p.Workload = o.Name[:i]
				}
			}
			p.Workload = stripHash(p.Workload)
		}
		for _, v := range it.Spec.Volumes {
			if v.HostPath != nil {
				p.HostPaths = append(p.HostPaths, v.HostPath.Path)
			}
		}
		ready := map[string]bool{}
		ids := map[string]string{}
		for _, cs := range it.Status.ContainerStatuses {
			ready[cs.Name] = cs.Ready
			ids[cs.Name] = cs.ImageID
		}
		p.Ready = len(it.Status.ContainerStatuses) > 0
		podNonRoot := it.Spec.SecurityContext.RunAsNonRoot != nil && *it.Spec.SecurityContext.RunAsNonRoot
		for _, c := range it.Spec.Containers {
			cc := Container{Name: c.Name, Image: c.Image, ImageID: ids[c.Name], RunAsRoot: !podNonRoot}
			if sc := c.SecurityContext; sc != nil {
				cc.Privileged = sc.Privileged != nil && *sc.Privileged
				if sc.RunAsNonRoot != nil {
					cc.RunAsRoot = !*sc.RunAsNonRoot
				}
				cc.NoPrivEsc = sc.AllowPrivilegeEscalation != nil && !*sc.AllowPrivilegeEscalation
				if sc.Capabilities != nil {
					cc.Caps = sc.Capabilities.Add
				}
			}
			if !ready[c.Name] {
				p.Ready = false
			}
			p.Containers = append(p.Containers, cc)
		}
		out = append(out, p)
	}
	return out, nil
}

// Nodes returns node names with their architecture.
func (k *Kube) Nodes(ctx context.Context) (map[string]string, error) {
	var nl struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				NodeInfo struct {
					Architecture  string `json:"architecture"`
					KernelVersion string `json:"kernelVersion"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := k.get(ctx, "/api/v1/nodes", nil, &nl); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, n := range nl.Items {
		out[n.Metadata.Name] = n.Status.NodeInfo.Architecture
	}
	return out, nil
}

// stripHash removes a trailing generated hex id so Longhorn instance managers
// and similar owners group as one workload.
func stripHash(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 {
		return name
	}
	suf := name[i+1:]
	if len(suf) < 8 {
		return name
	}
	for _, r := range suf {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return name
		}
	}
	return name[:i]
}
