# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/rehearsal`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 120.1 | 98.8 | +21.6% | below noise floor |
| HTTP/2 | 105.9 | 117.9 | -10.2% | below noise floor |
| HTTP/3 | 157.4 | 171.6 | -8.3% | below noise floor |
| gRPC | 103.1 | 123.6 | -16.6% | below noise floor |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.4 | 43.2 | -1.8% | below noise floor |
| HTTP/2 | 41.0 | 45.7 | -10.2% | poseidon better |
| HTTP/3 | 46.2 | 45.1 | +2.5% | below noise floor |
| gRPC | 43.8 | 44.5 | -1.6% | below noise floor |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.5 | 43.4 | +2.6% | below noise floor |
| HTTP/2 | 43.8 | 46.2 | -5.2% | below noise floor |
| HTTP/3 | 46.4 | 45.4 | +2.3% | below noise floor |
| gRPC | 44.2 | 44.7 | -1.0% | below noise floor |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 38.6 | 60.1 | -35.8% | poseidon better |
| HTTP/2 | 11.2 | 49.4 | -77.4% | poseidon better |
| HTTP/3 | 159.4 | 148.4 | +7.4% | standard better |
| gRPC | 30.9 | 87.9 | -64.8% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,483 | 24,285 | -89.8% | poseidon better |
| HTTP/2 | 1,642 | 28,088 | -94.2% | poseidon better |
| HTTP/3 | 66,997 | 30,309 | +121.0% | standard better |
| gRPC | 77,648 | 29,186 | +166.0% | standard better |

## Validity

- Unrecognized file `h1-standard-pooled.json` (expected `<regime>-<arm>.json`); ignored.
- Unrecognized file `h2-standard-pooled.json` (expected `<regime>-<arm>.json`); ignored.
- HTTP/2 / poseidon: 5 plateau error(s) - sample: `stream: client: stream reset by peer: 7`
- HTTP/1.1 / poseidon: wall clock advanced 6.3% further than the monotonic plateau (63.84s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 2.0% further than the monotonic plateau (61.27s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 3.2% further than the monotonic plateau (61.98s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 6.2% further than the monotonic plateau (63.73s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 5.7% further than the monotonic plateau (63.45s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 9.1% further than the monotonic plateau (65.49s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 1.9% further than the monotonic plateau (61.18s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,937 | 5 | 596 | 198.8 |
| `h2-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,938 | 0 | 596 | 198.9 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,937 | 0 | 0 | 198.8 |
