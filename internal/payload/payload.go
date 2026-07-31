// Package payload generates the dynamic request/response bodies used by every
// regime of the benchmark.
//
// "Dynamic" here means both the *size* and the *content* of a body vary from
// request to request — see CONTEXT.md. That is deliberate: a fixed-size body
// would let whichever client happens to size its buffers well at that one size
// look artificially good, and would hide the JSON marshal/unmarshal
// allocations a real consumer actually pays.
//
// Generation is seeded and deterministic: given the same seed and sequence
// number, every arm of the comparison produces byte-identical bodies. Without
// that, the two arms would be doing measurably different amounts of work and
// the allocation numbers would not be comparable.
package payload

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
)

// Item is one element of a generated collection body.
type Item struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Score float64 `json:"score"`
	Tags  []string `json:"tags"`
}

// Body is the request/response envelope exchanged in every regime.
type Body struct {
	Seq     uint64 `json:"seq"`
	Scenario string `json:"scenario"`
	Items   []Item `json:"items"`
}

// SizeClass describes one bucket of the payload-size distribution.
type SizeClass struct {
	// MinItems and MaxItems bound the item count for this class.
	MinItems, MaxItems int
	// Weight is this class's relative share of generated payloads.
	Weight int
}

// DefaultSizeClasses is the payload-size distribution: mostly small bodies
// with a long tail of large ones, which is what typical API traffic looks
// like. The large class is what exercises buffer growth and streaming paths.
var DefaultSizeClasses = []SizeClass{
	{MinItems: 1, MaxItems: 8, Weight: 60},    // small: ~0.2-1.5 KiB
	{MinItems: 20, MaxItems: 120, Weight: 30}, // medium: ~4-25 KiB
	{MinItems: 400, MaxItems: 1200, Weight: 10}, // large: ~80-250 KiB
}

var kinds = [...]string{"alpha", "beta", "gamma", "delta", "epsilon"}
var tagPool = [...]string{"red", "green", "blue", "fast", "slow", "hot", "cold", "new"}

// Pool is a precomputed set of payloads that a worker cycles through.
//
// Generating a payload per request costs about 80 allocations — measured at
// roughly half of ALL allocations in both arms of the HTTP/1.1 comparison.
// That cost is identical on both sides, so it does not bias the comparison,
// but it is a large constant added to both and it therefore *compresses* the
// reported difference: a client-attributable +60% shows up as +23% once the
// shared harness overhead is averaged in.
//
// Precomputing removes the generator from the hot path entirely. Payloads
// still vary in both size and content from request to request, which is what
// CONTEXT.md commits to; they simply repeat after Size requests. Repetition is
// harmless here because nothing caches bodies — HPACK and QPACK index headers,
// not payloads.
type Pool struct {
	bodies [][]byte
	n      uint64
}

// DefaultPoolSize is large enough to span the size distribution — including
// the 10% large-payload tail — without holding much memory. At 1024 bodies
// averaging a few KiB this is single-digit MiB per worker.
const DefaultPoolSize = 1024

// NewPool precomputes size payloads using the same deterministic generator,
// so two arms with the same seed still see byte-identical sequences.
func NewPool(seed uint64, worker int, classes []SizeClass, size int) (*Pool, error) {
	if size <= 0 {
		size = DefaultPoolSize
	}
	g := NewGenerator(seed, worker, classes)
	p := &Pool{bodies: make([][]byte, size), n: uint64(size)}
	for i := 0; i < size; i++ {
		b, err := g.Next(uint64(i), "mixed")
		if err != nil {
			return nil, err
		}
		// Next returns a slice aliasing the generator's reusable buffer, so
		// each body must be copied out to own its storage.
		p.bodies[i] = append([]byte(nil), b...)
	}
	return p, nil
}

// Next returns the payload for the given sequence number. It allocates
// nothing. The returned slice is owned by the Pool and must not be modified.
func (p *Pool) Next(seq uint64) []byte { return p.bodies[seq%p.n] }

// For returns the payload for one worker's sequence number, hashing the two
// together so concurrent workers walk the pool out of step with each other
// rather than all sending the same body at the same moment.
//
// A Pool is shared by every worker on purpose. Giving each worker its own
// meant memory scaled with worker count — at 32 workers the pools held ~500
// MiB, and the benchmark's RSS column ended up measuring the fixture instead
// of the client, exactly the way per-request generation had swamped the
// allocation column. One shared pool is a fixed ~15 MiB regardless of
// concurrency.
//
// Safe for concurrent use: the pool is immutable after construction.
func (p *Pool) For(worker int, seq uint64) []byte {
	// Two odd multipliers (Knuth's 32-bit golden ratio, and a prime) decorrelate
	// the worker and sequence dimensions without needing a real hash.
	return p.bodies[(seq*2654435761+uint64(worker)*40503)%p.n]
}

// Generator produces deterministic dynamic payloads. It is NOT safe for
// concurrent use; give each worker goroutine its own Generator so that the
// sequence each worker sees is reproducible and lock-free.
//
// Prefer Pool for the request hot path; this is the builder behind it.
type Generator struct {
	rng         *rand.Rand
	classes     []SizeClass
	totalWeight int

	// buf is reused across Marshal calls. Reusing it is what keeps the
	// generator itself from dominating the allocation numbers we are trying
	// to attribute to the client libraries.
	buf   []byte
	items []Item
}

// NewGenerator returns a Generator seeded deterministically from seed and
// worker. Two runs with the same seed and worker count produce identical
// payload sequences, which is what makes the two arms comparable.
func NewGenerator(seed uint64, worker int, classes []SizeClass) *Generator {
	if len(classes) == 0 {
		classes = DefaultSizeClasses
	}
	total := 0
	for _, c := range classes {
		total += c.Weight
	}
	return &Generator{
		rng:         rand.New(rand.NewPCG(seed, uint64(worker))),
		classes:     classes,
		totalWeight: total,
		buf:         make([]byte, 0, 64*1024),
		items:       make([]Item, 0, 1200),
	}
}

// pickClass selects a size class according to the configured weights.
func (g *Generator) pickClass() SizeClass {
	n := g.rng.IntN(g.totalWeight)
	for _, c := range g.classes {
		n -= c.Weight
		if n < 0 {
			return c
		}
	}
	return g.classes[len(g.classes)-1]
}

// Next builds the next body for the given scenario and marshals it to JSON.
//
// The returned slice aliases an internal buffer and is only valid until the
// next call to Next on this Generator. Callers that hand it to an async
// writer must copy it first.
func (g *Generator) Next(seq uint64, scenario string) ([]byte, error) {
	c := g.pickClass()
	n := c.MinItems
	if c.MaxItems > c.MinItems {
		n += g.rng.IntN(c.MaxItems - c.MinItems)
	}

	g.items = g.items[:0]
	for i := 0; i < n; i++ {
		nTags := g.rng.IntN(3)
		tags := make([]string, 0, nTags)
		for t := 0; t < nTags; t++ {
			tags = append(tags, tagPool[g.rng.IntN(len(tagPool))])
		}
		g.items = append(g.items, Item{
			ID:    g.rng.Int64N(1 << 40),
			Name:  "item-" + strconv.Itoa(g.rng.IntN(1_000_000)),
			Kind:  kinds[g.rng.IntN(len(kinds))],
			Score: g.rng.Float64() * 100,
			Tags:  tags,
		})
	}

	b, err := json.Marshal(Body{Seq: seq, Scenario: scenario, Items: g.items})
	if err != nil {
		return nil, err
	}
	g.buf = append(g.buf[:0], b...)
	return g.buf, nil
}
