package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	writev2 "github.com/bwplotka/sink/go/sink/genproto/prometheus/v2"
	"github.com/prometheus/client_golang/api/prometheus/v1/remote"
	"github.com/prometheus/client_golang/prometheus"
)

type issue string

const (
	seriesWithoutTypeIssue   issue = "series-untyped"
	seriesWithoutHelpIssue   issue = "series-without-help"
	seriesWithoutUnitIssue   issue = "series-without-unit"
	cumulativeWithoutCTIssue issue = "cumulative-without-ct"
)

var allIssues = []issue{
	seriesWithoutTypeIssue,
	seriesWithoutHelpIssue,
	seriesWithoutUnitIssue,
	cumulativeWithoutCTIssue,
}

var allIssuesStrings = func() []string {
	ret := make([]string, len(allIssues))
	for i := range allIssues {
		ret[i] = string(allIssues[i])
	}
	return ret
}()

type issueReporter struct {
	logIssuesMu  sync.Mutex
	logIssues    map[issue]struct{}
	logMaxIssues int
}

func newIssueReporter(logIssues []string) *issueReporter {
	return &issueReporter{
		logIssues:    toMap(logIssues),
		logMaxIssues: 10,
	}
}

func toMap(s []string) map[issue]struct{} {
	ret := map[issue]struct{}{}
	for _, e := range s {
		ret[issue(e)] = struct{}{}
	}
	return ret
}

func (r *issueReporter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	issues := req.Form["issue"]
	maxIssues := req.Form.Get("max")

	var (
		err          error
		maxIssuesInt = 5
	)

	if maxIssues != "" {
		maxIssuesInt, err = strconv.Atoi(maxIssues)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Errorf("given 'max' parameter is not an integer: %w", err).Error()))
			return
		}
	}

	r.logIssuesMu.Lock()
	defer r.logIssuesMu.Unlock()

	r.logIssues = toMap(issues)
	r.logMaxIssues = maxIssuesInt
}

type reqIssueReporter struct {
	logger           *slog.Logger
	req              *writev2.Request
	shouldLogAnother func(issue) bool

	issues           map[issue]int
	detailsPerSeries map[int][]string
}

func (r *issueReporter) newRequest(logger *slog.Logger, req *writev2.Request) *reqIssueReporter {
	var (
		i         int
		logIssues map[issue]struct{}
		maxErrs   int
	)

	r.logIssuesMu.Lock()
	logIssues = r.logIssues
	maxErrs = r.logMaxIssues
	r.logIssuesMu.Unlock()

	return &reqIssueReporter{
		logger:           logger,
		req:              req,
		issues:           map[issue]int{},
		detailsPerSeries: map[int][]string{},
		shouldLogAnother: func(typ issue) bool {
			if i >= maxErrs {
				return false
			}
			if _, ok := logIssues[typ]; !ok {
				return false
			}
			i++
			return true
		},
	}
}

func (r *reqIssueReporter) report(typ issue, i int) {
	r.issues[typ]++
	if !r.shouldLogAnother(typ) {
		return
	}

	switch typ {
	case seriesWithoutTypeIssue:
		r.detailsPerSeries[i] = append(r.detailsPerSeries[i], "unspecified metric type")
	case seriesWithoutHelpIssue:
		r.detailsPerSeries[i] = append(r.detailsPerSeries[i], "no help")
	case seriesWithoutUnitIssue:
		r.detailsPerSeries[i] = append(r.detailsPerSeries[i], "no unit")
	case cumulativeWithoutCTIssue:
		r.detailsPerSeries[i] = append(r.detailsPerSeries[i], "no created timestamp")
	default:
		r.detailsPerSeries[i] = append(r.detailsPerSeries[i], string(typ))
	}
}

func (r *reqIssueReporter) commit(metric prometheus.ObserverVec, source, protoName string, series int, w remote.WriteResponseStats) {
	if len(r.issues) == 0 {
		return
	}

	var logArgs []any
	for typ, count := range r.issues {
		metric.WithLabelValues(source, protoName, string(typ)).Observe(float64(count))
		logArgs = append(logArgs, slog.Int(string(typ), count))
	}

	if len(r.detailsPerSeries) == 0 {
		return
	}

	var (
		details []string
		buf     []string
	)
	for i, errs := range r.detailsPerSeries {
		lset := writev2.DesymbolizeLabels(r.req.Timeseries[i].LabelsRefs, r.req.Symbols, buf)
		details = append(details, fmt.Sprintf("series %v: %v\n", lsetString(lset), strings.Join(errs, ",")))
	}

	r.logger.Warn(
		"received remote write request with some issues",
		append([]any{
			slog.String("source", source),
			slog.String("proto", protoName),
			slog.Int(seriesData, series),
			slog.Int(samplesData, w.Samples),
			slog.Int(histogramsData, w.Histograms),
			slog.Int(exemplarsData, w.Exemplars),
			slog.String("details", strings.Join(details, ",")),
		}, logArgs...,
		)...)
}

func lsetString(lset []string) string {
	var name string

	b := strings.Builder{}
	b.WriteString("{")
	var comma bool
	for i := 0; i < len(lset); i += 2 {
		if lset[i] == "__name__" {
			name = lset[i+1]
			continue
		}

		if comma {
			b.WriteString(", ")
		} else {
			comma = true
		}
		b.WriteString(fmt.Sprintf(`%v="%v"`, lset[i], lset[i+1]))
	}
	b.WriteString("}")
	return name + b.String()
}
