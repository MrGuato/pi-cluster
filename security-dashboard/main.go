// security-dashboard: a small SIEM-style page for pi-cluster.
//
// It pulls host events and posture from pisentry, receives Falco alerts on a
// separate ingest port, reads Trivy reports written by a sidecar, flags CVEs
// listed in the CISA KEV catalog, checks pod specs for risky settings and
// lists firing Alertmanager alerts. Everything is served from one binary with
// the page embedded. Standard library only.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed web/index.html
var web embed.FS

const version = "v0.1.1"

func main() {
	fs := flag.NewFlagSet("security-dashboard", flag.ExitOnError)
	listen := fs.String("listen", env("LISTEN", ":8080"), "UI and API address")
	ingest := fs.String("ingest", env("INGEST_LISTEN", ":8081"), "ingest address (Falco http_output, pisentry webhook)")
	dataDir := fs.String("data", env("DATA_DIR", "/data"), "state directory")
	_ = fs.Parse(os.Args[1:])

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("security-dashboard", "version", version, "listen", *listen, "ingest", *ingest, "data", *dataDir)

	store, err := OpenStore(*dataDir, log)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	kube := NewKube(log)
	c := &Collector{
		Store:        store,
		Kube:         kube,
		Log:          log,
		PisentryNS:   env("PISENTRY_NAMESPACE", "pisentry"),
		PisentryPort: env("PISENTRY_PORT", "9817"),
		FalcoNS:      env("FALCO_NAMESPACE", "falco"),
		Alertmanager: env("ALERTMANAGER_URL", "http://kube-prometheus-stack-alertmanager.monitoring.svc.cluster.local:9093"),
		ScanDir:      *dataDir + "/scan",
		KEVURL:       env("KEV_URL", "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"),
	}
	go c.Run(ctx)

	auth := NewAccessAuth(env("CF_ACCESS_TEAM", ""), env("CF_ACCESS_AUD", ""), env("AUTH_MODE", "access"), log)
	api := &API{Store: store, Collector: c, Log: log}

	ui := http.NewServeMux()
	ui.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) }))
	ui.Handle("/", auth.Wrap(api.Routes()))

	in := http.NewServeMux()
	in.HandleFunc("POST /ingest/falco", api.IngestFalco)
	in.HandleFunc("POST /ingest/pisentry", api.IngestPisentry)
	in.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	srvUI := &http.Server{Addr: *listen, Handler: ui, ReadHeaderTimeout: 10 * time.Second}
	srvIn := &http.Server{Addr: *ingest, Handler: in, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 1 << 16}
	errc := make(chan error, 2)
	go func() { errc <- serve(srvUI) }()
	go func() { errc <- serve(srvIn) }()

	select {
	case <-ctx.Done():
	case err := <-errc:
		log.Error("server", "err", err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srvUI.Shutdown(shutdown)
	_ = srvIn.Shutdown(shutdown)
	store.Close()
	log.Info("stopped")
}

func serve(s *http.Server) error {
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: %w", s.Addr, err)
	}
	return nil
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// clientIP returns the peer address without the port, for logs only.
func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}
