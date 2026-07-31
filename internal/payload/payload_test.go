package payload

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestDeterminism guards the invariant the entire comparison rests on: both
// arms of a regime must generate byte-identical payload sequences.
//
// If this breaks, the two arms are doing different amounts of work and every
// per-request number in the comparison table is meaningless — while still
// looking perfectly plausible. That failure would be invisible in the results,
// so it gets a test.
func TestDeterminism(t *testing.T) {
	const seed = 42
	for worker := 0; worker < 4; worker++ {
		a := NewGenerator(seed, worker, nil)
		b := NewGenerator(seed, worker, nil)
		for seq := uint64(1); seq <= 50; seq++ {
			ab, err := a.Next(seq, "echo")
			if err != nil {
				t.Fatalf("worker %d seq %d: generator A: %v", worker, seq, err)
			}
			// Copy: Next returns a slice aliasing an internal buffer.
			aCopy := append([]byte(nil), ab...)

			bb, err := b.Next(seq, "echo")
			if err != nil {
				t.Fatalf("worker %d seq %d: generator B: %v", worker, seq, err)
			}
			if !bytes.Equal(aCopy, bb) {
				t.Fatalf("worker %d seq %d: payloads diverged (%d vs %d bytes)",
					worker, seq, len(aCopy), len(bb))
			}
		}
	}
}

// TestWorkersDiffer checks the flip side: different workers must NOT produce
// identical streams, or the mix collapses to one repeated payload and the
// benchmark silently measures a far smaller working set than intended.
func TestWorkersDiffer(t *testing.T) {
	a := NewGenerator(1, 0, nil)
	b := NewGenerator(1, 1, nil)

	same := 0
	const n = 30
	for seq := uint64(1); seq <= n; seq++ {
		ab, err := a.Next(seq, "echo")
		if err != nil {
			t.Fatal(err)
		}
		aCopy := append([]byte(nil), ab...)
		bb, err := b.Next(seq, "echo")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(aCopy, bb) {
			same++
		}
	}
	if same == n {
		t.Fatalf("workers 0 and 1 produced identical payloads for all %d requests", n)
	}
}

// TestSizeVaries asserts payloads are dynamic in size, not just content.
// CONTEXT.md commits to both varying; a fixed-size body would let whichever
// client sizes its buffers well at that one size look artificially good.
func TestSizeVaries(t *testing.T) {
	g := NewGenerator(7, 0, nil)
	min, max := int(^uint(0)>>1), 0
	for seq := uint64(1); seq <= 500; seq++ {
		b, err := g.Next(seq, "fetch")
		if err != nil {
			t.Fatal(err)
		}
		if len(b) < min {
			min = len(b)
		}
		if len(b) > max {
			max = len(b)
		}
	}
	// The configured distribution spans 1 to 1200 items, so the largest
	// payload should dwarf the smallest by orders of magnitude.
	if max < min*10 {
		t.Fatalf("payload sizes too uniform: min=%d max=%d (want max >= 10x min)", min, max)
	}
}

// TestValidJSON makes sure what goes on the wire actually parses — the target
// unmarshals these bodies, and a malformed payload would show up as a target
// error rather than as the generator bug it is.
func TestValidJSON(t *testing.T) {
	g := NewGenerator(3, 0, nil)
	for seq := uint64(1); seq <= 20; seq++ {
		raw, err := g.Next(seq, "echo")
		if err != nil {
			t.Fatal(err)
		}
		var body Body
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("seq %d: payload is not valid JSON: %v", seq, err)
		}
		if body.Seq != seq {
			t.Fatalf("seq %d: round-tripped Seq is %d", seq, body.Seq)
		}
	}
}

