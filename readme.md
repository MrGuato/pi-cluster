[![Made With Love](https://img.shields.io/badge/Made%20with%20%E2%9D%A4%EF%B8%8F-by%20Jonathan-red)](https://github.com/MrGuato)
![GitHub last commit](https://img.shields.io/github/last-commit/MrGuato/pi-cluster?style=flat-square)
![GitHub repo size](https://img.shields.io/github/repo-size/MrGuato/pi-cluster?style=flat-square)
![GitHub commit activity](https://img.shields.io/github/commit-activity/m/MrGuato/pi-cluster?style=flat-square)
<<<<<<< HEAD

=======
![Kubernetes](https://img.shields.io/badge/Kubernetes-k3s-blue?style=flat-square&logo=kubernetes)
![GitOps](https://img.shields.io/badge/GitOps-FluxCD-blueviolet?style=flat-square)
![Architecture](https://img.shields.io/badge/Arch-ARM64-success?style=flat-square)
![Cluster](https://img.shields.io/badge/Cluster-Raspberry%20Pi-red?style=flat-square)
>>>>>>> 89a7e46 (feat: add badges to readme)

# pi-cluster  
**Raspberry Pi k3s Kubernetes Cluster — GitOps with FluxCD**

This repository tracks the full lifecycle of my **Raspberry Pi–based Kubernetes cluster**, managed entirely using **GitOps** principles with **FluxCD**.

This is not a “toy” home lab — it’s a **serious learning environment** designed to mirror how production Kubernetes clusters are built, operated, and evolved.

> If it’s not committed, it doesn’t exist.

---

## Project Goals

This project exists to:

- Learn **Kubernetes the right way**
- Practice **GitOps-first cluster management**
- Build production-style workflows on minimal hardware
- Treat a Raspberry Pi cluster like a real platform, not a sandbox

Inspired by:
- **Kubecraft / Misha Brukman**
- Real-world platform engineering practices
- Git-driven infrastructure and automation

---

## Core Principles

- **Git is the source of truth**
- No manual `kubectl apply` for workloads
- Declarative over imperative
- Reproducible, auditable cluster state
- Small cluster, production mindset

---

## Cluster Overview

- **Hardware:** Raspberry Pi (ARM64)
- **Kubernetes:** k3s
- **GitOps Engine:** FluxCD
- **Ingress:** Traefik (k3s default)
- **Workloads:** Deployed only via Git
- **Management:** Remote (SSH + kubectl)

---

##  Repository Structure

```text
.
├── clusters/
│   └── staging/
│       ├── flux-system/        # Flux bootstrap manifests
│       └── apps.yaml           # Flux Kustomization for apps
│
├── apps/
│   ├── base/                   # Base app definitions
│   │   └── linkding/
│   │       ├── namespace.yaml
│   │       ├── deployment.yaml
│   │       ├── service.yaml
│   │       └── kustomization.yaml
│   │
│   └── staging/                # Environment overlays
│       └── linkding/
│           └── kustomization.yaml
│
└── README.md

