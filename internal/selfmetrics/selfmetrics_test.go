package selfmetrics

import (
	"runtime"
	"testing"
	"time"
)

// TestCPUAdvancesUnderLoad is a regression test for the measurement bug that
// nearly reached the published comparison table.
//
// Go's /cpu/classes/* metrics are only refreshed during a GC cycle. Once the
// driver stopped allocating per request, a low-allocating arm could run a full
// 60-second plateau without a GC, and both plateau snapshots then reported a
// byte-identical CPU figure — a delta of exactly zero millicores. Because GC
// frequency tracks allocation rate, the arm doing BETTER got the more broken
// number, which is the worst possible direction for a comparison to fail in.
//
// This test burns CPU without allocating, which is precisely the shape that
// defeats the runtime-metrics source. If CPUBusySeconds ever stops advancing
// under those conditions, the CPU column has silently gone dead again.
func TestCPUAdvancesUnderLoad(t *testing.T) {
	start := Read()

	// Spin without allocating. A tight integer loop keeps the GC idle, which
	// is exactly the condition that froze the runtime-metrics reading.
	deadline := time.Now().Add(300 * time.Millisecond)
	var sink uint64
	for time.Now().Before(deadline) {
		for i := 0; i < 200000; i++ {
			sink = sink*31 + uint64(i)
		}
	}
	runtime.KeepAlive(sink)

	end := Read()
	d := Sub(start, end)

	if d.CPUBusySeconds <= 0 {
		t.Fatalf("CPU did not advance across a busy window: delta=%v "+
			"(start=%v end=%v). On Linux this means the /proc/self/stat source "+
			"broke; elsewhere it means the runtime-metrics fallback froze, which "+
			"is the bug this test exists to catch.",
			d.CPUBusySeconds, start.CPUBusySeconds, end.CPUBusySeconds)
	}
	if d.Duration <= 0 {
		t.Fatalf("snapshot timestamps did not advance: %v", d.Duration)
	}
}

// TestAllocCountersAdvance guards the headline metric: allocations must be
// counted even across a window with few or no GC cycles.
func TestAllocCountersAdvance(t *testing.T) {
	start := Read()

	sink := make([][]byte, 0, 1024)
	for i := 0; i < 1024; i++ {
		sink = append(sink, make([]byte, 64))
	}
	runtime.KeepAlive(sink)

	end := Read()
	d := Sub(start, end)

	if d.AllocObjects == 0 {
		t.Fatal("alloc object counter did not advance after 1024 allocations")
	}
	if d.AllocBytes == 0 {
		t.Fatal("alloc byte counter did not advance after 1024 allocations")
	}
}

// TestSnapshotFieldsPopulated catches a metric name that has been renamed or
// removed by a Go release: runtime/metrics silently yields a zero-kind sample
// for an unknown name rather than failing, so a typo would quietly zero out a
// whole column.
func TestSnapshotFieldsPopulated(t *testing.T) {
	s := Read()
	if s.TotalMemBytes == 0 {
		t.Error("TotalMemBytes is zero — check /memory/classes/total:bytes still exists")
	}
	if s.Goroutines == 0 {
		t.Error("Goroutines is zero — check /sched/goroutines:goroutines still exists")
	}
	if s.AllocObjects == 0 {
		t.Error("AllocObjects is zero — check /gc/heap/allocs:objects still exists")
	}
	if s.At.IsZero() {
		t.Error("snapshot timestamp is zero")
	}
}
