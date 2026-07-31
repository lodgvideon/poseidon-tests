// Package scenario defines the weighted call mix every arm of the comparison
// drives. Both arms of a regime run the identical mix in the identical order
// (same seed), so any difference in the resulting numbers is attributable to
// the client library rather than to a different sequence of work.
package scenario

import (
	"math/rand/v2"

	"github.com/lodgvideon/poseidon-tests/internal/api"
)

// Kind identifies a scenario shape.
type Kind int

const (
	// Echo POSTs a variable-size body and reads back a body of similar size.
	// Symmetric, and the heaviest on both directions.
	Echo Kind = iota
	// Fetch GETs a variable-size server-generated body. Download-shaped.
	Fetch
	// Ingest POSTs a variable-size body and reads a tiny ack. Upload-shaped.
	Ingest
	// Stream GETs a chunked response read incrementally.
	Stream
	// NotFound exercises the non-2xx path. A client must not treat it as an
	// error; a 404 that is counted as a failure would silently shrink the
	// effective request rate of whichever arm handles it differently.
	NotFound
)

// Scenario is one entry in the mix.
type Scenario struct {
	Kind   Kind
	Name   string
	Method string
	Path   string
	// HasBody reports whether the driver must generate a request body.
	HasBody bool
}

// All is the scenario catalogue, indexed by Kind.
var All = map[Kind]Scenario{
	Echo:     {Echo, "echo", "POST", api.RouteEcho, true},
	Fetch:    {Fetch, "fetch", "GET", api.RouteFetch, false},
	Ingest:   {Ingest, "ingest", "POST", api.RouteIngest, true},
	Stream:   {Stream, "stream", "GET", api.RouteStream, false},
	NotFound: {NotFound, "notfound", "GET", api.RouteNotFound, false},
}

// Weight is a scenario's share of the mix.
type Weight struct {
	Kind   Kind
	Weight int
}

// DefaultMix approximates the shape of real API traffic: mostly reads, a
// meaningful slice of writes, some streaming, and a small error tail.
var DefaultMix = []Weight{
	{Fetch, 40},
	{Echo, 25},
	{Ingest, 20},
	{Stream, 10},
	{NotFound, 5},
}

// GRPCMix is the mix used for the gRPC regime.
//
// gRPC unary has no notion of a path or a method verb, and the target exposes
// a single Echo method, so the HTTP scenario shapes do not map onto it. Rather
// than fake the mapping — sending a body-less call that the server cannot
// unmarshal, which is what an earlier version did and which failed ~55% of
// gRPC calls — the gRPC regime runs echo-shaped calls exclusively, drawn from
// the same payload-size distribution as every other regime.
//
// The consequence is explicit: the gRPC row is a clean poseidon-vs-grpc-go
// comparison on identical work, but it is NOT comparable to the HTTP rows the
// way the HTTP rows are comparable to each other.
var GRPCMix = []Weight{{Echo, 100}}

// MixFor returns the scenario weights appropriate to a regime. The regime is
// passed as a string to avoid an import cycle with package clientset.
func MixFor[T ~string](regime T) []Weight {
	if string(regime) == "grpc" {
		return GRPCMix
	}
	return DefaultMix
}

// Mix picks scenarios according to their weights.
type Mix struct {
	weights []Weight
	total   int
	rng     *rand.Rand
}

// NewMix returns a Mix seeded deterministically from seed and worker, so that
// both arms replay the same scenario sequence.
func NewMix(seed uint64, worker int, weights []Weight) *Mix {
	if len(weights) == 0 {
		weights = DefaultMix
	}
	total := 0
	for _, w := range weights {
		total += w.Weight
	}
	return &Mix{weights: weights, total: total, rng: rand.New(rand.NewPCG(seed, uint64(worker)))}
}

// Next returns the next scenario in the sequence.
func (m *Mix) Next() Scenario {
	n := m.rng.IntN(m.total)
	for _, w := range m.weights {
		n -= w.Weight
		if n < 0 {
			return All[w.Kind]
		}
	}
	return All[m.weights[len(m.weights)-1].Kind]
}
