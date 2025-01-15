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

const (
	seriesData     = "series"
	samplesData    = "samples"
	histogramsData = "histograms"
	exemplarsData  = "exemplars"

	seriesWithoutTypeIssue   = "series-untyped"
	seriesWithoutHelpIssue   = "series-without-help"
	seriesWithoutUnitIssue   = "series-without-unit"
	cumulativeWithoutCTIssue = "cumulative-without-ct"

	sourceHeader = "X-SINK-SOURCE"
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
		http.HandleFunc("/sink/prw",
			detectSource(instrument(reg, "/sink/prw", remote.NewRemoteWriteHandler(logger, s))))
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
	recvData       *prometheus.HistogramVec
	recvDataIssues *prometheus.HistogramVec
}

func newSinkMetrics(reg prometheus.Registerer) sinkMetrics {
	sources := map[string]struct{}{
		"unknown": {},
	}
	sourcesValuesLimit := 10
	sourceConstraintFn := func(v string) string {
		if _, ok := sources[v]; ok {
			return v
		}
		if len(sources) < sourcesValuesLimit {
			sources[v] = struct{}{}
			return v
		}
		return "other"
	}

	protoConstraintFn := func(v string) string {
		switch remote.WriteProtoFullName(v) {
		case remote.WriteProtoFullNameV1, remote.WriteProtoFullNameV2:
			return v
		default:
			return "unknown"
		}
	}

	s := sinkMetrics{
		recvData: prometheus.V2.NewHistogramVec(
			prometheus.HistogramVecOpts{
				HistogramOpts: prometheus.HistogramOpts{
					Name: "sink_received_data_elements",
					// Custom buckets, so the key metrics are visible in the text format (for testing and local debugging).
					Buckets:                     []float64{0, 5, 10, 50, 100, 1000, 2000, 10000},
					NativeHistogramBucketFactor: 1.1,

					Help: "Histogram of elements in each category like series, samples, histograms and exemplars per the received request, unrelated if broken e.g. without type.",
				},
				VariableLabels: prometheus.ConstrainedLabels{
					{Name: "source", Constraint: sourceConstraintFn},
					{Name: "proto", Constraint: protoConstraintFn},
					{Name: "data", Constraint: func(v string) string {
						switch v {
						case seriesData, samplesData, histogramsData, exemplarsData:
							return v
						default:
							return "unknown"
						}
					}},
				},
			},
		),
		recvDataIssues: prometheus.V2.NewHistogramVec(
			prometheus.HistogramVecOpts{
				HistogramOpts: prometheus.HistogramOpts{
					Name: "sink_received_data_issues",
					// Custom buckets, so the key metrics are visible in the text format (for testing and local debugging).
					Buckets:                     []float64{0, 5, 10, 50, 100, 1000, 2000, 10000},
					NativeHistogramBucketFactor: 1.1,

					Help: "Histogram of instances of certain issues or problems per the received request. Some issues are not required by the spec (e.g. optional items like CTs).",
				},
				VariableLabels: prometheus.ConstrainedLabels{
					{Name: "source", Constraint: sourceConstraintFn},
					{Name: "proto", Constraint: protoConstraintFn},
					{Name: "issue", Constraint: func(v string) string {
						switch v {
						case seriesWithoutTypeIssue, seriesWithoutHelpIssue, seriesWithoutUnitIssue, cumulativeWithoutCTIssue:
							return v
						default:
							return "unknown"
						}
					}},
				},
			},
		),
	}
	reg.MustRegister(s.recvData, s.recvDataIssues)
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

type sourceCtxKey struct{}

var key sourceCtxKey

func detectSource(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if got := r.Header.Get(sourceHeader); got != "" {
			ctx = context.WithValue(ctx, key, got)
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	}
}

func getSource(ctx context.Context) string {
	ret, ok := ctx.Value(key).(string)
	if !ok {
		return "unknown"
	}
	return ret
}

func (s *sink) Store(ctx context.Context, proto remote.WriteProtoFullName, serializedRequest []byte) (w remote.WriteResponseStats, code int, _ error) {
	source := getSource(ctx)

	var (
		series               int
		encoded              fmt.Stringer
		noTypeSeries         int
		noHelpSeries         int
		noUnitSeries         int
		noCTCumulativeSeries int
	)
	switch proto {
	case remote.WriteProtoFullNameV1:
		r := &writev1.WriteRequest{}
		if err := r.UnmarshalVT(serializedRequest); err != nil {
			return w, http.StatusInternalServerError, fmt.Errorf("decoding v1 request %w", err)
		}
		encoded = r
		series = len(r.Timeseries)
		for _, ts := range r.Timeseries {
			w.Samples += len(ts.Samples)
			w.Histograms += len(ts.Histograms)
			w.Exemplars += len(ts.Exemplars)
		}

	case remote.WriteProtoFullNameV2:
		r := &writev2.Request{}
		if err := r.UnmarshalVT(serializedRequest); err != nil {
			return w, http.StatusInternalServerError, fmt.Errorf("decoding v2 request %w", err)
		}
		encoded = r
		series = len(r.Timeseries)
		for _, ts := range r.Timeseries {
			w.Samples += len(ts.Samples)
			w.Histograms += len(ts.Histograms)
			w.Exemplars += len(ts.Exemplars)

			if ts.Metadata.HelpRef == 0 {
				noHelpSeries++
			}
			if ts.Metadata.UnitRef == 0 {
				noUnitSeries++
			}
			if ts.Metadata.Type == writev2.Metadata_METRIC_TYPE_UNSPECIFIED {
				noTypeSeries++
			} else if ts.Metadata.Type == writev2.Metadata_METRIC_TYPE_COUNTER ||
					ts.Metadata.Type == writev2.Metadata_METRIC_TYPE_HISTOGRAM ||
					ts.Metadata.Type == writev2.Metadata_METRIC_TYPE_SUMMARY {
				if ts.CreatedTimestamp == 0 {
					noCTCumulativeSeries++
				}
			}
		}
	default:
		return w, http.StatusInternalServerError, fmt.Errorf("expected proto full names validated; got unknown one %v", proto)
	}

	s.recvData.WithLabelValues(source, string(proto), seriesData).Observe(float64(series))
	if w.Samples > 0 {
		s.recvData.WithLabelValues(source, string(proto), samplesData).Observe(float64(w.Samples))
	}
	if w.Histograms > 0 {
		s.recvData.WithLabelValues(source, string(proto), histogramsData).Observe(float64(w.Histograms))
	}
	if w.Exemplars > 0 {
		s.recvData.WithLabelValues(source, string(proto), exemplarsData).Observe(float64(w.Exemplars))
	}
	if noTypeSeries > 0 {
		s.recvDataIssues.WithLabelValues(source, string(proto), seriesWithoutTypeIssue).Observe(float64(noTypeSeries))
	}
	if noHelpSeries > 0 {
		s.recvDataIssues.WithLabelValues(source, string(proto), seriesWithoutHelpIssue).Observe(float64(noHelpSeries))
	}
	if noUnitSeries > 0 {
		s.recvDataIssues.WithLabelValues(source, string(proto), seriesWithoutUnitIssue).Observe(float64(noUnitSeries))
	}
	if noCTCumulativeSeries > 0 {
		s.recvDataIssues.WithLabelValues(source, string(proto), cumulativeWithoutCTIssue).Observe(float64(noCTCumulativeSeries))
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		// Converting message to string is expensive, do this only if debug is actually enabled.
		s.logger.Debug(
			"received remote write request",
			slog.String("source", source),
			slog.String("proto", string(proto)),
			slog.Int(seriesData, series),
			slog.Int(samplesData, w.Samples),
			slog.Int(histogramsData, w.Histograms),
			slog.Int(exemplarsData, w.Exemplars),
			slog.Int(seriesWithoutTypeIssue, noTypeSeries),
			slog.Int(seriesWithoutHelpIssue, noHelpSeries),
			slog.Int(seriesWithoutUnitIssue, noUnitSeries),
			slog.Int(cumulativeWithoutCTIssue, noCTCumulativeSeries),
			slog.String("decoded", encoded.String()),
		)
	}
	return w, http.StatusOK, nil
}

func instrument(reg prometheus.Registerer, handlerName string, handler http.Handler) http.HandlerFunc {
	reg = prometheus.WrapRegistererWith(prometheus.Labels{"handler": handlerName}, reg)

	requestDuration := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Tracks the latencies for HTTP requests.",

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"source", "method", "code"},
	)
	requestSize := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_size_bytes",
			Help: "Tracks the size of HTTP requests.",

			// Custom buckets, so key metric is visible in the text format (for testing and local debugging).
			Buckets: []float64{0, 200, 1024, 2048, 10240},

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"source", "method", "code"},
	)
	requestsTotal := promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Tracks the number of HTTP requests.",
		}, []string{"source", "method", "code"},
	)
	responseSize := promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_response_size_bytes",
			Help: "Tracks the size of HTTP responses.",

			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"source", "method", "code"},
	)

	opt := promhttp.WithLabelFromCtx("source", getSource)

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
					opt,
				),
				opt,
			),
			opt,
		),
		opt,
	)
	return base.ServeHTTP
}
