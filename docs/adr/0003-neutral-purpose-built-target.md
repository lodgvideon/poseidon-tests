# The target server is neutral and purpose-built, not poseidon-http-server

The fixed target is a single `chi` router written for this benchmark, served
over the three HTTP transports (stdlib `net/http` for HTTP/1.1 and HTTP/2,
`quic-go/http3` for HTTP/3). The gRPC leg runs a `grpc-go` server whose single
Echo method **mirrors** the router's `/v1/echo` handler — it does the same
unmarshal/echo/marshal work on the same bytes, but it is a separate closure,
not the chi router, because gRPC does not dispatch on HTTP method and path.

The original plan used `poseidon-http-server` for the HTTP/1.1, HTTP/2, and
gRPC legs; that was replaced for one hard reason and one soft one.

## Considered Options

**`poseidon-http-server` as target — rejected.**

- *Hard blocker:* it cannot serve plain HTTP/1.1. Without an `Upgrade: h2c`
  header the connection is answered `400 Bad Request — "Only h2c supported"`
  (`server/h2c.go`). HTTP/1.1 exists there only as a doorway into h2c, so the
  H1 leg was impossible as designed.
- *Credibility:* for the H2 and gRPC legs it would have worked, but it makes
  poseidon-client the only arm talking to a sibling implementation by the same
  author, tested against it in CI. The benchmark exists to persuade an external
  consumer, and "you benchmarked your client against your own server" is a
  cheap and fair objection we can simply design away.

**Caddy as the neutral target — rejected.** Neutral, and already an interop
peer in the client's own test suite, but it means configuring and deploying a
separate product, and its H1/H2/H3 handlers are not literally the same code
path, which weakens cross-row comparability.

## Consequences

- The **three HTTP rows** are comparable to each other, not merely within
  themselves, because they run byte-identical application logic — literally the
  same `chi` router — behind a different transport, and all three run over TLS.
  This is better than what ADR-0001 originally warned about.
- The **gRPC row is not comparable to the HTTP rows**. Its target is a
  different server, its handler is a mirror rather than the same code, and it
  runs an echo-only scenario mix (see `scenario.GRPCMix`). It is a valid
  poseidon-vs-grpc-go comparison within itself and nothing more.
- The gRPC leg carries **no protobuf**: both arms exchange the same raw JSON
  bytes used by the HTTP legs, via a pass-through codec (`grpc.ForceCodec`) on
  the grpc-go side and poseidon's natively-`[]byte` message API on the other.
  Payload is therefore constant across all four regimes, and `protoc` stays out
  of the build.
- We now own and must maintain the target. That is accepted: it is a few
  hundred lines, and owning it is what guarantees the four transports share one
  handler.
