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
| CPU (millicores) | `/cpu/classes/total` − `/cpu/classes/idle`, over the plateau |
| Allocations/request | Δ`/gc/heap/allocs:objects` ÷ plateau requests |
| Bytes/request | Δ`/gc/heap/allocs:bytes` ÷ plateau requests |
| RSS avg / peak | `/proc/self/status` `VmRSS`, sampled every 2s during the plateau |

Prometheus + Grafana scrape the same numbers continuously for a live view of
the run; pprof heap profiles at both plateau boundaries give per-callsite
allocation attribution.

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
REGIMES=h2 RAMP=15s PLATEAU=60s OUTDIR=results/rehearsal bash scripts/run.sh
```

Drop `REGIMES=h2` to rehearse all eight cells — about 12 minutes.
`results/rehearsal/` in this repo is the committed output of exactly that
full-matrix rehearsal.

### Watching a run

```bash
kubectl -n poseidon-bench port-forward svc/grafana 3000:3000
```

Dashboard "poseidon vs standard" is pre-provisioned; Grafana has anonymous
admin access since the cluster is throwaway.

## Reading the output

`results/COMPARISON.md` has one table per metric, each row a regime, with
poseidon, standard, Δ%, and a verdict. **Lower is better everywhere**, so a
negative Δ means poseidon won.

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
