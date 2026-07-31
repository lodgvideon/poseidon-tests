# Findings while building the harness

Things learned about the libraries under test while wiring them up. These are
integration findings, not the benchmark's headline results.

**Provenance of the numbers below.** The measurements quoted in this document
come from targeted **local** runs on the author's machine (Windows, Go 1.26.4,
driver and target as host processes over loopback), at 200 RPS with a 10s ramp
and a 40s plateau, 32 workers, 8 connections. They were taken to diagnose
specific behaviour, not to produce the comparison table, and they are not the
same runs as anything under `results/`. Absolute figures — especially CPU
millicores — are not comparable across those two environments; the poseidon
vs. standard *ratios* within a single run are the meaningful part.

The in-cluster comparison table is produced by `scripts/run.sh` and lands in
`results/COMPARISON.md`. `results/rehearsal/` holds a short (15s ramp / 60s
plateau) full-matrix rehearsal, committed as a worked example of the output
format — it is **not** a production result; see README's Caveats.

## poseidon-http-client

> **Status: both poseidon findings below were reported and fixed upstream.**
> [#331](https://github.com/lodgvideon/poseidon-http-client/issues/331) (the
> 16 KiB per-request allocation) and
> [#334](https://github.com/lodgvideon/poseidon-http-client/issues/334) (the
> missing H1 TLS dialer) are closed. The harness now builds against
> post-fix `main`, and the "after" measurements are in
> [the H1 section below](#the-http11-pooled-transport-allocated-16-kib-per-request-fixed).
> The descriptions are kept in the past tense as a record of what was found and
> how, not as a description of current behaviour.

### No usable TLS dialer for the HTTP/1.1 transport (fixed)

`TransportH1Pool` and `TransportH1SingleConn` document their requirement
clearly:

> ConnOpts.Dialer must NOT assert ALPN "h2" — use a plain TCP dialer or a TLS
> dialer with NextProtos containing only "http/1.1".

But no such dialer is exported. The three that ship are all unusable for
HTTPS + HTTP/1.1:

| Dialer | Behaviour |
|---|---|
| `conn.TLSDialer` | **Prepends `"h2"`** to whatever `NextProtos` you supply, then returns `ErrALPNFailed` unless the server picks h2 |
| `conn.FlexDialer` | Offers both, so an h2-capable server will prefer h2 |
| `conn.PlaintextDialer` | No TLS at all |

So any consumer talking HTTPS over HTTP/1.1 must write their own `conn.Dialer`.
Ours is `h1TLSDialer` in `internal/clientset/poseidon.go`.

**The failure mode is silent, not loud.** Passing `conn.TLSDialer` with
`NextProtos: ["http/1.1"]` looks correct and dials successfully — the "h2"
prepended behind your back wins ALPN, and poseidon's HTTP/1.1 transport then
writes an HTTP/1.1 request into a connection the server believes is HTTP/2.
The client reports `http1: read status line: EOF`; the server logs
`bogus greeting "GET /v1/fetch HTTP/1.1\r\n"`. Neither message points at ALPN.
In our first smoke run this failed 100% of H1 requests while still producing
plausible-looking allocation and CPU numbers — 568 allocs/req and 74
millicores, which would have gone straight into the comparison table as a
catastrophic poseidon result had the error counts not been checked.

**Fixed upstream** in
[#334](https://github.com/lodgvideon/poseidon-http-client/issues/334) — and
both suggested remedies were taken, not just one:

- `conn.H1TLSDialer` now exists, offering only `http/1.1` and asserting the
  peer did not select anything else.
- `conn.TLSDialer` no longer silently overrides an explicit `NextProtos`. A
  config asking for `http/1.1` is now rejected **before dialing** with a typed
  `ErrALPNConflict` naming the alternative, so the misconfiguration fails
  loudly at construction instead of as a mangled exchange.
- The H1 transport additionally asserts the connection it was handed really is
  `http/1.1` (`assertH1Conn`), turning the old
  `http1: read status line: EOF` into a message that names the protocol.

This harness now uses the upstream dialer; the local workaround is gone.

### The HTTP/1.1 pooled transport allocated 16 KiB per request (fixed)

The HTTP/1.1 arm was the one regime where poseidon lost consistently. Profiling
localised it to a single line — `client/h1_pool_transport.go:40`:

```go
return &h1Exchange{ex: mc.c.NewExchange(), nc: mc.c, release: release}, ...
```

`h1Exchange` (`client/h1_transport.go:26`) carries an inline array:

```go
// scratch buffer for ReadBodyChunk; avoids per-Recv allocation.
buf [16 * 1024]byte
```

`openExchange` runs once per request, so every request heap-allocates a fresh
16 KiB exchange. In a 40s / 8,000-request plateau that line accounted for
**69.5% of all bytes allocated** by the arm — one allocation per request, but a
very large one. (Note the two pprof indices disagree sharply here:
`alloc_objects` shows a forgettable ~1/request, `alloc_space` shows the
dominant cost. Reading only the object counts would have missed this entirely.)

The comment's stated goal is sound — avoid allocating inside `ReadBodyChunk` —
but the buffer is scoped to the exchange rather than to the pooled connection,
so it trades a per-`Recv` allocation for a per-*request* one that is larger.
Pooling the scratch buffer (`sync.Pool`, or hanging it off the pooled
`http1.Conn`, which already outlives individual exchanges) would remove it.

Measured impact against `net/http` on identical work, generator overhead
excluded, 200 RPS over a 40s plateau (local, pre-fix):

| Metric | poseidon | net/http | Δ |
|---|---:|---:|---:|
| allocs/request | 64.2 | 56.3 | +14% |
| bytes/request | 26,276 | 14,080 | **+87%** |
| CPU (millicores) | 9.4 | 2.0 | **+368%** |

The allocation *count* was nearly competitive; the byte volume and the CPU cost
of collecting it were not. It was specific to the H1 transport — H2, H3, and
gRPC never showed it.

#### After the fix

`h1Exchange` is now pointer-sized; the scratch buffer moved off the
per-request exchange. Re-measured in-cluster at 200 RPS over a 60s plateau,
same harness, same target, only the client version changed:

| Metric | before | after | change |
|---|---:|---:|---:|
| allocs/request | 63.6 | 37.7 | **−40.6%** |
| bytes/request | 27,481 | 2,317 | **−91.6%** |
| CPU (millicores) | 109.9 | 100.3 | −8.8% |

The 16 KiB array was essentially the *whole* byte volume of the H1 path —
removing it cut allocated bytes by more than 11×. And the row flipped: against
`net/http` on that same run, poseidon now allocates **32% fewer objects** and
**85% fewer bytes**, where before it lost on both.

**CPU did not follow, and that is the interesting part.** Poseidon's H1 arm is
still ~12% *above* `net/http` on CPU despite allocating 85% fewer bytes, so
whatever dominates H1 CPU is not allocation or GC pressure. That is now the
open question for this transport; nothing in this benchmark localises it, and
a CPU profile (`driver -profile-dir` writes heap profiles only — `/debug/pprof/
profile` would be the entry point) is the obvious next step.

### The gRPC client sends a bare `application/grpc` content-type

`grpc/conn.go` hardcodes `content-type: application/grpc`, with no way to set a
content-subtype. gRPC servers resolve a bare `application/grpc` to the default
protobuf codec, so a non-protobuf codec cannot be selected by the client.

Since poseidon's gRPC API is `[]byte`-oriented and carries no protobuf
dependency at all, a grpc-go server will reject everything it sends unless the
server is configured to override codec selection:

```go
grpc.NewServer(grpc.ForceServerCodec(myRawCodec{}))
```

Without it: `INTERNAL: grpc: error unmarshalling request: failed to unmarshal,
message is *[]uint8, want proto.Message`.

This is not a bug so much as a documented-nowhere constraint: **poseidon's gRPC
client can only talk to a server whose codec is fixed server-side**, or one
that happens to use protobuf with the caller marshalling by hand. Contrast
grpc-go's client, which advertises `application/grpc+rawbytes` and lets the
server resolve the codec by subtype.

## Harness bugs this caught (fixed)

- **The two arms were not doing the same work with response bodies.** The
  poseidon arm used `BodyBuffer`, materialising every response into the
  caller-owned `Response`; the standard arm did `io.Copy(io.Discard, ...)`,
  streaming the body straight to nowhere. Discarding is strictly less work than
  buffering, so the bytes-per-request column was comparing "buffered" against
  "thrown away" rather than comparing the libraries. In the first full cluster
  run this showed as poseidon allocating **118–173% more** on H1, H3, and gRPC
  — a result that would have been published as a serious poseidon regression.
  The standard arm now uses `io.ReadAll`, the idiomatic net/http body read.
  Note the direction: the bug was biased *against* poseidon, and only surfaced
  because a +173% delta was implausible enough to be worth re-deriving.

- **stdout/stderr interleaving corrupted a report.** Kubernetes merges a
  container's stdout and stderr into one stream. The driver printed a
  pretty-printed multi-line JSON report on stdout and logged its summary on
  stderr, and in one cell out of eight the log line landed *between two lines
  of the JSON document*, making it unparseable. As a race it would have been
  intermittent and very hard to diagnose across a 3.3-hour matrix. The report
  is now a single marker-prefixed line (`POSEIDON_REPORT_JSON {...}`) written
  after all logging — one `Println` is one write, so nothing can land inside
  it, and extraction is a grep rather than a `/^{/,/^}/` range match.

- **The harness dominated its own measurement.** Generating a payload per
  request cost ~94 allocations on the poseidon arm and ~72 on the standard one
  — roughly 60% of every number the benchmark reported. Being paid by both arms
  it did not *bias* the result, but it buried the signal: removing it dropped
  allocs/request from 158 to 64 (poseidon) and 128 to 56 (standard), and only
  then did the H1 byte-volume gap become legible. Payloads are now precomputed
  into a `payload.Pool` and cycled; they still vary in size and content per
  request, they merely repeat after 1024 of them, which is harmless because
  nothing caches response bodies.

- **…and then the fix reproduced the same bug in a different column.** The pool
  was initially built *per worker*. At 32 workers that is 32 × 1024 bodies, and
  RSS went from ~35 MiB to ~530 MiB — so the memory column was now measuring
  the fixture, exactly as the allocation column had been. The tell was that
  absolute RSS moved by 15× while the poseidon-vs-standard deltas stayed small:
  a large shared constant added to both arms. One pool is now shared by all
  workers, indexed by `(worker, seq)` so they still walk it out of step, and
  memory is independent of worker count.

  Worth internalising as a pattern: **every fixture the harness holds or builds
  lands in the same counters as the thing under test.** Both times, the giveaway
  was an absolute number moving far more than the delta between arms.

- **The CPU column silently died, in the worst possible direction.** Go's
  `/cpu/classes/*` metrics are only refreshed during a GC cycle. Once payloads
  were precomputed, the driver allocated little enough that a low-allocating
  arm could run an entire 60-second plateau without a single GC — and both
  plateau snapshots then returned a *byte-identical* CPU figure (`1.229714` →
  `1.229714`), giving a delta of exactly zero millicores. Two cells out of
  eight reported `0.0`.

  The direction is what makes this dangerous rather than merely wrong: GC
  frequency tracks allocation rate, so the arm allocating **less** — the one
  doing better — is the one whose CPU reading freezes. A naive reading of that
  table would have concluded the efficient client used no CPU at all. Note it
  was *introduced* by a correctness fix: removing the harness's own allocation
  overhead is what starved the GC.

  CPU now comes from the OS — `/proc/self/stat` on Linux (where the benchmark
  actually runs), `GetProcessTimes` on Windows (where it is developed and
  locally profiled) — neither of which depends on the collector's own
  behaviour. The runtime-metrics figure is still reported alongside as
  `cpu_runtime_seconds` so the two can be compared. `TestCPUAdvancesUnderLoad`
  burns CPU without allocating, which is exactly the shape that defeated the
  old source, and fails if the column ever goes dead again.

- **HTTP/2 `REFUSED_STREAM` resets — mitigated, not eliminated (open).** The
  first hypothesis was a stream-limit race: the client requesting 250 streams
  per connection against a server advertising exactly 250. The target now
  advertises 1000, well above anything either client asks for.

  **That did not fix it.** The poseidon H2 arm still reports roughly 0.1% of
  requests failing this way (17 in ~11,900), concentrated on the `stream`
  scenario. Tracing the error code back through the client, the reset is raised
  in poseidon's **GOAWAY handler** (`conn/conn.go:1459`): when a peer sends
  GOAWAY, every stream with an id above `lastStreamID` is surfaced to the
  caller as `EventReset(REFUSED_STREAM)`. So the client is reporting a
  connection being retired mid-flight, not a stream cap being hit — which means
  the original diagnosis was wrong about the mechanism.

  What is still unknown is *why* a connection is being retired: candidates are
  the target's http2 server closing it, poseidon's pool health-check/idle
  eviction racing an in-flight stream, or the streaming scenario's long-lived
  responses interacting with either. This has not been run to ground.

  Consequences for reading results: the affected cell is flagged in
  `COMPARISON.md`'s Validity section, and 0.1% is small enough not to move the
  per-request figures materially — but it is a real, reproducible defect
  somewhere in the client or the harness, and it is recorded here as open
  rather than quietly dropped. Note also that a bounded-retry `Retryer` is
  exactly what poseidon ships for this case and the benchmark deliberately does
  not enable one, since retries would mask cost differences.

- **Mismatched counter scoping.** Plateau request counts were deltas but error
  counts were since-process-start, producing "294 errors out of 250 requests".
  Every reported counter now latches a baseline at the plateau boundary.
- **gRPC scenario mapping.** The HTTP mix includes body-less scenarios
  (`fetch`, `stream`, `notfound`). Mapped naively onto the single gRPC Echo
  method they sent a nil body, and `json.Unmarshal(nil)` failed ~55% of gRPC
  calls. The gRPC regime now runs an echo-only mix — see `scenario.GRPCMix`
  and the comparability caveat recorded there.
