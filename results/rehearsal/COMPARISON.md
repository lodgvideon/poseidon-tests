# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/rehearsal`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 117.4 | 98.6 | +19.1% | below noise floor |
| HTTP/2 | 99.8 | 114.3 | -12.7% | below noise floor |
| HTTP/3 | 137.6 | 159.6 | -13.8% | below noise floor |
| gRPC | 108.9 | 133.7 | -18.5% | below noise floor |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.0 | 43.5 | -3.6% | ~equal |
| HTTP/2 | 41.2 | 45.6 | -9.6% | below noise floor |
| HTTP/3 | 46.3 | 45.1 | +2.7% | ~equal |
| gRPC | 43.3 | 44.8 | -3.4% | ~equal |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 43.9 | 43.8 | +0.3% | ~equal |
| HTTP/2 | 44.7 | 46.1 | -3.2% | ~equal |
| HTTP/3 | 46.7 | 45.6 | +2.3% | ~equal |
| gRPC | 43.5 | 45.0 | -3.2% | ~equal |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.4 | 59.9 | -35.8% | poseidon better |
| HTTP/2 | 11.0 | 49.8 | -77.9% | poseidon better |
| HTTP/3 | 159.3 | 148.1 | +7.6% | standard better |
| gRPC | 30.9 | 88.5 | -65.0% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,380 | 24,200 | -90.2% | poseidon better |
| HTTP/2 | 1,642 | 28,102 | -94.2% | poseidon better |
| HTTP/3 | 67,579 | 30,303 | +123.0% | standard better |
| gRPC | 77,549 | 29,257 | +165.1% | standard better |

## Validity

- HTTP/2 / poseidon: 6 plateau error(s) - sample: `stream: client: stream reset by peer: 7`
- HTTP/1.1 / poseidon: wall clock advanced 6.2% further than the monotonic plateau (63.74s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 2.3% further than the monotonic plateau (61.45s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.48s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 2.9% further than the monotonic plateau (61.76s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.48s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 9.1% further than the monotonic plateau (65.50s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,936 | 6 | 596 | 198.8 |
| `h2-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `grpc-poseidon.json` | 11,935 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,938 | 0 | 0 | 198.8 |
