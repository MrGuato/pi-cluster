[![Made With Love](https://img.shields.io/badge/Made%20with%20%E2%9D%A4%EF%B8%8F-by%20Jonathan-red)](https://github.com/MrGuato)
![GitHub last commit](https://img.shields.io/github/last-commit/MrGuato/pi-cluster?style=flat-square)
![GitHub repo size](https://img.shields.io/github/repo-size/MrGuato/pi-cluster?style=flat-square)
![GitHub commit activity](https://img.shields.io/github/commit-activity/m/MrGuato/pi-cluster?style=flat-square)
![Kubernetes](https://img.shields.io/badge/Kubernetes-k3s-blue?style=flat-square&logo=kubernetes)
![GitOps](https://img.shields.io/badge/GitOps-FluxCD-blueviolet?style=flat-square)
![Architecture](https://img.shields.io/badge/Arch-ARM64-success?style=flat-square)
![Cluster](https://img.shields.io/badge/Cluster-Raspberry%20Pi-red?style=flat-square)
![Secrets](https://img.shields.io/badge/Secrets-SOPS%2Fage-yellow?style=flat-square)
![Tunnel](https://img.shields.io/badge/Ingress-Cloudflare%20Tunnel-orange?style=flat-square&logo=cloudflare)
![Monitoring](https://img.shields.io/badge/Monitoring-Prometheus%20%2B%20Grafana-informational?style=flat-square&logo=grafana)
 
# pi-cluster
**Raspberry Pi k3s Kubernetes Cluster - GitOps with FluxCD**
 
A **production-minded** single-node Kubernetes cluster running on a Raspberry Pi 4 (ARM64), managed entirely via **GitOps** with **FluxCD**. Every workload is declared in Git, secrets are encrypted with **SOPS/age**, and external access is handled through a **Cloudflare Tunnel** - no ports forwarded, no manual `kubectl apply`.
 
This is not a toy homelab. It's a serious learning and operational environment built to mirror real-world platform engineering practices on minimal hardware.
 
> _If it's not committed, it doesn't exist._
 
---
 
## Project Goals
 
- Learn **Kubernetes the right way** - declarative, GitOps-first, auditable
- Practice **platform engineering** on constrained hardware
- Run **real, useful workloads** for personal productivity and knowledge management
- Build production-style workflows: IaC, secrets management, observability, CD
- Treat infrastructure as a **craft**, not a checklist
 
Inspired by **Mischa van den Burg** and real-world platform engineering practices.
 
---
 
## Core Principles
 
- **Git is the source of truth** - no snowflakes, no manual state
- No manual `kubectl apply` for workloads - FluxCD drives everything
- Declarative over imperative, always
- Secrets never touch Git in plaintext - encrypted via SOPS/age
- Reproducible, auditable cluster state at every commit
- Small cluster, production mindset
 
---
 
## Cluster Overview
 
| Component       | Details                              |
|----------------|--------------------------------------|
| **Hardware**    | Raspberry Pi 4 (ARM64)              |
| **OS**          | Raspberry Pi OS / Ubuntu (ARM64)    |
| **Kubernetes**  | k3s (lightweight single-node)       |
| **GitOps**      | FluxCD                               |
| **Secrets**     | SOPS + age encryption                |
| **Ingress**     | Cloudflare Tunnel (zero-trust, no open ports) |
| **Monitoring**  | kube-prometheus-stack (Prometheus + Grafana + Alertmanager) |
| **Management**  | Remote - SSH + kubectl               |
 
---
 
## Workloads & Service Status
 
| Service | Category | Status | Notes |
|---|---|---|---|
| 🔗 **linkding** | Bookmarks | ✅ Running | Self-hosted bookmark manager |
| 🧠 **obsidian-sync** | Knowledge Sync | ✅ Running | CouchDB backend for Obsidian LiveSync |
| ✅ **Vikunja** | Task Management | ✅ Running | Self-hosted task manager with Postgres backend |
| 📊 **kube-prometheus-stack** | Observability | 🔄 In Progress | Prometheus, Grafana, Alertmanager |
| 🤖 **Renovate** | Automation | ✅ Running | Automated dependency updates via CronJob |
| 🔐 **SOPS/age** | Secrets | ✅ Operational | All secrets encrypted at rest in Git |
| 🌐 **Cloudflare Tunnel** | Networking | ✅ Operational | Zero-trust external access, no forwarded ports |
 
**Status key:** ✅ Running · 🔄 In Progress · 🧪 Planned · ⚠️ Degraded
 
---
 
## Secrets Management
 
All secrets in this repository are encrypted using **[SOPS](https://github.com/getsops/sops)** with **age** as the key provider. Encrypted secret files are committed directly to Git and decrypted at reconcile time by FluxCD's native SOPS integration.
 
- No plaintext secrets ever touch the repository
- Age private key is stored only on the cluster node
- FluxCD's `sops-secrets` Kustomize controller handles decryption automatically
 
---
 
## Networking
 
External access is handled entirely via a **Cloudflare Tunnel** - the cluster has zero inbound ports open. Services are exposed selectively through Cloudflare's zero-trust edge.
 
- No dynamic DNS, no NAT hairpinning, no open firewall rules
- TLS is handled at the Cloudflare edge
- Internal cluster traffic routes through k3s/Traefik as normal
 
---
 
## Observability
 
The cluster is instrumented with **kube-prometheus-stack**, providing:
 
- **Prometheus** - metrics scraping and storage
- **Grafana** - dashboards and visualization
- **Alertmanager** - alerting pipeline
 
> Actively being built out. Dashboards and alert rules are a work in progress.
 
---
 
## Repository Structure
 
```text
.
├── apps/
│   ├── base/
│   │   ├── linkding/
│   │   │   ├── deployment.yaml
│   │   │   ├── kustomization.yaml
│   │   │   ├── namespace.yaml
│   │   │   ├── pvc.yaml
│   │   │   └── service.yaml
│   │   ├── obsidian-sync/          # CouchDB backend for Obsidian LiveSync
│   │   │   ├── configmap.yaml
│   │   │   ├── deployment.yaml
│   │   │   ├── kustomization.yaml
│   │   │   ├── namespace.yaml
│   │   │   ├── pvc.yaml
│   │   │   └── service.yaml
│   │   └── vikunja/
│   │       ├── configmap.yaml
│   │       ├── deployment.yaml
│   │       ├── kustomization.yaml
│   │       ├── namespace.yaml
│   │       ├── postgres.yaml
│   │       ├── pvc.yaml
│   │       └── service.yaml
│   └── staging/                    # Environment overlays + Cloudflare ingress
│       ├── linkding/
│       ├── obsidian-sync/
│       └── vikunja/
│
├── clusters/staging/
│   ├── flux-system/                # Flux bootstrap manifests
│   ├── .sops.yaml
│   ├── apps.yaml
│   ├── infrastructure.yaml
│   └── monitoring.yaml
│
├── infrastructure/controllers/
│   ├── base/renovate/              # Renovate CronJob for automated updates
│   └── staging/renovate/
│
├── monitoring/
│   ├── configs/staging/kube-prometheus-stack/
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
Git commit → FluxCD detects drift → Reconcile → Cluster state updated
```
 
1. All changes are made via **Pull Request or direct commit** to `main`
2. FluxCD polls the repository on a defined interval
3. On drift detection, FluxCD reconciles the live cluster state to match Git
4. Secrets are decrypted in-cluster at reconcile time via SOPS
 
No `kubectl apply`. No manual state. Git is the only interface.
 
---
 
## Inspiration & References
 
- **[Kubecraft](https://github.com/mishabrukman/kubecraft)** - Misha Brukman's production-minded homelab methodology
- **[FluxCD Docs](https://fluxcd.io/flux/)** - GitOps toolkit
- **[SOPS](https://github.com/getsops/sops)** - Secret encryption for GitOps
- **[k3s](https://k3s.io/)** - Lightweight Kubernetes for constrained hardware
- **[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)** - Zero-trust ingress without open ports
 
---
 
## About
 
Built and maintained by **[Jonathan (MrGuato)](https://github.com/MrGuato)** - Lead Infrastructure & Cybersecurity Engineer, homelab practitioner, and believer that the best way to understand production systems is to build them yourself, even on a $75 piece of hardware.
 
Related projects:
- **[gitops-notes](https://github.com/MrGuato/gitops-notes)** - Astro-based DevOps blog documenting this journey
- **[enshrouded-docker](https://github.com/MrGuato/enshrouded-docker)** - Dockerized Enshrouded dedicated server (Wine + SteamCMD)
