package clientset

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
)

// stdHTTPPooled is the standard arm with ONE line changed: the response body is
// materialised into a sync.Pool'd bytes.Buffer instead of a fresh io.ReadAll
// buffer per request.
//
// Why it exists. The headline H1/H2 bytes-per-request win is reported against
// an arm that calls io.ReadAll, which allocates a growth-doubling buffer per
// response — at this mix's ~7.7 KB mean body that is tens of kilobytes of
// churn per request that has nothing to do with the wire path. poseidon's arm
// appends into a caller-owned Response drawn from a sync.Pool, whose Reset does
// `r.Body = r.Body[:0]` (client/response.go:83) and therefore keeps capacity
// across reuse. Both are idiomatic for their library, but the two halves of the
// delta answer different questions:
//
//	poseidon vs standard         = "what does switching library cost/save?"
//	poseidon vs standard-pooled  = "what does the transport itself cost?"
//	standard  vs standard-pooled = "what does the body idiom alone cost?"
//
// Everything else — request construction, bytes.NewReader body, header set,
// transport configuration, connection counts — is identical to stdHTTP by
// construction: this type reuses the same newStdH1/newStdH2 transport builders.
type stdHTTPPooled struct {
	c    *http.Client
	base string

	// bufPool mirrors poseidonHTTP.respPool exactly: a sync.Pool of a
	// caller-owned, capacity-retaining body buffer. Using a sync.Pool rather
	// than a per-worker buffer keeps the two arms symmetric, including the
	// GC-eviction behaviour a sync.Pool has.
	bufPool sync.Pool
}

func (s *stdHTTPPooled) Do(ctx context.Context, call Call) (Result, error) {
	var body io.Reader
	if call.Body != nil {
		body = bytes.NewReader(call.Body)
	}
	req, err := http.NewRequestWithContext(ctx, call.Method, s.base+call.Path, body)
	if err != nil {
		return Result{}, err
	}
	if call.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.c.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	buf, _ := s.bufPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = new(bytes.Buffer)
	}
	defer func() {
		buf.Reset() // keeps the underlying array, as Response.Reset does
		s.bufPool.Put(buf)
	}()

	// ReadFrom is the pooled equivalent of io.ReadAll: same result, same
	// full materialisation of the body, but the buffer that grows to hold it
	// is reused instead of discarded. It is io.Copy's read loop underneath,
	// so this is also the io.CopyBuffer-into-a-pooled-buffer shape.
	n, err := buf.ReadFrom(resp.Body)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: resp.StatusCode, BodyLen: int(n)}, nil
}

func (s *stdHTTPPooled) Close() error {
	s.c.CloseIdleConnections()
	return nil
}

// newStdH1Pooled and newStdH2Pooled build the SAME transports the scored
// standard arms use. They intentionally do not restate the transport options:
// duplicating them is how two arms silently drift apart, which this project has
// already been bitten by once (the gRPC topology mismatch).

func newStdH1Pooled(cfg Config) (Client, error) {
	c, err := newStdH1(cfg)
	if err != nil {
		return nil, err
	}
	s := c.(*stdHTTP)
	return &stdHTTPPooled{c: s.c, base: s.base}, nil
}

func newStdH2Pooled(cfg Config) (Client, error) {
	c, err := newStdH2(cfg)
	if err != nil {
		return nil, err
	}
	s := c.(*stdHTTP)
	return &stdHTTPPooled{c: s.c, base: s.base}, nil
}
