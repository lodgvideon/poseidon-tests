# poseidon-tests

A comparative benchmark answering one question: **if a consumer switches its
outbound HTTP client to [poseidon-http-client](https://github.com/lodgvideon/poseidon-http-client),
what happens to CPU, memory, and allocations?**

Four protocol regimes, two client libraries each, identical work on both sides,
run in a local Kubernetes cluster. Output is a comparison table.

## The comparison

| Regime | poseidon arm | standard arm (the incumbent) |
|---|---|---|
| HTTP/1.1 | `client.TransportH1Pool` | `net/http` |
| HTTP/2 | `client.TransportPool` | `golang.org/x/net/http2` |
| HTTP/3 | `client.TransportH3Pool` | `quic-go/http3` |
| gRPC | `poseidon-http-client/grpc` | `google.golang.org/grpc` |

**Only the client changes.** The target server is fixed and identical within
every regime — one `chi` router served over stdlib H1/H2, quic-go H3, and
grpc-go. It is deliberately *not* `poseidon-http-server`: benchmarking a client
against its own sibling server invites the obvious objection, and stdlib is
unimpeachable. See [ADR-0003](docs/adr/0003-neutral-purpose-built-target.md).

Because all four regimes run byte-identical application logic behind different
transports, and every regime runs over TLS, **rows are comparable to each other**,
not merely within themselves. The one exception is gRPC — see
`scenario.GRPCMix` for why.

## Load model

Closed-loop, rate-limited, **200 RPS**, identical for all eight cells: 5 min
ramp, then 20 min plateau. Only the plateau is measured, so connection setup,
TLS handshakes, and GC warm-up stay out of the numbers.

This measures footprint *at equal delivered throughput* — "same traffic, this
much less CPU". It deliberately says nothing about peak capacity; see
[ADR-0002](docs/adr/0002-fixed-rate-closed-loop-load-model.md).

Payloads are dynamic in both **size and content**, drawn from a weighted
distribution (60% small, 30% medium, 10% large — up to ~250 KiB), and seeded
deterministically so both arms replay an identical request sequence.

## What is measured, and how

There is no metrics-server in the cluster, so the driver measures **itself**
via `runtime/metrics` — which is the better source anyway, because it yields
allocation counts that no container-level metric can provide.

| Metric | Source |
|---|---|
| CPU (millicores) | `/proc/self/stat` utime+stime (Linux) — see the caveat below |
| Allocations/request | Δ`/gc/heap/allocs:objects` ÷ plateau requests |
| Bytes/request | Δ`/gc/heap/allocs:bytes` ÷ plateau requests |
| RSS avg / peak | `/proc/self/status` `VmRSS`, sampled every 2s during the plateau |

Prometheus + Grafana scrape the same numbers continuously for a live view of
the run; pprof heap profiles at both plateau boundaries give per-callsite
allocation attribution.

**The CPU column is the weak one.** At 200 RPS the driver runs at 5–8% of one
core, so run-to-run variation is a large share of any delta — on a quiet host
per-arm CV is ~1%, on a busy one it reaches 13%. Replicated cells therefore
claim a winner only on complete separation, and print `overlapping` otherwise;
single-run cells fall back to a 10% floor. The allocation columns are exact
runtime counters and are the trustworthy output.

Go's own `/cpu/classes/*` metrics are **not** used: they only refresh at GC mark
termination, so differencing them spans *[last GC before start, last GC before
end]* rather than the plateau — and the error is arm-correlated, because GC
frequency tracks allocation rate. The driver still records them as
`cpu_runtime_seconds` alongside a validity flag, purely as a cross-check.

## Running it

### Prerequisites

- A **kind-style local Kubernetes cluster** — one whose nodes are local Docker
  containers. Built against Docker Desktop's kind-based cluster; a stock
  `kind create cluster` works too. A remote cluster will **not** work without
  changes: image loading side-loads straight into the node containers (see
  below).
- `kubectl`, pointed at that cluster
- `docker`
- Go 1.26 (only to run tests locally; the image builds Go itself)
- **Python 3** — set `PYTHON=/path/to/python3` if it is not on `PATH` as
  `python3` or `python`
- **`envsubst`** (GNU gettext) — `brew install gettext` on macOS,
  `apt install gettext-base` on Debian/Ubuntu. `scripts/run.sh` renders the
  per-cell Job template with it and checks for it up front.

### Steps

```bash
bash scripts/build-and-load.sh
```

Builds the image and side-loads it into each node container with
`docker save | ctr images import` — no `kind` binary and no registry needed.
Node names are read from `kubectl get nodes` (kind names its containers after
the node objects); override with `NODES="a b"` if that ever mismatches.

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml \
              -f deploy/k8s/10-target.yaml \
              -f deploy/k8s/30-prometheus.yaml \
              -f deploy/k8s/40-grafana.yaml
```

Applied individually on purpose. `deploy/k8s/20-driver-job.yaml` is an
`envsubst` template full of `${REGIME}`/`${ARM}` placeholders that `run.sh`
renders once per cell — `kubectl apply -f deploy/k8s/` would try to apply it
raw and be rejected by the API server.

```bash
bash scripts/run.sh
```

Runs all eight cells **sequentially** (~3h20m at the default 5m ramp + 20m
plateau). Concurrent runs would contend for node CPU and for the single target,
measuring the contention rather than the libraries. It writes one JSON per cell
plus `results/COMPARISON.md`.

Smoke-test the whole pipeline on one regime in a couple of minutes first:

```bash
REGIMES=h2 RAMP=15s PLATEAU=60s OUTDIR=results/smoke bash scripts/run.sh
```

Drop `REGIMES=h2` to rehearse all eight cells — about 12 minutes.

`results/replicated/` in this repo is the committed dataset: **three replicates
per cell**, arms alternated within each replicate so host drift hits both. Write
replicates to `rep1/`, `rep2/`, … under one output directory and `report.py`
aggregates them automatically:

```bash
for i in 1 2 3; do
  for regime in h1 h2 h3 grpc; do for arm in poseidon standard; do
    REGIMES=$regime ARMS=$arm RAMP=15s PLATEAU=60s       OUTDIR=results/replicated/rep$i bash scripts/run.sh
  done; done
done
```

### Watching a run

```bash
kubectl -n poseidon-bench port-forward svc/grafana 3000:3000
```

Dashboard "poseidon vs standard" is pre-provisioned; Grafana has anonymous
admin access since the cluster is throwaway.

## Reading the output

`COMPARISON.md` has one table per metric, each row a regime, with poseidon,
standard, Δ%, and a verdict. **Lower is better everywhere**, so a negative Δ
means poseidon won.

**Replicated cells are scored by complete separation** — a winner is declared
only when every replicate of one arm beats every replicate of the other. That
test needs no distributional assumption, which is what makes it usable at n=3.
Single-run cells fall back to a per-metric noise floor, which is weaker: a floor
guessed in advance once suppressed a real result here and caused a true finding
to be retracted upstream (see `docs/FINDINGS.md`, Round 4).

The committed dataset (3 replicates per cell, arms alternated):

| Regime | allocs/req | bytes/req | CPU |
|---|---|---|---|
| HTTP/1.1 | **−47.0%** | **−92.2%** | +5.9% (overlapping) |
| HTTP/2 | **−84.1%** | **−98.1%** | −6.8% (overlapping) |
| HTTP/3 | **−37.5%** | +98.7% | **−17.2%** |
| gRPC | **−69.5%** | +80.7% | **−21.9%** |

Bold is a poseidon win; `overlapping` means the replicate ranges overlap, so no
winner is claimed. **poseidon wins the allocation count in all four regimes.**

Measured against poseidon-http-client `main` @ `62fdeec`, which carries fixes
for several defects this harness reported. The previous pin
(`bf099fa`) scored −36.3% / −77.8% / **+9.0%** / −64.7% on allocations: HTTP/3
flipped from a loss to a decisive win, and the HTTP/1.1 CPU gap fell from +15.3%
to statistically indistinguishable. What remains is the HTTP/3 and gRPC byte
volume, still tracked upstream as
[#342](https://github.com/lodgvideon/poseidon-http-client/issues/342),
[#347](https://github.com/lodgvideon/poseidon-http-client/issues/347) and
[#348](https://github.com/lodgvideon/poseidon-http-client/issues/348).

### Two things to know before quoting the bytes/request row

**Most of it is API idiom, not the wire path.** The standard arm calls
`io.ReadAll`, allocating a fresh growth-doubling buffer per response; the
poseidon arm appends into a pooled, caller-owned `Response`. Both are idiomatic
for their library, but a `net/http` consumer who already pools read buffers
sees far less benefit:

| | vs `net/http` | vs `net/http` **with a pooled read buffer** |
|---|---:|---:|
| H1 bytes/req | −89.8% | **−60.3%** |
| H2 bytes/req | −94.2% | **−82.9%** |

Measure it yourself with the diagnostic arm: `-arm standard-pooled` (H1/H2
only; deliberately outside the scored matrix).

**The allocation-count row is not affected by this** — poseidon is still
−27.3% (H1) and −73.9% (H2) against the pooled arm, because it comes from `net/http`'s per-request
object graph rather than from body buffering. See `docs/FINDINGS.md`.

Read the **Validity** section first. It flags anything that makes the numbers
untrustworthy — nonzero errors, the two arms doing materially different request
counts, or achieved rates diverging. A cell with errors is not a result.

## Repository map

| Path | What |
|---|---|
| `cmd/target` | Wires `internal/api` over the four transports and serves them |
| `cmd/driver` | The measured process — links one client, drives the mix, reports on itself |
| `internal/api` | The chi router and the handlers behind it |
| `internal/clientset` | The seam: one `Client` interface, eight implementations |
| `internal/payload` | Deterministic dynamic payload generation and the precomputed pool |
| `internal/scenario` | The weighted call mix |
| `internal/load` | Ramp + plateau rate control |
| `internal/selfmetrics` | runtime/metrics sampling and Prometheus exposition |
| `internal/rawgrpc` | Pass-through gRPC codec — no protobuf in the comparison |
| `internal/tlsutil` | Self-signed certificate generated at target startup |
| `deploy/k8s` | Namespace, target, driver Job template, Prometheus, Grafana |
| `scripts` | Build/load, run orchestration, table generation |
| `docs/adr` | Why the benchmark is shaped the way it is |
| `docs/FINDINGS.md` | What building this taught us about the libraries |
| `CONTEXT.md` | Glossary and the decisions behind the design |

## Caveats

- **One pass per cell.** No repeat runs, so no variance or error bars. Numbers
  close to each other should be read as "no difference detected", not as a win.
- **A local two-node cluster is noisy.** It is not an isolated benchmark rig.
- **gRPC rows are not comparable to the HTTP rows** — different protocol, a
  different target server, a handler that mirrors rather than shares the chi
  route, and an echo-only mix.
- **Peak capacity is not measured.** See ADR-0002.

## License

[MIT](LICENSE). The benchmark output under `results/` is covered by the same
license — reuse the numbers freely, but read the Caveats above first.
