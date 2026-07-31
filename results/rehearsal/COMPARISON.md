# poseidon-http-client vs standard clients

All figures are measured over the plateau window only; lower is better for every metric, so a negative Δ means poseidon won.

Source directory: `results/rehearsal`

## CPU (millicores)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 109.9 | 87.1 | +26.2% | standard better |
| HTTP/2 | 87.8 | 101.8 | -13.8% | poseidon better |
| HTTP/3 | 133.9 | 169.4 | -21.0% | poseidon better |
| gRPC | 105.1 | 133.4 | -21.2% | poseidon better |

## RSS avg (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 43.6 | 42.8 | +2.0% | ~equal |
| HTTP/2 | 40.6 | 44.9 | -9.6% | poseidon better |
| HTTP/3 | 46.2 | 45.6 | +1.3% | ~equal |
| gRPC | 46.2 | 43.9 | +5.2% | standard better |

## RSS peak (MiB)

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 43.9 | 42.9 | +2.4% | ~equal |
| HTTP/2 | 44.3 | 45.1 | -1.9% | ~equal |
| HTTP/3 | 46.6 | 45.9 | +1.6% | ~equal |
| gRPC | 46.8 | 44.2 | +6.0% | standard better |

## Allocations per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 63.6 | 55.7 | +14.2% | standard better |
| HTTP/2 | 10.5 | 45.5 | -76.9% | poseidon better |
| HTTP/3 | 141.7 | 133.7 | +6.0% | standard better |
| gRPC | 31.3 | 88.1 | -64.4% | poseidon better |

## Bytes allocated per request

| Regime | poseidon | standard | Δ | verdict |
| --- | ---: | ---: | ---: | --- |
| HTTP/1.1 | 27,481 | 15,526 | +77.0% | standard better |
| HTTP/2 | 1,557 | 15,779 | -90.1% | poseidon better |
| HTTP/3 | 43,115 | 21,019 | +105.1% | standard better |
| gRPC | 78,795 | 29,218 | +169.7% | standard better |

## Validity

- HTTP/2 / poseidon: 5 plateau error(s) - sample: `stream: client: stream reset by peer: 7`

## Raw

| File | Requests | Errors | Non-2xx | Achieved RPS |
| --- | ---: | ---: | ---: | ---: |
| `h1-poseidon.json` | 11,936 | 0 | 596 | 198.8 |
| `h1-standard.json` | 11,937 | 0 | 596 | 198.8 |
| `h2-poseidon.json` | 11,935 | 5 | 597 | 198.8 |
| `h2-standard.json` | 11,938 | 0 | 596 | 198.9 |
| `h3-poseidon.json` | 11,937 | 0 | 596 | 198.8 |
| `h3-standard.json` | 11,936 | 0 | 596 | 198.8 |
| `grpc-poseidon.json` | 11,937 | 0 | 0 | 198.8 |
| `grpc-standard.json` | 11,937 | 0 | 0 | 198.8 |
