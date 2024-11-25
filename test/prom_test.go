package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	"github.com/efficientgo/e2e/monitoring/matchers"
	"github.com/prometheus/client_golang/api/prometheus/v1/remote"
)

const (
	sinkImage = "quay.io/bwplotka/sink:latest"
)

// Requires make docker DOCKER_TAG=latest before starting.
func TestSink_PrometheusWriting(t *testing.T) {
	e, err := e2e.New()
	t.Cleanup(e.Close)
	testutil.Ok(t, err)

	// Create sink.
	sink := newSink(e, "sink-1", sinkImage, nil)
	// Create self-scraping Prometheus writing two streams of PRW writes to sink-1: v1 and v2.
	prom := newPrometheus(e, "prom-1", "quay.io/prometheus/prometheus:v3.0.0", fmt.Sprintf("http://%s/sink/prw", sink.InternalEndpoint("http")), nil)
	testutil.Ok(t, e2e.StartAndWaitReady(sink, prom))

	const expectSamples float64 = 2e3

	testutil.Ok(t, prom.WaitSumMetricsWithOptions(
		e2emon.Greater(expectSamples), []string{"prometheus_remote_storage_samples_total"},
		e2emon.WithLabelMatchers(&matchers.Matcher{Name: "remote_name", Value: "v2-to-sink", Type: matchers.MatchEqual}),
		e2emon.WithWaitBackoff(&backoff.Config{Min: 1 * time.Second, Max: 1 * time.Second, MaxRetries: 300}), // Wait 5m max.
	))
	testutil.Ok(t, prom.WaitSumMetricsWithOptions(
		e2emon.Greater(expectSamples), []string{"prometheus_remote_storage_samples_total"},
		e2emon.WithLabelMatchers(&matchers.Matcher{Name: "remote_name", Value: "v1-to-sink", Type: matchers.MatchEqual}),
	))

	// Uncomment for the interactive run (test will run until you kill the test or hit endpoint that was logged)
	// so you can explore Prometheus UI and sink metrics.
	// testutil.Ok(t, e2einteractive.OpenInBrowser("http://"+prom.Endpoint("http"))) // Open Prometheus UI
	// testutil.Ok(t, e2einteractive.OpenInBrowser("http://"+sink.Endpoint("http")+"/metrics"))
	// testutil.Ok(t, e2einteractive.RunUntilEndpointHit())

	promAllReqsSent := 0.0
	// Match sender with receiver.
	for _, ver := range []struct {
		remoteName string
		proto      remote.WriteProtoFullName
	}{
		{"v1-to-sink", remote.WriteProtoFullNameV1},
		{"v2-to-sink", remote.WriteProtoFullNameV2},
	} {
		t.Run(string(ver.proto), func(t *testing.T) {
			t.Run("requests", func(t *testing.T) {
				promReqsSent, err := prom.SumMetrics([]string{"prometheus_remote_storage_sent_batch_duration_seconds"},
					e2emon.WithLabelMatchers(&matchers.Matcher{Name: "remote_name", Value: ver.remoteName, Type: matchers.MatchEqual}),
					e2emon.WithMetricCount())
				testutil.Ok(t, err)
				promAllReqsSent += promReqsSent[0]
				sinkReqsRecv, err := sink.SumMetrics([]string{"sink_received_data_elements"},
					e2emon.WithLabelMatchers(
						&matchers.Matcher{Name: "proto", Value: string(ver.proto), Type: matchers.MatchEqual},
						&matchers.Matcher{Name: "data", Value: "series", Type: matchers.MatchEqual},
					),
					e2emon.WithMetricCount())
				testutil.Ok(t, err)
				if promReqsSent[0] <= 0 || sinkReqsRecv[0] < promReqsSent[0] {
					t.Fatal("in sink, expected non zero requests", promReqsSent, "but got", sinkReqsRecv)
				}
			})

			t.Run("samples", func(t *testing.T) {
				promSamplesSent, err := prom.SumMetrics([]string{"prometheus_remote_storage_samples_total"},
					e2emon.WithLabelMatchers(&matchers.Matcher{Name: "remote_name", Value: ver.remoteName, Type: matchers.MatchEqual}))
				testutil.Ok(t, err)
				sinkSamplesRecv, err := sink.SumMetrics([]string{"sink_received_data_elements"},
					e2emon.WithLabelMatchers(
						&matchers.Matcher{Name: "proto", Value: string(ver.proto), Type: matchers.MatchEqual},
						&matchers.Matcher{Name: "data", Value: "samples", Type: matchers.MatchEqual},
					))
				testutil.Ok(t, err)
				if promSamplesSent[0] < expectSamples || sinkSamplesRecv[0] < promSamplesSent[0] {
					t.Fatal("in sink, expected non zero samples", promSamplesSent, "but got", sinkSamplesRecv)
				}
			})

			t.Run("histograms", func(t *testing.T) {
				promHistSent, err := prom.SumMetrics([]string{"prometheus_remote_storage_histograms_total"},
					e2emon.WithLabelMatchers(&matchers.Matcher{Name: "remote_name", Value: ver.remoteName, Type: matchers.MatchEqual}))
				testutil.Ok(t, err)
				sinkHistRecv, err := sink.SumMetrics([]string{"sink_received_data_elements"},
					e2emon.WithLabelMatchers(
						&matchers.Matcher{Name: "proto", Value: string(ver.proto), Type: matchers.MatchEqual},
						&matchers.Matcher{Name: "data", Value: "histograms", Type: matchers.MatchEqual},
					))
				testutil.Ok(t, err)
				if promHistSent[0] <= 0 || sinkHistRecv[0] < promHistSent[0] {
					t.Fatal("in sink, expected non zero histograms", promHistSent, "but got", sinkHistRecv)
				}
			})
		})
	}

	got, err := sink.SumMetrics([]string{"http_requests_total"},
		e2emon.WithLabelMatchers(
			&matchers.Matcher{Name: "handler", Value: "/sink/prw", Type: matchers.MatchEqual},
			&matchers.Matcher{Name: "code", Value: "204", Type: matchers.MatchEqual},
		))
	testutil.Ok(t, err)
	if got[0] < promAllReqsSent {
		t.Fatal("in sink, expected not less succesful request than", promAllReqsSent, "but got", got)
	}

	// Expect no failures on sender side.
	got, err = prom.SumMetrics([]string{"prometheus_remote_storage_samples_retried_total", "prometheus_remote_storage_samples_failed_total", "prometheus_remote_storage_exemplars_retried_total", "prometheus_remote_storage_exemplars_failed_total"})
	testutil.Ok(t, err)
	for i := 0; i < 4; i++ {
		testutil.Equals(t, 0.0, got[i])
	}

	// No failures on sink side.
	got, err = sink.SumMetrics([]string{"http_requests_total"},
		e2emon.WithLabelMatchers(
			&matchers.Matcher{Name: "code", Value: "204", Type: matchers.MatchNotEqual},
		),
		e2emon.SkipMissingMetrics(),
	)
	testutil.Ok(t, err)
	testutil.Equals(t, 0.0, got[0])
}

