# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/replicated/rep2`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 115.6 | 101.4 | +14.0% | standard better |
| HTTP/2 | 99.3 | 118.3 | -16.0% | poseidon better |
| HTTP/3 | 141.9 | 166.7 | -14.9% | poseidon better |
| gRPC | 102.3 | 129.1 | -20.8% | poseidon better |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.5 | 43.1 | -1.5% | below noise floor |
| HTTP/2 | 40.8 | 46.0 | -11.4% | poseidon better |
| HTTP/3 | 46.3 | 45.2 | +2.5% | below noise floor |
| gRPC | 43.7 | 45.0 | -3.0% | below noise floor |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.6 | 43.2 | +3.1% | below noise floor |
| HTTP/2 | 43.4 | 46.2 | -6.0% | below noise floor |
| HTTP/3 | 46.8 | 45.6 | +2.6% | below noise floor |
| gRPC | 44.3 | 45.2 | -2.0% | below noise floor |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.2 | 60.1 | -36.5% | poseidon better |
| HTTP/2 | 11.2 | 49.2 | -77.2% | poseidon better |
| HTTP/3 | 160.8 | 147.6 | +9.0% | standard better |
| gRPC | 31.1 | 87.6 | -64.5% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,267 | 24,283 | -90.7% | poseidon better |
| HTTP/2 | 1,574 | 27,997 | -94.4% | poseidon better |
| HTTP/3 | 67,385 | 30,193 | +123.2% | standard better |
| gRPC | 77,796 | 29,101 | +167.3% | standard better |

## Validity

- HTTP/2 / poseidon: 1 plateau error(s) - sample: `stream: client: stream reset by peer: 7`
- HTTP/1.1 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.47s vs 60.02s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 2.7% further than the monotonic plateau (61.66s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 3.3% further than the monotonic plateau (61.97s vs 60.02s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: runtime-sourced CPU fields cover 74.6s of a 60.0s plateau (2 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,937 | 0 | 596 | 198.9 |
| `h1-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,937 | 1 | 596 | 198.9 |
| `h2-standard.json` | 11,937 | 0 | 597 | 198.8 |
| `h3-poseidon.json` | 11,937 | 0 | 597 | 198.8 |
| `h3-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.9 |
| `grpc-standard.json` | 11,937 | 0 | 0 | 198.8 |
