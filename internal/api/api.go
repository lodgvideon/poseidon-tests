// Package api holds the target server's application logic: one chi router,
// shared byte-for-byte by every transport (HTTP/1.1, HTTP/2, HTTP/3) and
// mirrored by the gRPC target.
//
// Keeping a single handler is what makes the comparison rows comparable to
// each other and not merely within themselves — only the transport differs
// between regimes, never the work the server does. See ADR-0003.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lodgvideon/poseidon-tests/internal/payload"
)

// Routes are the scenario endpoints the driver exercises. They are named so a
// reader of the load mix can map a scenario straight onto a route.
const (
	RouteEcho    = "/v1/echo"    // POST: read body, echo a derived body back
	RouteFetch   = "/v1/fetch"   // GET:  server-generated body, size varies
	RouteIngest  = "/v1/ingest"  // POST: read body, reply small ack
	RouteStream  = "/v1/stream"  // GET:  chunked/streamed body
	RouteNotFound = "/v1/missing" // GET: error path (404)
	RouteHealth  = "/healthz"
)

// Handler builds the chi router. seed makes server-generated bodies
// deterministic across runs, so both arms of a comparison receive identical
// response bytes.
func Handler(seed uint64) http.Handler {
	r := chi.NewRouter()

	r.Get(RouteHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})

	// echo: consume the request body, reply with a body derived from it.
	// Exercises both directions with a large-ish payload.
	r.Post(RouteEcho, func(w http.ResponseWriter, req *http.Request) {
		var in payload.Body
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		out := payload.Body{Seq: in.Seq, Scenario: "echo", Items: in.Items}
		writeJSON(w, http.StatusOK, out)
	})

	// ingest: consume a body, reply with a tiny ack. Upload-heavy shape.
	r.Post(RouteIngest, func(w http.ResponseWriter, req *http.Request) {
		n, err := io.Copy(io.Discard, req.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"received":`+strconv.FormatInt(n, 10)+`}`)
	})

	// fetch: no request body, server generates a variable-size response.
	// Download-heavy shape. `n` lets the driver request a specific item count
	// so response size stays deterministic per sequence number.
	r.Get(RouteFetch, func(w http.ResponseWriter, req *http.Request) {
		n := intParam(req, "n", 16)
		seq := uint64(intParam(req, "seq", 0))
		g := payload.NewGenerator(seed, n, []payload.SizeClass{
			{MinItems: n, MaxItems: n, Weight: 1},
		})
		b, err := g.Next(seq, "fetch")
		if err != nil {
			http.Error(w, "gen", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	// stream: emit the body in chunks with explicit flushes, so the client's
	// incremental-read path is exercised rather than one buffered write.
	r.Get(RouteStream, func(w http.ResponseWriter, req *http.Request) {
		chunks := intParam(req, "chunks", 8)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, canFlush := w.(http.Flusher)
		_, _ = io.WriteString(w, `{"chunks":[`)
		for i := 0; i < chunks; i++ {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, `{"i":`+strconv.Itoa(i)+`,"pad":"`)
			// A fixed pad keeps each chunk meaningfully sized without
			// re-running the generator per chunk.
			_, _ = w.Write(streamPad)
			_, _ = io.WriteString(w, `"}`)
			if canFlush {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, `]}`)
	})

	// Anything under /v1 that is not a known route is a 404 — the error-path
	// scenario. Clients must handle a non-2xx without treating it as failure.
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	return r
}

// streamPad is a package-level constant buffer; regenerating it per chunk
// would put the target's own allocations into the measurement.
var streamPad = func() []byte {
	b := make([]byte, 512)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}()

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func intParam(req *http.Request, key string, def int) int {
	s := req.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}
