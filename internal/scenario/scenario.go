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

// PathPool holds precomputed request paths, including the query parameters that
// make server-generated responses vary in size.
//
// This exists because the driver used to issue bare `/v1/fetch` and `/v1/stream`
// paths, so the target fell back to its defaults and returned a byte-identical
// response every time. Half the HTTP mix (fetch 40% + stream 10%) was therefore
// downloading a constant-size body — precisely the fixed-size flattery CONTEXT.md
// rejects by name, and biased toward whichever client reuses a response buffer
// best. See docs/FINDINGS.md.
//
// Paths are precomputed rather than formatted per request for the same reason
// payloads are: string formatting on the hot path would put harness allocations
// back into the numbers being measured.
type PathPool struct {
	fetch  []string
	stream []string
	n      uint64
}

// NewPathPool builds size variants of each parameterised path. The sequences are
// deterministic, so both arms of a regime request identical response sizes.
func NewPathPool(size int) *PathPool {
	if size <= 0 {
		size = 1024
	}
	p := &PathPool{
		fetch:  make([]string, size),
		stream: make([]string, size),
		n:      uint64(size),
	}
	for i := 0; i < size; i++ {
		// 4..203 items spans roughly 0.9 KiB to 45 KiB of response.
		items := 4 + i%200
		p.fetch[i] = All[Fetch].Path + "?n=" + itoa(items) + "&seq=" + itoa(i)
		// 2..17 chunks, so the flushed-chunk count varies too.
		p.stream[i] = All[Stream].Path + "?chunks=" + itoa(2+i%16)
	}
	return p
}

// Path returns the request path for a scenario at a given sequence number.
// Scenarios without parameters return their static path.
func (p *PathPool) Path(k Kind, seq uint64) string {
	switch k {
	case Fetch:
		return p.fetch[seq%p.n]
	case Stream:
		return p.stream[seq%p.n]
	default:
		return All[k].Path
	}
}

// itoa avoids pulling strconv into a package that otherwise has no imports
// beyond the standard rand; it runs only at pool construction.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
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
