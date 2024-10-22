# sink

An efficient binary, with predictive latency, for receiving streams of data for sender testing and benchmarking purposes.

In the future fault injection logic is planned, see [functionality](#functionality) to learn more.

```bash mdox-exec="bash scripts/format_help.sh"
Usage of sink:
  -listen-address string
    	Address to listen on. Available HTTP paths: /metrics, /-/ready, /-/health, /sink/prw (default ":9011")
  -log.level string
    	Logging level, available values: 'debug', 'info', 'warn', 'error'. (default "info")
```

Check our [test/prom_test.go](./test/prom_test.go) test to see end-to-end example on how it can be used, with docker and Prometheus.

## Functionality

The idea of sink is that it should have **a predictive latency of sending**, for benchmark reproducibility, because the efficiency of sender heavily depends on efficiency of receiver. To achieve this sink can't be a complex and slow-ish database with complex efficiency guarantees, it has to be fast and ideally trivial to scale. For this reason sink generally discards the data (aka sending to `/dev/null`).

However, for testability of benchmark (e.g. to check if the communication is even successful), it's essential to capture basic metrics about the incoming data. Sink does that in Prometheus format, exposing them on `/metrics` HTTP endpoint.

Currently supported functionality:

* Semantic sink for [Remote Write 2.0](https://prometheus.io/docs/specs/remote_write_spec_2_0/) and [1.0](https://prometheus.io/docs/specs/remote_write_spec/) protocol, capturing rich histogram `sink_received_data_elements` per `data` (`series|samples|histograms|exemplars`) and `proto` (`io.prometheus.write.v2.Request|prometheus.WriteRequest`). See example metrics produced [here](./example-metrics.promtext).
* Generic HTTP metrics for received traffic e.g `http_request_duration_seconds`, `http_request_size_bytes`, `http_response_size_bytes`, `http_requests_total`. See example metrics produced [here](./example-metrics.promtext).

### Wishlist

Currently, sink supports only [Remote Write 2.0](https://prometheus.io/docs/specs/remote_write_spec_2_0/) and [1.0](https://prometheus.io/docs/specs/remote_write_spec/) protocol.

However, there are things in the scope of this binary, but not implemented yet (help wanted!):

* [] Generic HTTP fault injections (different status codes for certain times).
* [] Generic `sink/any` for any HTTP streams.
* [] Semantic sink for OpenTelemetry OTLP.
* [] Fault injection logic.
* [] CI for building image.

## Installing

### Docker

```bash
docker pull quay.io/bwplotka/sink@latest
```

### Go

```bash
go install github.com/bwplotka/sink/go/sink@latest
```

### Local

After cloning the repo:

`$ cd go && CGO_ENABLED=0 go build ./sink`
