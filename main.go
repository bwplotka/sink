package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var listenAddress = flag.String("listen-address", ":19091",
	"Address on which to expose metrics and the Remote Write handler.")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	metrics := prometheus.NewRegistry()
	metrics.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	client := openfeature.NewClient("what")
	// client.String()

	var g run.Group
	{
		term := make(chan os.Signal, 1)
		cancel := make(chan struct{})
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)

		g.Add(
			func() error {
				select {
				case <-term:
					logger.Info("received SIGTERM, exiting gracefully...")
				case <-cancel:
				}
				return nil
			},
			func(err error) {
				close(cancel)
			},
		)
	}
	{
		ver, err := export.Version()
		if err != nil {
			level.Error(logger).Log("msg", "detect version", "err", err)
			os.Exit(1)
		}

		env := setup.UAEnvUnspecified
		// Default target fields if we can detect them in GCP.
		if metadata.OnGCE() {
			env = setup.UAEnvGCE
			cluster, _ := metadata.InstanceAttributeValue("cluster-name")
			if cluster != "" {
				env = setup.UAEnvGKE
			}
		}

		// Identity User Agent for all gRPC requests.
		ua := strings.TrimSpace(fmt.Sprintf("%s/%s %s (env:%s;mode:%s)",
			"prometheus-engine-prw2gcm", ver, "prw2-gcm", env, "unspecified"))

		ctx, cancel := context.WithCancel(context.Background())
		client, err := newGCMClient(ctx, *gcmEndpoint, ua, *credentialsFile)
		if err != nil {
			defer cancel()
			level.Error(logger).Log("msg", "create GCM client", "err", err)
			os.Exit(1)
		}

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(metrics, promhttp.HandlerOpts{Registry: metrics}))
		mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "prw2gcm is Healthy.\n")
		})
		mux.HandleFunc("/-/ready", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "prw2gcm is Ready.\n")
		})
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		registerRWHandler(mux, client)
		server := &http.Server{
			Handler: mux,
			Addr:    *listenAddress,
		}

		g.Add(func() error {
			level.Info(logger).Log("msg", "Starting web server for metrics", "listen", *listenAddress)
			return server.ListenAndServe()
		}, func(err error) {
			ctx, _ = context.WithTimeout(ctx, time.Minute)
			server.Shutdown(ctx)
			cancel()
		})
	}

	if err := g.Run(); err != nil {
		level.Error(logger).Log("msg", "running reloader failed", "err", err)
		os.Exit(1)
	}
}
