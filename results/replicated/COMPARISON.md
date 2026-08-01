# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/replicated`

## CPU (millicores)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 115.3 | 100.0 | +15.3% | 3 | 1.6% | standard better |
| HTTP/2 | 99.2 | 117.6 | -15.6% | 3 | 0.7% | poseidon better |
| HTTP/3 | 142.5 | 166.6 | -14.5% | 3 | 1.2% | poseidon better |
| gRPC | 102.6 | 129.6 | -20.9% | 3 | 0.6% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.4 | 43.2 | -1.8% | 3 | 0.7% | poseidon better |
| HTTP/2 | 40.8 | 45.6 | -10.5% | 3 | 0.8% | poseidon better |
| HTTP/3 | 46.2 | 45.4 | +1.6% | 3 | 0.5% | standard better |
| gRPC | 43.6 | 44.6 | -2.3% | 3 | 0.8% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.5 | 43.4 | +2.6% | 3 | 0.7% | standard better |
| HTTP/2 | 43.3 | 45.8 | -5.5% | 3 | 0.9% | poseidon better |
| HTTP/3 | 46.7 | 45.7 | +2.0% | 3 | 0.4% | standard better |
| gRPC | 44.0 | 44.9 | -2.0% | 3 | 0.6% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## Allocations per request

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.2 | 60.0 | -36.3% | 3 | 0.4% | poseidon better |
| HTTP/2 | 10.9 | 49.4 | -77.8% | 3 | 2.3% | poseidon better |
| HTTP/3 | 160.8 | 147.4 | +9.0% | 3 | 0.3% | standard better |
| gRPC | 31.0 | 87.9 | -64.7% | 3 | 0.3% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## Bytes allocated per request

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,360 | 24,264 | -90.3% | 3 | 3.4% | poseidon better |
| HTTP/2 | 1,548 | 28,101 | -94.5% | 3 | 1.5% | poseidon better |
| HTTP/3 | 67,303 | 30,186 | +123.0% | 3 | 0.6% | standard better |
| gRPC | 77,715 | 29,167 | +166.5% | 3 | 0.2% | standard better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

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
