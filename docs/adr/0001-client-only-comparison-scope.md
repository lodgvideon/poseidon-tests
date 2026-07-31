# Compare clients only, holding the server fixed

The question this benchmark answers is "what happens if a consumer swaps its
outbound HTTP client for poseidon-http-client" — so the **target server is held
fixed and identical across both arms** of each protocol leg, and only the
calling client library changes.

## Considered Options

Swapping the whole stack (poseidon-client + poseidon-server vs. `net/http`
client + `net/http` server) was considered and rejected: it conflates the
client's contribution with the server's, so a delta in the results could not be
attributed to the client library — which is the thing a potential consumer is
actually deciding about.

## Consequences

See ADR-0003: the fixed server is a purpose-built neutral target, not
`poseidon-http-server`. One `chi` handler is served over stdlib H1/H2 and
quic-go H3; the gRPC leg is a `grpc-go` server whose Echo method mirrors the
same handler's work.

Because the three HTTP legs run literally the same handler, their rows are
comparable to each other as well as within themselves. The gRPC row is not:
different server, mirrored rather than shared handler, and an echo-only
scenario mix.
