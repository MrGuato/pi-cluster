# security-dashboard

A small SIEM-style page for pi-cluster. One static Go binary, no
dependencies, page embedded. Companion to `dashboard/` (the public status
page), but this one is private: it shows source IPs, command lines and CVEs.

## What it shows

| Section | Source | How |
| --- | --- | --- |
| Needs attention | all of the below | queue built server side, sorted by severity, with a Reviewed button |
| Hosts | pisentry `/api/events` and `/api/top10` | pulled from every agent pod every 30 s |
| Runtime detection | Falco `http_output` | pushed to the ingest port, grouped by rule over 24 h |
| Image vulnerabilities | Trivy sidecar + CISA KEV | sidecar scans every running image, server flags CVEs in the exploited list |
| Cluster posture | pod specs | privileged, hostPID, hostNetwork, hostIPC, hostPath, added capabilities, `latest` tags |
| Alerts | Alertmanager `/api/v2/alerts` | firing, not silenced or inhibited |
| Event feed | store | last 200 events, filter by source and severity |
| History | store and a daily snapshot | 14 day strip of open items by severity, sparklines of events per day per source, "red for N days" per image |

Events live 30 days in `/data/events.ndjson`; acknowledgements in
`/data/acks.json`. Every 10 minutes the collector writes a snapshot of the
open queue (counts by severity, images with critical fixable CVEs) to
`/data/history.json`, one point per UTC day, 30 days kept. All of it sits
on the Longhorn PVC. Events per day come straight from the store, so the
sparklines fill in from the first start; the strip and the red-day markers
grow one point a day from the first snapshot.

Hosts, Runtime detection, Image vulnerabilities, Cluster posture and the
Event feed start collapsed to a one-line summary with counts. Click the
heading to expand; the choice is kept per section in localStorage.
`?open=all` expands everything for one load, `?theme=light|dark` picks a
theme, both handy for screenshots.

## Ports

* `8080` UI and read API, served through the tunnel only.
* `8081` ingest (`POST /ingest/falco`, `POST /ingest/pisentry`), reachable
  from the falco and pisentry namespaces only (NetworkPolicy).

## Auth

Set `CF_ACCESS_TEAM` and `CF_ACCESS_AUD` and every request to `8080` must
carry a valid Cloudflare Access JWT (header `Cf-Access-Jwt-Assertion` or
the `CF_Authorization` cookie). Signature, issuer, audience and expiry are
checked against the team's JWKS. Leave both empty to run open.

## Local run

```bash
kubectl proxy --port=8001 &
AUTH_MODE=open KUBE_API=http://127.0.0.1:8001 DATA_DIR=/tmp/sd LISTEN=:8090 INGEST_LISTEN=:8091 \
  ALERTMANAGER_URL=http://<alertmanager-clusterip>:9093 ./security-dashboard
```

pisentry pods on other nodes refuse the connection from outside the
cluster (their NetworkPolicy); in the cluster all four report.

## Release

Same flow as cluster-dashboard: build on kubepi, push, bump the tag in
`apps/base/security-dashboard/deployment.yaml`, PR, merge.

```bash
docker buildx build --builder pisentry-multi --platform linux/arm64 --load \
  -t ghcr.io/mrguato/security-dashboard:vX.Y.Z security-dashboard/
docker push ghcr.io/mrguato/security-dashboard:vX.Y.Z
```

Add `,linux/amd64` to the platform list and drop `--load` for a
multi-arch push. Bump `version` in `main.go` with the tag.
