package scenario

import "testing"

// TestMixDeterminism guards half of the invariant the comparison rests on:
// both arms must issue the same call in the same order. (The other half —
// identical payload bytes — is tested in internal/payload.)
//
// If this breaks, the two arms do different work and every per-request figure
// in the comparison table is meaningless while still looking plausible.
func TestMixDeterminism(t *testing.T) {
	a := NewMix(9, 2, nil)
	b := NewMix(9, 2, nil)
	for i := 0; i < 200; i++ {
		sa, sb := a.Next(), b.Next()
		if sa.Name != sb.Name {
			t.Fatalf("step %d: scenario diverged: %q vs %q", i, sa.Name, sb.Name)
		}
	}
}

// TestGRPCMixIsEchoOnly pins the documented gRPC constraint. The gRPC target
// exposes a single Echo method; a body-less scenario mapped onto it sends a
// nil body that fails to unmarshal server-side. That mistake failed ~55% of
// gRPC calls once already — see docs/FINDINGS.md.
func TestGRPCMixIsEchoOnly(t *testing.T) {
	mix := NewMix(1, 0, MixFor("grpc"))
	for i := 0; i < 100; i++ {
		if s := mix.Next(); !s.HasBody {
			t.Fatalf("gRPC mix produced body-less scenario %q", s.Name)
		}
	}
}

// TestDefaultMixCoversErrorPath makes sure the non-2xx scenario is actually
// reachable. A client that mishandles a 404 must be caught by the benchmark,
// not accommodated by a mix that never sends one.
func TestDefaultMixCoversErrorPath(t *testing.T) {
	mix := NewMix(5, 0, nil)
	seen := map[string]int{}
	const n = 2000
	for i := 0; i < n; i++ {
		seen[mix.Next().Name]++
	}
	for _, want := range []string{"echo", "fetch", "ingest", "stream", "notfound"} {
		if seen[want] == 0 {
			t.Errorf("scenario %q never selected in %d draws", want, n)
		}
	}
	// notfound carries 5% weight; allow generous slack for RNG variance but
	// catch a mix that has silently lost the error path.
	if got := seen["notfound"]; got < n/50 {
		t.Errorf("notfound selected %d/%d times, want roughly %d", got, n, n/20)
	}
}

// TestMixForNonGRPC confirms every HTTP regime shares one mix, which is what
// makes the H1/H2/H3 rows comparable to each other.
func TestMixForNonGRPC(t *testing.T) {
	for _, regime := range []string{"h1", "h2", "h3"} {
		if got := len(MixFor(regime)); got != len(DefaultMix) {
			t.Errorf("regime %q: got %d weights, want the default %d", regime, got, len(DefaultMix))
		}
	}
}
