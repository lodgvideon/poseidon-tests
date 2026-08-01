package selfmetrics

import (
	"runtime"
	"testing"
	"time"
)

// TestRuntimeCPUFieldsFreezeWithoutGC pins down a property of the runtime CPU
// source that is easy to forget and expensive to rediscover: /cpu/classes/* is
// refreshed at GC mark termination and nowhere else, so a window containing no
// GC yields a delta of exactly zero on every runtime-sourced CPU field.
//
// This is not a bug being tolerated — it is the reason CPUBusySeconds comes
// from the OS. The test exists so that if a future Go release starts
// refreshing these metrics continuously (runtime/metrics.go carries a TODO to
// do exactly that), the change is noticed here rather than silently changing
// what the report means.
//
// It also asserts the property that actually matters for reporting: the
// OS-sourced fields advance under the same load, and Delta.RuntimeCPUUsable
// correctly refuses the runtime figures.
func TestRuntimeCPUFieldsFreezeWithoutGC(t *testing.T) {
	start := Read()

	// Spin without allocating: no allocation means no GC, which is the shape
	// that froze the CPU column once the harness stopped allocating per
	// request. 300ms is far longer than a GC period under any real load.
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

	if d.GCCycles != 0 {
		t.Skipf("a GC ran during the window (%d cycles) — background allocation "+
			"defeated the premise, not the instrument", d.GCCycles)
	}

	t.Logf("wall=%v busy(OS)=%.4f user(OS)=%.4f sys(OS)=%.4f runtime=%.6f rt_user=%.6f rt_gc=%.6f rt_window=%v",
		d.Duration, d.CPUBusySeconds, d.CPUProcUserSeconds, d.CPUProcSysSeconds,
		d.CPURuntimeSeconds, d.CPUUserSeconds, d.CPUGCSeconds, d.RuntimeWindow)

	if d.CPUBusySeconds <= 0 {
		t.Fatalf("OS CPU source did not advance across a busy window: %v", d.CPUBusySeconds)
	}
	if d.CPUProcUserSeconds+d.CPUProcSysSeconds <= 0 {
		t.Fatalf("OS user/sys split did not advance: user=%v sys=%v",
			d.CPUProcUserSeconds, d.CPUProcSysSeconds)
	}
	if d.CPURuntimeSeconds != 0 || d.CPUUserSeconds != 0 || d.CPUGCSeconds != 0 {
		t.Errorf("runtime CPU fields advanced without a GC — the Go runtime now "+
			"refreshes /cpu/classes/* outside GC. Re-check whether the report "+
			"should still be treating them as unusable. runtime=%v user=%v gc=%v",
			d.CPURuntimeSeconds, d.CPUUserSeconds, d.CPUGCSeconds)
	}
	if d.RuntimeCPUUsable() {
		t.Errorf("RuntimeCPUUsable() said yes over a window with no GC "+
			"(window=%v of %v)", d.RuntimeWindow, d.Duration)
	}
}
