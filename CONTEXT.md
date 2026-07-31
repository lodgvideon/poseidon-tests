# poseidon-tests — Domain Glossary

## Purpose

A comparative benchmark ("depo-приложение", demo/showcase app) answering: if a
potential consumer switches its outbound HTTP client from a standard library
client to **poseidon-http-client**, what happens to memory, CPU, and allocation
counts under sustained load, across HTTP/1.1, HTTP/2, and HTTP/3? Output is a
comparison table for that adoption decision.

## Terms

### Comparison scope: client-only

The test isolates the **client library's** contribution to resource usage. The
target **server is fixed and identical** across both arms (poseidon-client run
and standard-client run) for a given protocol — only the calling client
changes. This is a deliberate choice: it answers "what does switching our
client cost/save us," not "what does switching our whole stack cost/save us."
The client+server-swap variant was considered and rejected because it conflates
client effect with server effect, muddying attribution.

### Fixed target server: neutral, one chi handler everywhere

The target is **written for this benchmark** — a single `chi` router
(`net/http`-compatible `http.Handler`) holding all application logic, served
over four transports:

| Regime | Target transport | Library |
|---|---|---|
| HTTP/1.1 | `net/http` server | Go stdlib |
| HTTP/2 | `net/http` server (h2 / h2c) | Go stdlib (+`x/net/http2`) |
| HTTP/3 | `http3.Server{Handler: chiRouter}` | `quic-go/http3` |
| gRPC | `grpc.Server` | `google.golang.org/grpc` |

Two properties this buys:

1. **Neutrality.** No target is any client's sibling. `poseidon-http-server`
   was the original plan for H1/H2, but that creates an obvious credibility
   attack for a benchmark meant to persuade an external consumer: "of course
   poseidon-client won, it was talking to its own server." A stdlib/chi target
   is unimpeachable, and poseidon-client vs. a `grpc-go` server doubles as an
   interop demonstration.
2. **Comparable rows.** Application logic is byte-identical across regimes
   (same chi handler), so the H1/H2/H3 rows can be compared to each other, not
   just within a row. Only the transport differs.

`poseidon-http-server` was additionally *disqualified* for HTTP/1.1 on a hard
technical ground, not just a fairness one: it cannot serve plain HTTP/1.1 at
all. A request without `Upgrade: h2c` is answered `400 Bad Request — "Only h2c
supported"` (`server/h2c.go`); HTTP/1.1 exists there solely as an upgrade path
into h2c.

### "Standard client" (baseline), per protocol

- **HTTP/1.1, HTTP/2** → Go stdlib `net/http`.
- **HTTP/3** → `quic-go/http3`. Go stdlib has no HTTP/3 client; `quic-go/http3`
  is the de facto standard Go HTTP/3 client (and what Caddy itself is built
  on), so it's the natural "what would you use instead of poseidon" baseline.

### Dynamic data

Both **size and content** of request/response JSON payloads vary per request
(not a fixed-size blob with only field values changing). This deliberately
lets JSON marshal/unmarshal and buffer-sizing allocations show up in the
comparison, not just wire-codec allocations — those app-layer allocations are
real cost for a consumer regardless of which HTTP client they pick, and hiding
them behind a fixed-size payload would make the comparison too favorable to
whichever client happens to cache better at one specific size.

Payloads are **precomputed** into a per-worker pool of 1024 and cycled, rather
than generated per request. Generating them inline turned out to cost ~60% of
every allocation the benchmark measured, burying the signal it exists to
capture (see `docs/FINDINGS.md`). Size and content still vary from request to
request; they simply repeat after 1024 requests, which is harmless because
nothing caches response bodies — HPACK and QPACK index headers, not payloads.

### Workload shape: weighted scenario mix

The demo app drives a **weighted mix of scenarios** for each of the three HTTP
regimes (not a single flat endpoint) — same spirit as `poseidon-http-server/loadtest/loadgen`
(variable-size JSON, streaming, error paths, etc.), adapted so the *same*
scenario mix can run through either client library. Chosen deliberately over
the simpler single-endpoint option: closer to real product traffic, even
though it makes attributing a resource delta to "the client library specifically"
a little less clean (some of the variance will land in JSON/marshaling code
shared by both arms, not just in the client transport).

**gRPC is the exception**: it runs an echo-only mix (`scenario.GRPCMix`). gRPC
unary has no path or method verb and the target exposes a single Echo method,
so the HTTP scenario shapes do not map onto it — mapping them naively sent a
body-less call the server could not unmarshal, which failed ~55% of gRPC
requests before it was caught (see `docs/FINDINGS.md`). The gRPC row is a clean
poseidon-vs-grpc-go comparison within itself, but is not comparable to the
HTTP rows.

