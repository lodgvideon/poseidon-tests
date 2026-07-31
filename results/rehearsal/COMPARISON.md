# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/rehearsal`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 100.3 | 89.3 | +12.3% | below noise floor |
| HTTP/2 | 89.4 | 104.4 | -14.3% | below noise floor |
| HTTP/3 | 133.8 | 162.9 | -17.9% | below noise floor |
| gRPC | 105.8 | 129.9 | -18.6% | below noise floor |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 42.2 | 42.8 | -1.4% | ~equal |
| HTTP/2 | 41.3 | 45.4 | -8.9% | below noise floor |
| HTTP/3 | 46.3 | 44.9 | +3.1% | ~equal |
| gRPC | 46.3 | 44.0 | +5.3% | below noise floor |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 44.5 | 42.9 | +3.8% | ~equal |
| HTTP/2 | 44.1 | 45.5 | -3.2% | ~equal |
| HTTP/3 | 46.9 | 45.3 | +3.7% | ~equal |
| gRPC | 46.5 | 44.1 | +5.4% | below noise floor |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 37.7 | 55.8 | -32.3% | poseidon better |
| HTTP/2 | 10.5 | 45.5 | -77.0% | poseidon better |
| HTTP/3 | 141.9 | 134.7 | +5.3% | standard better |
| gRPC | 31.3 | 88.0 | -64.5% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 2,317 | 15,489 | -85.0% | poseidon better |
| HTTP/2 | 1,527 | 15,918 | -90.4% | poseidon better |
| HTTP/3 | 43,282 | 21,123 | +104.9% | standard better |
| gRPC | 78,680 | 29,177 | +169.7% | standard better |

## Validity

- HTTP/2 / poseidon: 4 plateau error(s) - sample: `stream: client: stream reset by peer: 7`
- HTTP/1.1 / poseidon: wall clock advanced 3.1% further than the monotonic plateau (61.87s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/1.1 / standard: wall clock advanced 1.3% further than the monotonic plateau (60.84s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / poseidon: wall clock advanced 2.0% further than the monotonic plateau (61.22s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/2 / standard: wall clock advanced 4.6% further than the monotonic plateau (62.84s vs 60.06s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / poseidon: wall clock advanced 2.3% further than the monotonic plateau (61.44s vs 60.03s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- HTTP/3 / standard: wall clock advanced 3.6% further than the monotonic plateau (62.21s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / poseidon: wall clock advanced 5.2% further than the monotonic plateau (63.17s vs 60.04s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.
- gRPC / standard: wall clock advanced 2.3% further than the monotonic plateau (61.40s vs 60.05s) - the measuring host lost time mid-window, so CPU figures for this cell are unreliable.

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,936 | 0 | 597 | 198.8 |
| `h1-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,936 | 4 | 596 | 198.8 |
| `h2-standard.json` | 11,938 | 0 | 596 | 198.8 |
| `h3-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,937 | 0 | 0 | 198.8 |
