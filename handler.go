package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	v2 "github.com/bwplotka/remote-write-hole/prompb/write/v2"
	"github.com/golang/snappy"
	"google.golang.org/protobuf/proto"
)

const (
	pathPrefix = "/v1/projects/"
	pathSuffix = "/location/global/prometheus/api/v2/write"
)

var projectIDRe = regexp.MustCompile("[a-z-0-9]+")

type rwHandler struct {
}

func registerRWHandler(mux *http.ServeMux) {
	p := &rwHandler{}
	// We expect to serve in PRW /v1/projects/PROJECT_ID/location/global/prometheus/api/v2/write
	mux.Handle(pathPrefix, http.StripPrefix(pathPrefix, p))
}

func (h *rwHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		// TODO(bwplotka): Implement once we agreed on this form of negotiation.
		http.NotFound(w, r)
		return
	case http.MethodPost:
	default:
		http.NotFound(w, r)
		return
	}

	if err := h.handle(r.Context(), r.Body); err != nil {
		HTTPError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	// TODO(bwplotka): Response headers..
}

func (h *rwHandler) handle(ctx context.Context, body io.ReadCloser) error {
	// TODO(bwplotka): Add concurrency limit.

	// TODO(bwplotka): Support more compressions based on the negotiation.
	compressed, err := io.ReadAll(body)
	if err != nil {
		return NewError(err, http.StatusInternalServerError)
	}

	reqBuf, err := snappy.Decode(nil, compressed)
	if err != nil {
		return NewError(fmt.Errorf("snappy decode: %w", err), http.StatusBadRequest)
	}

	var req *v2.WriteRequest
	if err := proto.Unmarshal(reqBuf, req); err != nil {
		return NewError(fmt.Errorf("unmarshal remote write v2: %w", err), http.StatusBadRequest)
	}

	// Stats.
	return nil
}

func HTTPError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var ew *errorWithHTTPCode
	if errors.As(err, &ew) {
		code = ew.HTTPCode()
	}
	http.Error(w, err.Error(), code)
}

type errorWithHTTPCode struct {
	error
	code int
}

func (e *errorWithHTTPCode) HTTPCode() int {
	if e == nil || e.code == 0 {
		return http.StatusInternalServerError
	}
	return e.code
}

func NewError(err error, code int) error {
	if err == nil {
		return nil
	}
	return &errorWithHTTPCode{
		error: err, code: code,
	}
}
