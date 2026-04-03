[![Made With Love](https://img.shields.io/badge/Made%20with%20%E2%9D%A4%EF%B8%8F-by%20Jonathan-red)](https://github.com/MrGuato)
![GitHub last commit](https://img.shields.io/github/last-commit/MrGuato/pi-cluster?style=flat-square)
![GitHub repo size](https://img.shields.io/github/repo-size/MrGuato/pi-cluster?style=flat-square)
![GitHub commit activity](https://img.shields.io/github/commit-activity/m/MrGuato/pi-cluster?style=flat-square)
![Kubernetes](https://img.shields.io/badge/Kubernetes-k3s-blue?style=flat-square&logo=kubernetes)
![GitOps](https://img.shields.io/badge/GitOps-FluxCD-blueviolet?style=flat-square)
![Architecture](https://img.shields.io/badge/Arch-ARM64%20%2B%20x86__64-success?style=flat-square)
![Storage](https://img.shields.io/badge/Storage-Longhorn-blue?style=flat-square)
![Secrets](https://img.shields.io/badge/Secrets-SOPS%2Fage-yellow?style=flat-square)
![Tunnel](https://img.shields.io/badge/Ingress-Cloudflare%20Tunnel-orange?style=flat-square&logo=cloudflare)
![Monitoring](https://img.shields.io/badge/Monitoring-Prometheus%20%2B%20Grafana-informational?style=flat-square&logo=grafana)
![Status](https://img.shields.io/badge/Status-Live-brightgreen?style=flat-square)

# pi-cluster
**Mixed-Architecture k3s Kubernetes Cluster - GitOps with FluxCD**

A **production-minded** Kubernetes cluster running across Raspberry Pi 4 (ARM64) and Lenovo ThinkStation (x86_64) nodes, managed entirely via **GitOps** with **FluxCD**. Every workload is declared in Git, secrets are encrypted with **SOPS/age**, distributed storage is handled by **Longhorn**, and external access runs through **Cloudflare Tunnels** with zero open ports.

This is not a toy homelab. It is a serious learning and operational environment built to mirror real-world platform engineering practices across heterogeneous hardware and operating systems.

> _If it is not in Git, it does not exist._

**Live status dashboard:** [status.deleontech.net](https://status.deleontech.net)

---

## Project Goals

- Learn **Kubernetes the right way** - declarative, GitOps-first, auditable
- Practice **platform engineering** on constrained and mixed hardware
- Run **real, useful workloads** for personal productivity and knowledge management
- Build production-style workflows: IaC, secrets management, observability, distributed storage, CD
- Operate a **mixed-architecture, mixed-OS cluster** the way enterprises do
- Treat infrastructure as a **craft**, not a checklist

Inspired by **[Mischa van den Burg](https://mischavandenburg.com/)** and real-world platform engineering practices.

---

## Core Principles

- **Git is the source of truth** - no snowflakes, no manual state
- No manual `kubectl apply` for workloads - FluxCD drives everything
- Declarative over imperative, always
- Secrets never touch Git in plaintext - encrypted via SOPS/age
- Reproducible, auditable cluster state at every commit
- Document the failures, not just the happy path

---

## Cluster Overview

| Component        | Details                                                        |
|-----------------|----------------------------------------------------------------|
| **Hardware**     | 3x Raspberry Pi 4 (ARM64) + 1x Lenovo ThinkStation (x86_64)  |
| **OS**           | Debian 13 (Pi nodes) + RHEL 10.1 (ThinkStation)               |
| **Kubernetes**   | k3s v1.34.6                                                    |
| **GitOps**       | FluxCD                                                         |
| **Secrets**      | SOPS + age encryption                                          |
| **Storage**      | Longhorn (distributed) + local-path                            |
| **Ingress**      | Cloudflare Tunnels (zero-trust, no open ports)                 |
| **Monitoring**   | kube-prometheus-stack (Prometheus + Grafana)                    |
| **Backups**      | Velero + MinIO                                                  |
| **Deps**         | Renovate Bot (CronJob)                                          |

---

## Cluster Nodes

| Node | Hardware | RAM | Arch | OS | Role |
|------|----------|-----|------|----|------|
| `kubepi` | Raspberry Pi 4 | 8GB | ARM64 | Debian 13 | Control plane |
| `kubepi-w1` | Raspberry Pi 4 | 4GB | ARM64 | Debian 13 | Worker + Longhorn storage |
| `kubepi-w2` | Raspberry Pi 4 | 2GB | ARM64 | Debian 13 | Worker (compute only) |
| `kubethink` | Lenovo ThinkStation | 8GB | x86_64 | RHEL 10.1 | Worker + primary Longhorn storage |

**Storage topology:** Longhorn replicates volumes across `kubethink` (2TB Samsung T7 Shield at `/mnt/longhorn-storage/`) and `kubepi-w1` (SD card at `/var/lib/longhorn/`). Default replica count is 2. The 2GB Pi and control plane node are excluded from storage scheduling.

---

## Deployed Services

| Service | Category | Namespace | Status |
|---------|----------|-----------|--------|
| **Linkding** | Bookmarks | linkding | Running |
| **Obsidian LiveSync** | Knowledge Sync | obsidian-sync | Running |
| **Vikunja** | Task Management | vikunja | Running |
| **Grafana** | Dashboards | monitoring | Running |
| **Prometheus** | Metrics | monitoring | Running |
| **Longhorn** | Distributed Storage | longhorn-system | Running |
| **Velero** | Backups | velero | Running |
| **Renovate** | Dependency Updates | renovate | Running |
| **Cloudflare Tunnels** | Ingress | per-namespace | Running |
| **Status Dashboard** | Monitoring | cluster-dashboard | Running |

---

## Architecture Decisions

**Mixed architecture (ARM64 + x86_64):** Single-arch images are pinned to the correct nodes via `nodeSelector`. Multi-arch images schedule freely. This mirrors real enterprise clusters where hardware is rarely uniform.

**Mixed OS (Debian + RHEL):** RHEL requires explicit preparation (firewalld disabled, SELinux permissive, kernel modules loaded) before joining a k3s cluster. Running both OS families is intentional as a learning exercise in cross-platform Kubernetes operations.

**Longhorn over NFS:** Longhorn provides Kubernetes-native distributed block storage with dynamic provisioning, replication, and snapshot/restore. NFS would have been easier but teaches nothing transferable to enterprise environments. Longhorn is CNCF-incubating and the concepts map directly to Portworx, Rook-Ceph, and cloud CSI drivers.

**local-path remains the default StorageClass.** Longhorn is opt-in via `storageClassName: longhorn`. Existing workloads are unaffected. This avoids surprise migrations.

**One Cloudflare Tunnel deployment per namespace.** Each app gets its own tunnel with dedicated credentials. Namespace isolation is maintained from Git to the edge.

---

## Secrets Management

All secrets are encrypted using **[SOPS](https://github.com/getsops/sops)** with **age** as the key provider. Encrypted secret files are committed directly to Git and decrypted at reconcile time by FluxCD's native SOPS integration.

- No plaintext secrets ever touch the repository
- Age private key is stored only on the cluster node
- `.sops.yaml` config lives at `clusters/staging/` (not repo root), requiring `--config` flag for manual operations
- Workflow: decrypt to `/tmp/` -> edit -> re-encrypt -> delete plaintext -> commit

---

## Networking

External access is handled entirely via **Cloudflare Tunnels**. The cluster has zero inbound ports open.

- No dynamic DNS, no NAT hairpinning, no open firewall rules
- TLS terminates at the Cloudflare edge
- Cloudflare Access provides OTP-based authentication for sensitive services
- Internal cluster traffic routes through k3s/Traefik
- Each namespace runs its own `cloudflared` deployment with dedicated credentials

---

## Observability

The cluster is instrumented with **kube-prometheus-stack**:

- **Prometheus** scrapes metrics from all nodes, pods, and Longhorn volumes
- **Grafana** provides dashboards (currently internal-only via port-forward)
- **Longhorn ServiceMonitor** feeds storage metrics into Prometheus
- **Custom status dashboard** at [status.deleontech.net](https://status.deleontech.net) shows live node metrics, service health, and cluster topology via a metrics sidecar querying Prometheus

---

## Repository Structure

```text
.
├── apps/
│   ├── base/                       # Environment-neutral manifests
│   │   ├── cluster-dashboard/
│   │   ├── linkding/
│   │   ├── obsidian-sync/
│   │   └── vikunja/
│   └── staging/                    # Overlays, secrets, Cloudflare tunnels
│       ├── cluster-dashboard/
│       ├── linkding/
│       ├── obsidian-sync/
│       └── vikunja/
│
├── clusters/staging/
│   ├── flux-system/                # Flux bootstrap manifests
│   ├── .sops.yaml                  # SOPS config (note: not at repo root)
│   ├── apps.yaml
│   ├── infrastructure.yaml
│   └── monitoring.yaml
│
├── dashboard/                      # Status dashboard source + Dockerfile
│   ├── Dockerfile
│   └── index.html
│
├── infrastructure/controllers/
│   ├── base/
│   │   ├── longhorn/               # Longhorn distributed storage (Helm)
│   │   ├── renovate/               # Renovate CronJob
│   │   └── velero/                 # Velero backup (Helm)
│   └── staging/
│       ├── longhorn/
│       ├── renovate/
│       └── velero/
│
├── monitoring/
│   ├── configs/staging/
│   └── controllers/
│       ├── base/kube-prometheus-stack/
│       └── staging/kube-prometheus-stack/
│
├── renovate.json
└── README.md
```

---

## GitOps Workflow

```
Git Push → FluxCD Reconcile → SOPS Decrypt → k3s Apply → Cloudflare Tunnel
```

1. All changes are made via commit to `main`
2. FluxCD polls the repository on a defined interval
3. On drift detection, FluxCD reconciles the live cluster state to match Git
4. Secrets are decrypted in-cluster at reconcile time via SOPS/age
5. Cloudflare Tunnels expose services at the edge with zero-trust authentication

No `kubectl apply`. No manual state. Git is the only interface.

---

## Hard-Won Lessons

These are things that broke and what I learned from debugging them.

- **`resource:` vs `resources:`** in Kustomize - the singular form is silently ignored. Flux reconciles against an empty resource list with no error.
- **CouchDB on ARM64** crashes silently with restrictive `securityContext`. Remove it entirely. Configure CORS via API rather than ConfigMap mounts.
- **`nginx:alpine` crashes with `readOnlyRootFilesystem` + `runAsNonRoot`** - use `nginxinc/nginx-unprivileged` instead.
- **USB external SSDs may not power on via Raspberry Pi USB ports.** The Pi 4 USB bus has limited current delivery. Use a powered hub or move the drive to proper hardware.
- **RHEL requires explicit prep for k3s:** disable firewalld, set SELinux to permissive, load `br_netfilter` and `overlay` kernel modules, set sysctl for bridge networking.
- **open-iscsi must be installed on every node** where Longhorn's daemonset runs. Missing it causes silent crash-loops.
- **Mixed-arch clusters need architecture-aware scheduling.** Images built on ARM64 will `ImagePullBackOff` on x86_64 with "no match for platform in manifest". Pin with `nodeSelector` or build multi-arch.
- **A single malformed YAML line can block an entire Flux kustomization** - and every sibling resource in it. Validate before committing.
- **Longhorn disk annotation must be applied before the create-default-disk label**, or Longhorn creates a default disk at `/var/lib/longhorn/` before seeing your custom path.
- **Flux reconcile hangs** can be SSH port 22 timeouts to GitHub. The commit may have succeeded despite the hang.
- **GHCR images must be public** if the source repo is public and no `imagePullSecret` is configured.

---

## Inspiration & References

- **[Mischa van den Burg](https://mischavandenburg.com/)** - Microsoft MVP, Kubestronaut, and founder of [KubeCraft](https://github.com/mischavandenburg). His homelab approach and GitOps-first philosophy directly inspired how this cluster is built and operated.
- **[FluxCD Docs](https://fluxcd.io/flux/)** - GitOps toolkit
- **[Longhorn Docs](https://longhorn.io/docs/)** - Distributed block storage for Kubernetes
- **[SOPS](https://github.com/getsops/sops)** - Secret encryption for GitOps
- **[k3s](https://k3s.io/)** - Lightweight Kubernetes
- **[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)** - Zero-trust ingress

---

## About

Built and maintained by **[Jonathan (MrGuato)](https://github.com/MrGuato)** - Infrastructure and Cybersecurity professional (CISM), homelab practitioner, and believer that the best way to understand production systems is to build them yourself.

Related projects:
- **[gitops-notes](https://github.com/MrGuato/gitops-notes)** - Astro-based DevOps blog documenting this journey
- **[enshrouded-docker](https://github.com/MrGuato/enshrouded-docker)** - Dockerized Enshrouded dedicated server (Wine + SteamCMD)

<details>
<summary>Cluster Quick Reference</summary>

```bash
# Check Flux reconciliation status
flux get kustomizations

# Watch all pods across namespaces
kubectl get pods -A

# Force Flux to reconcile immediately
flux reconcile kustomization apps --with-source

# Check Longhorn volumes and replicas
kubectl get volumes.longhorn.io -n longhorn-system

# Check Longhorn node storage
kubectl get nodes.longhorn.io -n longhorn-system

# Decrypt a secret for inspection (requires age key)
SOPS_AGE_KEY_FILE=./age.agekey sops --config clusters/staging/.sops.yaml -d apps/staging/vikunja/secret.yaml
```

</details>
