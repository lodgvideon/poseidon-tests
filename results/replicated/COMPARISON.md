# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/replicated`

## CPU (millicores)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 86.8 | 82.0 | +5.9% | 3 | 11.0% | overlapping |
| HTTP/2 | 88.7 | 95.2 | -6.8% | 3 | 12.7% | overlapping |
| HTTP/3 | 117.2 | 141.6 | -17.2% | 3 | 5.2% | poseidon better |
| gRPC | 87.5 | 111.9 | -21.9% | 3 | 13.0% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 41.8 | 43.1 | -3.1% | 3 | 0.8% | poseidon better |
| HTTP/2 | 37.9 | 45.8 | -17.2% | 3 | 1.6% | poseidon better |
| HTTP/3 | 46.4 | 45.3 | +2.3% | 3 | 0.2% | standard better |
| gRPC | 43.6 | 44.9 | -2.8% | 3 | 0.4% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.3 | 43.4 | +2.2% | 3 | 1.0% | standard better |
| HTTP/2 | 40.6 | 46.0 | -11.7% | 3 | 1.2% | poseidon better |
| HTTP/3 | 46.7 | 45.7 | +2.2% | 3 | 0.2% | standard better |
| gRPC | 43.8 | 45.1 | -2.9% | 3 | 0.5% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## Allocations per request

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 31.6 | 59.6 | -47.0% | 3 | 0.4% | poseidon better |
| HTTP/2 | 7.9 | 49.5 | -84.1% | 3 | 4.5% | poseidon better |
| HTTP/3 | 92.3 | 147.7 | -37.5% | 3 | 0.2% | poseidon better |
| gRPC | 26.8 | 87.8 | -69.5% | 3 | 0.4% | poseidon better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## Bytes allocated per request

| Regime | poseidon | standard | Δ | n | CV | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| HTTP/1.1 | 1,886 | 24,191 | -92.2% | 3 | 1.4% | poseidon better |
| HTTP/2 | 527 | 27,923 | -98.1% | 3 | 11.2% | poseidon better |
| HTTP/3 | 60,092 | 30,236 | +98.7% | 3 | 0.3% | standard better |
| gRPC | 52,656 | 29,135 | +80.7% | 3 | 0.2% | standard better |

*Replicated cells are scored by complete separation - every replicate of one arm beating every replicate of the other - not against a noise floor. `overlapping` means the ranges overlap, so no winner is claimed. CV is the larger of the two arms'.*

## Validity

- HTTP/1.1 / poseidon: wall clock advanced 2.0% further than the monotonic plateau (61.26s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 2.6% further than the monotonic plateau (61.62s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 3.3% further than the monotonic plateau (62.00s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 5.4% further than the monotonic plateau (63.26s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 2.5% further than the monotonic plateau (61.56s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 3.2% further than the monotonic plateau (61.93s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 5.6% further than the monotonic plateau (63.40s vs 60.02s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 2.5% further than the monotonic plateau (61.56s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: runtime-sourced CPU fields cover 0.0s of a 60.0s plateau (0 GC cycles) - cpu_runtime_seconds, cpu_gc_seconds and cpu_user_seconds are not comparable for this cell. The scored CPU column (cpu_busy_seconds) is unaffected.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h2-standard.json` | 11,935 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,937 | 0 | 596 | 198.9 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.9 |
| `grpc-standard.json` | 11,936 | 0 | 0 | 198.8 |
