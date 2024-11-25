package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"syscall"

	writev1 "github.com/bwplotka/sink/go/sink/genproto/prometheus/v1"
	writev2 "github.com/bwplotka/sink/go/sink/genproto/prometheus/v2"
	"github.com/nelkinda/health-go"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/api/prometheus/v1/remote"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	logLevelFlag := flag.String("log.level", "info", "Logging level, available values: 'debug', 'info', 'warn', 'error'.")
	addr := flag.String("listen-address", ":9011", "Address to listen on. Available HTTP paths: /metrics, /-/ready, /-/health, /sink/prw")
	flag.Parse()

	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(*logLevelFlag)); err != nil {
		println("failed to parse -log.level flag", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	var g run.Group
	{
		healthHandler := health.New(health.Health{}).Handler
		httpSrv := &http.Server{Addr: *addr}
		http.HandleFunc("/-/health", healthHandler)
		http.HandleFunc("/-/ready", healthHandler)
		http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}))
		s := newSink(logger, reg)
		http.HandleFunc(instrumentHandlerFunc(reg, "/sink/prw", remote.NewRemoteWriteHandler(logger, s)))
		g.Add(func() error {
			logger.Info("starting HTTP server", "address", *addr)
			return httpSrv.ListenAndServe()
		}, func(_ error) {
			_ = httpSrv.Shutdown(context.Background())
		})
	}
	g.Add(run.SignalHandler(context.Background(), os.Interrupt, syscall.SIGTERM))

	logger.Info("sink starting...")
	if err := g.Run(); err != nil {
		logger.Error("running sink failed", "err", err)
		os.Exit(1)
	}
	logger.Info("sink finished")
}

type sinkMetrics struct {
	recvData *prometheus.HistogramVec
}

func newSinkMetrics(reg prometheus.Registerer) sinkMetrics {
	s := sinkMetrics{
		recvData: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "sink_received_data_elements",
				// Custom buckets, so key metric is visible in the text format (for testing and local debugging).
				Buckets: []float64{0, 5, 10, 50, 100, 1000, 2000, 10000},

				NativeHistogramBucketFactor: 1.1,
			}, []string{"proto", "data"},
		),
	}
	return s
}

type sink struct {
	sinkMetrics

	logger *slog.Logger
}

func newSink(logger *slog.Logger, reg prometheus.Registerer) *sink {
	return &sink{
		sinkMetrics: newSinkMetrics(reg),
		logger:      logger,
	}
}

func (s *sink) Store(_ context.Context, hdrs http.Header, proto remote.WriteProtoFullName, serializedRequest []byte) (w remote.WriteResponseStats, code int, _ error) {
	s.logger.Debug("received remote write request", slog.String("proto", string(proto)))

	for _, h := range hdrs {
		fmt.Println(h)
	}

	switch proto {
	case remote.WriteProtoFullNameV1:
		r := &writev1.WriteRequest{}
		if err := r.UnmarshalVT(serializedRequest); err != nil {
			return w, http.StatusInternalServerError, fmt.Errorf("decoding v1 request %w", err)
		}
		for _, ts := range r.Timeseries {
			w.Samples += len(ts.Samples)
			w.Histograms += len(ts.Histograms)
			w.Exemplars += len(ts.Exemplars)
		}
		s.recvData.WithLabelValues(string(proto), "series").Observe(float64(len(r.Timeseries)))
	case remote.WriteProtoFullNameV2:
		r := &writev2.Request{}
		if err := r.UnmarshalVT(serializedRequest); err != nil {
			return w, http.StatusInternalServerError, fmt.Errorf("decoding v2 request %w", err)
		}
		for _, ts := range r.Timeseries {
			w.Samples += len(ts.Samples)
			w.Histograms += len(ts.Histograms)
			w.Exemplars += len(ts.Exemplars)
		}
		s.recvData.WithLabelValues(string(proto), "series").Observe(float64(len(r.Timeseries)))
	}

	if w.Samples > 0 {
		s.recvData.WithLabelValues(string(proto), "samples").Observe(float64(w.Samples))
	}
	if w.Histograms > 0 {
		s.recvData.WithLabelValues(string(proto), "histograms").Observe(float64(w.Histograms))
	}
	if w.Exemplars > 0 {
		s.recvData.WithLabelValues(string(proto), "exemplars").Observe(float64(w.Exemplars))
	}
	return w, http.StatusOK, nil
}

func instrumentHandlerFunc(reg prometheus.Registerer, handlerName string, handler http.Handler) (string, http.HandlerFunc) {
	reg = prometheus.WrapRegistererWith(prometheus.Labels{"handler": handlerName}, reg)

	requestDuration := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Tracks the latencies for HTTP requests.",

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"method", "code"},
	)
	requestSize := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_size_bytes",
			Help: "Tracks the size of HTTP requests.",

			// Custom buckets, so key metric is visible in the text format (for testing and local debugging).
			Buckets: []float64{0, 200, 1024, 2048, 10240},

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"method", "code"},
	)
	requestsTotal := promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Tracks the number of HTTP requests.",
		}, []string{"method", "code"},
	)
	responseSize := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_response_size_bytes",
			Help: "Tracks the size of HTTP responses.",

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"method", "code"},
	)

	base := promhttp.InstrumentHandlerRequestSize(
		requestSize,
		promhttp.InstrumentHandlerCounter(
			requestsTotal,
			promhttp.InstrumentHandlerResponseSize(
				responseSize,
				promhttp.InstrumentHandlerDuration(
					requestDuration,
					http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
						handler.ServeHTTP(writer, r)
					}),
				),
			),
		),
	)
	return handlerName, base.ServeHTTP
}
