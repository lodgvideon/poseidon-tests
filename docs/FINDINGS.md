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

**CPU did not follow — but that turned out to be this harness, not the client.**
The row still measured ~12% *above* `net/http` on CPU despite allocating 85%
fewer bytes, which looked like a real unexplained cost and was reported as such
on #331. A CPU profile showed it is measurement floor: at 200 RPS the driver
runs at 7–8% of one core and only ~25% of samples are in the request path at
all. See [the CPU-column section below](#the-cpu-column-has-poor-signal-at-200-rps--including-the-h1-residual)
for the numbers, and #331 for the retraction. There is no unexplained H1 CPU
cost to chase.

### gRPC allocates ~24 KiB per RPC recycling a stream — the same shape as #331

> Reported as [#341](https://github.com/lodgvideon/poseidon-http-client/issues/341).

The gRPC row loses on bytes/request by **+170%** (78,680 vs 29,177 against
grpc-go). Profiling attributes 63% of the arm's allocated bytes to two sites,
both per-request:

```
  171.66MB 33.55%  grpc.(*decoder).Push        framing.go:66   d.buf = append(d.buf, chunk...)
  153.52MB 30.01%  conn.recycleStream          stream.go:243   s.events = make(chan StreamEvent, cap(s.events))
```

**`recycleStream` allocates a fresh channel every time it recycles a stream** —
in the path whose entire purpose is to avoid allocation. The size is not
incidental. For a gRPC connection, `eventBufferFor(DefaultMaxMessageSize,
maxFrameSize)` computes `(4 MiB + 256 KiB) / 16384 = 272` slots, and
`StreamEvent` is 88 bytes, so each channel is **~24 KiB**. At ~6,400 requests
that is 153 MB — matching the profile exactly.

This is the same defect class as
[#331](https://github.com/lodgvideon/poseidon-http-client/issues/331): a large
per-request allocation sitting in a reuse path. It is *bigger* than the 16 KiB
H1 one it followed.

Two things compound it, and they have different fixes:

1. **The recycle allocates at all.** The code comments explain why — a stale
   reference from the previous stream lifetime could otherwise write into the
   recycled stream's channel. That is a real correctness concern, so "just
   reuse the buffer" is not obviously safe; a generation counter on the stream
   would let stale writers detect a dead generation without re-minting.
2. **The channel is sized off `MaxRecvMessageSize`,** which defaults to 4 MiB
   whatever the caller's actual messages look like. Setting it to something
   near the real message size shrinks the channel dramatically — 256 KiB gives
   32 slots (~2.8 KiB), roughly 8× smaller. That is a caller-side knob
   available today, and this harness deliberately does not set it, because it
   measures default configuration.

`decoder.Push` is the second site: `d.buf` grows by `append` per chunk, ~27 KiB
per request, suggesting the decode buffer is not carried across calls.

### HTTP/3 copies the response body through three growing buffers

> Reported as [#342](https://github.com/lodgvideon/poseidon-http-client/issues/342).

The H3 row loses on bytes/request by **+105%** (43,282 vs 21,123 against
quic-go). Three sites account for 56% of the arm's bytes, and they are the same
operation at three different layers:

```
   72.12MB 23.29%  quic.(*recvStream).insert     stream.go:385  r.data = append(r.data, data[have-offset:]...)
   58.99MB 19.05%  http3.(*FrameReader).Feed     stream.go:55   r.buf = append(r.buf, b...)
   41.26MB 13.32%  http3.AppendData              frame.go:59    return append(dst, data...)
```

Response bytes are copied into a growing buffer at QUIC stream reassembly,
again into the HTTP/3 frame reader, and again when the frame payload is
appended out. Each `append` carries reallocation-on-growth cost, so a 15 KiB
body costs several times its own size before it reaches the caller.

For contrast, the quic-go arm's profile shows almost nothing on its receive
path — its largest entry is `io.ReadAll` at 77 MB, which is *this harness*
materialising the body, not the library. The gap is a layering cost, not a
tuning one, and closing it means passing buffers down rather than copying at
each boundary.

### The CPU column has poor signal at 200 RPS — including the H1 residual

> A correction was posted to
> [#331](https://github.com/lodgvideon/poseidon-http-client/issues/331) withdrawing
> the "unexplained H1 CPU cost" this supersedes — it was this harness's
> instrument, not the client.

After #331 was fixed, HTTP/1.1 still measured **+12% CPU** against `net/http`
despite allocating 85% fewer bytes. That looked like a real unexplained cost.
It is mostly measurement floor.

A 15-second CPU profile taken mid-plateau on each arm:

| | poseidon | net/http |
|---|---:|---:|
| total samples in 15s | 1.11s (7.4% of a core) | 1.22s (8.1% of a core) |
| `runtime.semawakeup` | 36.9% | 28.7% |
| in the request path (`runWorker` cum) | ~25% | ~25% |
| **this harness's own rate limiter** (`load.Ticker.run`) | — | 19.7% |

Three things follow:

- **Only about a quarter of CPU samples are in the request path at all.** The
  rest is goroutine park/unpark (`semawakeup`), the Go scheduler, and the
  harness's rate limiter, which wakes a goroutine per request to pace 200 RPS.
- **The sign does not even reproduce.** Locally poseidon consumed *less* total
  CPU than `net/http` (1.11s vs 1.22s) — the opposite of the in-cluster +12%.
- At 7–8% of one core, a ±12% difference in total process CPU is not
  distinguishable from scheduling noise.

**How to read the comparison table, then:** the allocation columns are exact
counters accumulated by the runtime, and they are trustworthy — a −85% or
+170% there is real. The CPU column is a sampled rate over a lightly-loaded
process, and **differences below roughly 20% should not be treated as
meaningful at this load.** The large CPU deltas (H2/H3/gRPC, −14% to −19%,
consistently signed across runs) are more likely real than the small H1 one,
but none of them are as solid as the allocation figures.

This is a limitation of the load model, not a bug: ADR-0002 fixes the rate at
200 RPS deliberately, and the price of a light, realistic load is that CPU
differences stay near the floor. Raising the rate would sharpen CPU at the cost
of answering a different question.

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

---

## Round 2 — 5-Whys investigation, and an adversarial review of the harness

A second pass investigated every gap left after #331/#334, using 5 Whys with each step
required to rest on gathered evidence rather than inference, and separately asked an
independent reviewer whether this benchmark can be trusted at all. Four more client
defects were filed; the review found four problems in the harness itself, all now fixed.

### Client defects found and filed

**[#344] HTTP/2 `REFUSED_STREAM` — the client destroys its own completed stream.**
This closes the open question recorded above, and corrects it: the earlier note blamed the
GOAWAY handler and rested on a false premise — *no connection is retired at all*. A frame
capture (`GODEBUG=http2debug=2`) shows the server writing `HEADERS` + 9 × `DATA` (the last
with `END_STREAM`) and then **reading `RST_STREAM(REFUSED_STREAM)` from the client**.

The default stream event buffer is 8 slots (`conn/options.go:128`); a `/v1/stream`
response delivers 10 events (1 HEADERS + 9 DATA); `pushLocked` (`conn/stream.go:346-376`)
answers a full channel by killing the stream rather than applying backpressure. Causal
test: setting `StreamEventBuffer: 64` and changing nothing else gave 0 errors across two
runs, against 3 and 4 on the immediately preceding controls.

The trigger is many *small* flushed frames, not many bytes — 8 × 529 B fits inside the
65535-byte window, so flow control, the only backpressure that exists, never engages. A
DATA-frames-per-stream histogram shows 671 of 6,453 streams with exactly 9 frames: 10.4%,
matching the scenario's 10% weight.

Worse than a tuning issue: the synthesised reset reuses `REFUSED_STREAM`, which
`client/retry.go:44-46` classifies as safe to replay on the grounds that "the server did
not process the request". Here the server processed it and wrote the whole response — so
poseidon's own `Retryer`, the library's recommended remedy for code 7, would silently
re-execute a completed request.

**[#345] QUIC allocates on every packet.** Four one-line sites — `nonce` and `full` crypto
scratch escaping through `cipher.AEAD` / `cipher.Block` interface calls, and a reflective
`sort.Slice` over a 1–2 element slice once per received packet — account for ~40% of H3's
allocation count. Confirmed by `-gcflags='-m'` escape output and pprof line attribution;
quic-go hoists the same buffers onto its structs and maintains ACK ranges with an ordered
insert.

**[#346] The gRPC `Stream` is per-call, defeating two reuse mechanisms that already
exist.** `decoder.buf` and `sendBuf` both start from nil on every RPC because the `Stream`
embedding them is heap-allocated per call. Measured against the module's own framing code:
poseidon costs 2.25× the message size where grpc-go costs 1.06×. Presizing from the length
prefix reaches exact parity; reusing via the already-written `Reset()` — which has **zero
non-test callers** module-wide — reaches 424 B/msg.

**[#347] H3 duplicates the whole request body to prepend a ≤9-byte DATA frame header**,
and copies every ~1157-byte chunk unpooled for retransmission. This also corrected a
mis-attribution in #342: `AppendData` is a *request-body* copy, not one of the
receive-path copies.

### The harness was wrong in four ways — all found by review, all fixed here

1. **Half the HTTP mix downloaded a byte-identical body.** The driver issued bare
   `/v1/fetch` and `/v1/stream` paths, so the target fell back to its defaults and
   returned the same response every time. fetch (40%) + stream (10%) = 50% of the mix had
   *zero* response-size variation — precisely the fixed-size flattery CONTEXT.md rejects
   by name, and biased toward whichever client reuses a response buffer best. Fixed:
   `scenario.PathPool` precomputes parameterised paths (4–203 items, 2–17 chunks), keeping
   the request path allocation-free.

2. **The two gRPC arms ran different connection topologies.** poseidon dialled 8
   connections and serialised one RPC on each; grpc-go used 1 multiplexed connection —
   confirmed by `netstat` during a run, and the exact opposite of what the code comment
   claimed. The comment was written from intent and never verified. That put 8× the
   per-connection state (TLS, HPACK tables, framer buffers) into RSS — the only row
   poseidon lost — and changed write-coalescing, confounding CPU. Per-RPC allocation
   figures were unaffected. Fixed: one connection, concurrent `Invoke`s.

3. **The CPU column stamped verdicts it cannot support.** `report.py` applied a single 5%
   band to every metric, labelling −14/−18/−19% CPU rows "poseidon better" — four times
   below the noise floor established earlier in this document, and at least one of them
   flips sign when re-run in another environment. Fixed: per-metric noise floors
   (CPU 25%, RSS 10%); those rows now read **"below noise floor"**.

4. **The measuring host was losing wall-clock time, invisibly.** In every cell the
   wall-clock plateau exceeded the monotonic duration the rates divide by — by 1.3% to
   5.2%, and *by different amounts in the two arms of the same regime*. That is a paused
   or clock-stepped VM under the kind cluster. Fixed: `report.py` now compares wall
   against monotonic duration and flags any cell over 1%. It fires on all eight cells of
   the committed rehearsal.

Also fixed: response body volume is now reported per arm and cross-checked between them,
flagging any divergence over 0.5%. That is the mechanical guard for the failure mode this
project has now hit four times — the harness doing unequal work while request counts match
— and it would have caught both the buffered-vs-discarded body bug and problem 1 above.
Achieved rate is checked against target rate. The start heap profile is written before the
plateau snapshot rather than inside the measured window. A misleading `MaxIncomingStreams`
comment on the H3 standard arm was removed: it bounds peer-initiated streams and was a
no-op.

### What the reviewer concluded

**Trustworthy with caveats, 6/10.** The allocation columns — what the benchmark exists to
produce — were independently reproduced within 1–4% on a different OS, machine, run length
and worker count, so the large deltas are robust and the defects they point at are real
client findings rather than harness artifacts. The CPU column was not trustworthy and is
now labelled accordingly.

Verified sound, recorded so the audit is symmetric: H1/H2/H3 connection topology is fair;
counter scoping is correct; `/proc/self/stat` parsing is correct; payload determinism and
the shared pool hold; the marker-line report extraction and validity section are real
safeguards; non-2xx counts match across arms, confirming mix equality in practice; and the
target is not a bottleneck at 200 RPS.

### Still open

The reviewer's own questions, which this round raised but did not answer:

- Does the H3 allocation-count gap reproduce in-cluster at the magnitude seen locally?
  (+59% local vs +5.3% in-cluster; the per-request packet rate driving it was never
  measured in-cluster.)
- What are the true CPU deltas — or can this design measure CPU at 200 RPS at all?
- Does the gRPC RSS gap survive now that both arms use one connection?
- How much do the byte and allocation columns move now that fetch and stream sizes
  actually vary?
- How much of the flagship H1/H2 byte win is transport efficiency versus API idiom
  (a reused caller-owned `Response` against a per-request `io.ReadAll`)?

---

## Round 3 — the headline was overstated, and the H3 number was measured on the wrong OS

Two corrections, both material, and both to claims this document previously made.

### Most of the flagship byte win is API idiom, not the wire path

The headline "poseidon allocates 90% fewer bytes on H1, 94% on H2" bundles two independent
effects. A third **diagnostic arm** (`standard-pooled`: byte-for-byte the standard arm
except the response body is materialised into a `sync.Pool`'d buffer instead of a fresh
`io.ReadAll` buffer) separates them:

| | poseidon vs `net/http` | poseidon vs `net/http` **with a pooled read buffer** |
|---|---:|---:|
| H1 bytes/req | −90.2% | **−63%** |
| H2 bytes/req | −94.2% | **−84%** |

So roughly **four fifths of the H1 delta and two thirds of the H2 delta is response-body
buffer reuse** — something any `net/http` consumer can do today without changing client
libraries. Body volume across the three arms agreed to 0.005–0.238%, 0 errors in all 12
cells, and the local headline reproduced the in-cluster one (H1 −89.9% local vs −90.2%
in-cluster), so the split carries over.

The honest transport claim is the smaller one: *against a `net/http` arm that already pools
its read buffer, poseidon still allocates 63% (H1) and 84% (H2) fewer bytes* — mostly by
not paying `x/net/http2`'s per-stream `dataChunkPool` refill (~5.2 KB/req, H2) or
`net/http`'s 32 KiB request-write copy buffer (~2.2 KB/req, H1).

**This correction does not extend to the allocation-count column.** 67–82% of the count
win survives the pooled control (H1 poseidon 39.0 vs pooled 53.3 vs standard 59.9; H2 11.3
vs 42.5 vs 49.5), because `io.ReadAll`'s cost is byte-volume-dominated while the object
count is dominated by `net/http`'s per-request object graph — `Request`, parsed and cloned
`URL`, two header maps, the cancel-context chain, `Response` — which pooling a buffer does
nothing about. The **−35.8% / −77.9% allocation-count headline stands** as a genuine
library difference.

The diagnostic arm is committed but deliberately **outside the scored matrix**
(`clientset.ArmStandardPooled`, selectable with `-arm standard-pooled`, absent from `Arms`
and from `report.py`), so the 4×2 comparison is unchanged.

### The H3 allocation gap was quoted from the wrong operating system

#345 was filed citing a +59% allocation-count gap measured on Windows loopback. The gap in
the environment this benchmark reports is **+8.9%**. Measured on byte-identical work
across three environments:

| Environment | poseidon | quic-go | gap | QUIC packets/req |
|---|---:|---:|---:|---:|
| Windows loopback | 243.4 | 145.2 | +67.6% | 37.8 |
| Linux, same-pod loopback (MTU 65536) | 159.0 | 145.1 | +9.6% | 16.0 |
| Linux, pod-to-pod veth (MTU 1500) | 159.4 | 146.4 | +8.9% | 16.6 |

MTU and loopback are both ruled out — a 44× MTU change moves the number by 4%; the OS
moves it by 7×. The cause is that poseidon emits ~2.9× more, smaller QUIC packets on
Windows for the same stream bytes (423 B vs 962 B average chunk), and #345's per-packet
cost is multiplied by that rate. The mechanism is confirmed and quantitatively closed:
(243.4 − 159.4) allocs ÷ (37.8 − 16.6) packets = **3.96 objects per packet**, matching
#345's site list, corroborated by OS packet counters. quic-go's allocs/req moves 0.9%
across the same three environments, so the coupling is poseidon's.

*Why* poseidon fragments its sends on Windows was **not** established — loss, MTU,
flow-control blocking, stream contention and GSO batching were each ruled out by evidence;
what remains are timing-coupled clamps in `Stream.grantable`, which would need cwnd
instrumentation inside the client to separate. The chain was stopped rather than guessed.

#345 has been corrected upstream with the Linux figure. Its size is better stated as a
share of poseidon's *own* H3 count (~37% for the four named sites, ~75% including all
packet-driven sites), which is environment-robust, than as a ratio against quic-go, which
is not. A **new** Linux-only site was found in the process — the GRO receive path
allocating ~6 objects per `recvmsg`, ~20% of the in-cluster H3 count, invisible to Windows
profiling — filed as **#348**.

This also invalidates a claim made earlier in this document: that cross-environment
*ratios* are the meaningful part while absolute figures are not. That holds for H1/H2,
where per-request cost dominates. It is false for H3 by a factor of 7, because there the
cost is per-*packet* and the packet rate is set by the operating system.

### Harness fixes this round

- **H3 connection topology matched.** The poseidon H3 arm pooled 8 QUIC connections
  against quic-go's architecturally-single one — the **third** instance of the topology
  confound that already forced one retraction (gRPC). Now 1 connection both sides. The
  earlier claim in this document that H3 topology was "verified sound" was wrong.
- **The CPU noise floor was measured, not guessed:** running one arm against itself gives
  a 13.4% coefficient of variation at 200 RPS, so the minimum resolvable delta between two
  single runs is ~30%. The floor in `report.py` was raised from 25% to 30%. The cause is
  quantisation, not composition — a tick-sampled counter accumulates only ~100 ticks over
  a plateau where the process runs at 5–8% of one core. Raising RPS collapses the variance
  by making the measured quantity larger; it barely changes the request path's *share*.
- **A sub-floor delta is no longer labelled "~equal."** Calling a difference a tie is a
  positive claim of equality, and below the floor the instrument supports neither that nor
  a winner. Such rows now read "below noise floor" too.
- **`cpu_windows.go` was computing a nonsense absolute value.** `Filetime.Nanoseconds()`
  subtracts the 1601→1970 epoch offset, which is right for a wall-clock FILETIME and wrong
  for a duration, making absolute `CPUBusySeconds` a large negative number. Deltas were
  unaffected, which is why it survived. Now computed from raw 100 ns ticks.

### Still open

- Does the H3 allocation verdict survive matched topology **in-cluster**? The fix is in;
  the causal test (−3.0% allocs at `-conns 1`) was local Windows only.
- Two CPU instruments the harness already records (`cpu_busy_seconds` from the OS,
  `cpu_runtime_seconds` from `runtime/metrics`) disagree by 1.15–1.54×, and the
  disagreement correlates with the arm rather than cancelling — enough to flip the H1
  sign. Which is right is undetermined.
- The three-way byte decomposition should be reproduced in-cluster before the restated
  headline is treated as final.