func newSink(e e2e.Environment, name, image string, flagOverride map[string]string) *e2emon.InstrumentedRunnable {
	ports := map[string]int{"http": 9011}

	f := e.Runnable(name).WithPorts(ports).Future()
	args := map[string]string{
		"-listen-address": fmt.Sprintf(":%d", ports["http"]),
		"--log.level":     "info",
	}
	if flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
	}

	return e2emon.AsInstrumented(f.Init(e2e.StartOptions{
		Image:     image,
		Command:   e2e.NewCommandWithoutEntrypoint("sink", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	}), "http")
}

func newPrometheus(env e2e.Environment, name, image, remoteWriteAddress string, flagOverride map[string]string) *e2emon.Prometheus {
	ports := map[string]int{"http": 9090}

	f := env.Runnable(name).WithPorts(ports).Future()
	config := fmt.Sprintf(`
global:
  external_labels:
    prometheus: %v
scrape_configs:
- job_name: 'self'
  scrape_interval: 5s
  scrape_timeout: 5s
  static_configs:
  - targets: ['localhost:%v']

remote_write:
- url: %q
  name: v1-to-sink
  send_exemplars: true
  send_native_histograms: true
  protobuf_message: prometheus.WriteRequest #v1
- url: %q
  name: v2-to-sink
  send_exemplars: true
  send_native_histograms: true # Currently broken documentation, should be always true but it's not.
  protobuf_message: io.prometheus.write.v2.Request #v2
  basic_auth:
    username: "XXXX"
    password: "XXXX"
  headers:
    "X-Scope-OrgID": "1"
    "A": "B"

`, name, ports["http"], remoteWriteAddress, remoteWriteAddress)
	if err := os.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0o600); err != nil {
		return &e2emon.Prometheus{Runnable: e2e.NewFailedRunnable(name, fmt.Errorf("create prometheus config failed: %w", err))}
	}

	args := map[string]string{
		"--web.listen-address":                  fmt.Sprintf(":%d", ports["http"]),
		"--config.file":                         filepath.Join(f.Dir(), "prometheus.yml"),
		"--storage.tsdb.path":                   f.Dir(),
		"--enable-feature=exemplar-storage":     "",
		"--enable-feature=native-histograms":    "",
		"--enable-feature=metadata-wal-records": "",
		"--storage.tsdb.no-lockfile":            "",
		"--storage.tsdb.retention.time":         "1d",
		"--storage.tsdb.wal-compression":        "",
		"--storage.tsdb.min-block-duration":     "2h",
		"--storage.tsdb.max-block-duration":     "2h",
		"--web.enable-lifecycle":                "",
		"--log.format":                          "json",
		"--log.level":                           "info",
	}
	if flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
	}

	p := e2emon.AsInstrumented(f.Init(e2e.StartOptions{
		Image:     image,
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	}), "http")

	return &e2emon.Prometheus{
		Runnable:     p,
		Instrumented: p,
	}
}