Domain content of the
demo (what the JSON represents) is arbitrary/neutral — "депо" in the original
ask was a typo for "демо", not a request for a warehouse/logistics domain
model.

### Protocol/regime matrix: 4 regimes

HTTP/1.1, HTTP/2, HTTP/3, and **gRPC-over-HTTP/2** as a 4th regime (gRPC is a
distinct application protocol layered on H2, not an HTTP version, but it's
in scope because poseidon ships a real client for it — see below).

### gRPC client baseline

`poseidon-http-client` shipped a first-class `grpc` package **today**
(2026-07-31, commit `db2f8ce`, tagged as current release **v0.11.0**) —
`grpc.Dial` / `ClientConn.Invoke`, no protobuf dependency (messages are raw
`[]byte`, caller marshals), **HTTP/2 only** (no gRPC-over-H3 — `http3.Request`
has no incremental-send path yet). This is a real product surface, not the
hand-rolled/no-grpc-go test shim the existing `loadtest/loadgen` uses
internally for its own dogfooding scenario.

- **poseidon side**: `grpc.Dial` + `ClientConn.Invoke` (unary only, this test).
- **standard side**: `google.golang.org/grpc` (grpc-go) unary calls — the
  industry-standard Go gRPC client.
- **Call shapes**: unary only (not streaming) — keeps one call shape, cleaner
  attribution of any resource delta to transport/framing rather than call-shape
  differences.
- **Gotcha**: `poseidon-http-client/grpc`'s import path collides with
  `google.golang.org/grpc` when both are imported in the same file — needs an
  import alias in the demo app.
- **Target server for this regime**: a `grpc-go` server (see the neutral-target
  decision above), shared by both arms.
- **No protobuf.** Both arms exchange the *same raw JSON bytes* as every HTTP
  regime, via a pass-through gRPC codec (`grpc.ForceCodec`) on the grpc-go
  side and poseidon's natively-`[]byte` message API on the other. This keeps
  payloads identical across all four regimes and avoids dragging `protoc` into
  the build.

### Load model: fixed closed-loop 200 RPS

All 8 combinations (4 regimes × 2 clients) run a **closed-loop, rate-limited
generator fixed at 200 RPS** — not open-loop max-throughput discovery. This
answers "how much does switching to poseidon cost/save at our current traffic
level," not "which one wins at saturation." Ramp: 5 min to reach the 200 RPS
plateau; hold: 20 min at plateau. Metrics of interest (CPU, RSS, allocations)
are read from the plateau window only, after the ramp has stabilized GC/pool
warmup.

### Run count: 1 pass per combination (v1)

One 25-minute pass (5 ramp + 20 plateau) per combination for the first version
— ~3.3h total for all 8. Explicitly deferred: repeat runs for statistical
confidence (variance/error bars). Add repeats later only if numbers look noisy
on the first pass.

### Resource requests/limits: generous, observe-only

Pods get generous CPU/memory requests+limits (enough that neither client is
throttled or OOM-killed) — the goal is measuring **true** resource consumption,
not testing behavior under artificial constraint. A tight-limits/throttling
study was considered and explicitly deferred — it answers a different question
("does poseidon let us run smaller pods") and would confound "actual usage"
numbers with throttling artifacts in the same run.

### Metrics pipeline: Prometheus + Grafana + pprof snapshots

- **Prometheus + Grafana** in-cluster, scraping the **driver pods only**, on
  `:9100`. The driver exposes its own Go `runtime/metrics` — CPU, RSS, and
  GC/allocation counters — carrying `regime` and `arm` labels so Grafana can
  plot the two arms of a regime against each other. There are deliberately no
  server-side RED metrics: the target is a neutral, uninstrumented stdlib/chi
  server (ADR-0003), and instrumenting it would measure the target rather than
  the thing under test.
- The cluster has no metrics-server, so nothing external could report the
  driver's resource use anyway. Self-reporting turns out to be the better
  source regardless, because it yields allocation *counts*, which no
  container-level metric can provide.
- **pprof heap snapshots** taken at the start and end of the plateau window
  (`driver -profile-dir`), diffed with `go tool pprof -base`, for the
  per-callsite allocation attribution that counters alone cannot give.
- Both together because Prometheus alone doesn't say *which code path*
  allocates, and pprof snapshots alone don't give a continuous time series
  across the run.

### Output table: rows = regime, absolute values + delta%

One row per regime (HTTP/1.1, HTTP/2, HTTP/3, gRPC). Columns: for each metric
(avg CPU, avg/peak RSS, allocations) — poseidon value, standard-client value,
and Δ% — so the table is self-contained (raw numbers visible, not just a
"poseidon is X% better" claim without the numbers backing it).

## Deferred

Work that was consciously scoped out rather than forgotten — repeat runs for
variance, and the tight-limits/throttling study — is recorded in the sections
above and summarised in README's Caveats.
