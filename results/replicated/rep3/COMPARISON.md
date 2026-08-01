# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/replicated/rep3`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 114.4 | 98.3 | +16.4% | standard better |
| HTTP/2 | 99.1 | 116.8 | -15.1% | poseidon better |
| HTTP/3 | 142.3 | 164.5 | -13.5% | poseidon better |
| gRPC | 103.3 | 129.9 | -20.5% | poseidon better |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.1 | 43.5 | -3.3% | below noise floor |
| HTTP/2 | 40.9 | 45.6 | -10.4% | poseidon better |
| HTTP/3 | 46.1 | 45.5 | +1.2% | below noise floor |
| gRPC | 43.5 | 44.5 | -2.2% | below noise floor |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.2 | 43.7 | +1.0% | below noise floor |
| HTTP/2 | 42.9 | 45.7 | -6.3% | below noise floor |
| HTTP/3 | 46.5 | 45.7 | +1.6% | below noise floor |
| gRPC | 43.9 | 44.6 | -1.7% | below noise floor |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.4 | 60.0 | -35.9% | poseidon better |
| HTTP/2 | 10.9 | 49.5 | -78.1% | poseidon better |
| HTTP/3 | 160.3 | 147.5 | +8.7% | standard better |
| gRPC | 31.0 | 88.0 | -64.8% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,412 | 24,279 | -90.1% | poseidon better |
| HTTP/2 | 1,537 | 28,368 | -94.6% | poseidon better |
| HTTP/3 | 66,878 | 30,177 | +121.6% | standard better |
| gRPC | 77,671 | 29,195 | +166.0% | standard better |

## Validity

- HTTP/2 / poseidon: 4 plateau error(s) - sample: `stream: client: stream reset by peer: 7`
- HTTP/1.1 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.51s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 6.0% further than the monotonic plateau (63.62s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 2.0% further than the monotonic plateau (61.22s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 5.3% further than the monotonic plateau (63.25s vs 60.06s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 5.9% further than the monotonic plateau (63.60s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / poseidon: runtime-sourced CPU fields cover 69.6s of a 60.1s plateau (3 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.
- HTTP/2 / poseidon: runtime-sourced CPU fields cover 37.2s of a 60.0s plateau (1 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,937 | 4 | 596 | 198.8 |
| `h2-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,936 | 0 | 596 | 198.7 |
| `grpc-poseidon.json` | 11,935 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,938 | 0 | 0 | 198.8 |
