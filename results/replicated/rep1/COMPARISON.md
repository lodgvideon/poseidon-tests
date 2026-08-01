# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/replicated/rep1`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 115.8 | 100.3 | +15.5% | standard better |
| HTTP/2 | 99.3 | 117.8 | -15.7% | poseidon better |
| HTTP/3 | 143.4 | 168.6 | -15.0% | poseidon better |
| gRPC | 102.1 | 129.9 | -21.4% | poseidon better |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.6 | 42.9 | -0.7% | below noise floor |
| HTTP/2 | 40.9 | 45.3 | -9.8% | below noise floor |
| HTTP/3 | 46.2 | 45.6 | +1.2% | below noise floor |
| gRPC | 43.7 | 44.4 | -1.6% | below noise floor |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.7 | 43.2 | +3.6% | below noise floor |
| HTTP/2 | 43.6 | 45.5 | -4.1% | below noise floor |
| HTTP/3 | 46.7 | 45.9 | +1.8% | below noise floor |
| gRPC | 43.9 | 44.9 | -2.2% | below noise floor |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.1 | 59.8 | -36.4% | poseidon better |
| HTTP/2 | 10.8 | 49.4 | -78.2% | poseidon better |
| HTTP/3 | 161.1 | 147.3 | +9.4% | standard better |
| gRPC | 30.9 | 88.1 | -64.9% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,400 | 24,229 | -90.1% | poseidon better |
| HTTP/2 | 1,532 | 27,939 | -94.5% | poseidon better |
| HTTP/3 | 67,644 | 30,186 | +124.1% | standard better |
| gRPC | 77,679 | 29,204 | +166.0% | standard better |

## Validity

- HTTP/1.1 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 3.5% further than the monotonic plateau (62.14s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.48s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.47s vs 60.02s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 2.8% further than the monotonic plateau (61.71s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 6.1% further than the monotonic plateau (63.71s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / poseidon: runtime-sourced CPU fields cover 50.8s of a 60.0s plateau (2 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.
- HTTP/2 / poseidon: runtime-sourced CPU fields cover 38.8s of a 60.0s plateau (1 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,938 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,938 | 0 | 596 | 198.8 |
| `h2-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,936 | 0 | 596 | 198.9 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,940 | 0 | 0 | 198.8 |
