// Package selfmetrics measures the driver process itself — the process whose
// only meaningful variable is which client library it was linked against.
//
// The cluster has no metrics-server, so nothing external can report the
// driver's CPU or memory. That turns out to be the better design anyway:
// reading Go's own runtime/metrics gives allocation counts, which no
// container-level metric can provide, and allocation count is the headline
// number this benchmark exists to produce.
package selfmetrics

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"
)

// Metric names read from runtime/metrics. Verified present on Go 1.26.
const (
	mCPUTotal    = "/cpu/classes/total:cpu-seconds"
	mCPUIdle     = "/cpu/classes/idle:cpu-seconds"
	mCPUUser     = "/cpu/classes/user:cpu-seconds"
	mCPUGC       = "/cpu/classes/gc/total:cpu-seconds"
	mAllocBytes  = "/gc/heap/allocs:bytes"
	mAllocObjs   = "/gc/heap/allocs:objects"
	mHeapObjects = "/memory/classes/heap/objects:bytes"
	mMemTotal    = "/memory/classes/total:bytes"
	mGoroutines  = "/sched/goroutines:goroutines"
	mGCCycles    = "/gc/cycles/total:gc-cycles"
)

var sampleNames = []string{
	mCPUTotal, mCPUIdle, mCPUUser, mCPUGC,
	mAllocBytes, mAllocObjs, mHeapObjects, mMemTotal, mGoroutines, mGCCycles,
}

// procStart is when this process started measuring, on the monotonic clock.
// Snapshot.UptimeSeconds is measured from here so that it can be compared
// against the runtime's own CPUStatsTotalSeconds/GOMAXPROCS, which is the
// same quantity as of the last GC.
var procStart = time.Now()

// Snapshot is one point-in-time reading of the driver's own resource use.
type Snapshot struct {
	At time.Time `json:"at"`

	// CPUBusySeconds is cumulative CPU time consumed by this process. On
	// Linux it comes from /proc/self/stat; elsewhere it falls back to Go's
	// runtime metrics. See readProcCPU for why the fallback is a last resort.
	CPUBusySeconds float64 `json:"cpu_busy_seconds"`
	// CPUProcUserSeconds and CPUProcSysSeconds are the OS split of
	// CPUBusySeconds (utime / stime on Linux, user / kernel FILETIME on
	// Windows). Like CPUBusySeconds they advance whether or not a GC runs.
	CPUProcUserSeconds float64 `json:"cpu_proc_user_seconds"`
	CPUProcSysSeconds  float64 `json:"cpu_proc_sys_seconds"`

	// CPURuntimeSeconds is the runtime/metrics view (total minus idle), kept
	// alongside so the two can be compared and the fallback audited.
	//
	// CAUTION — the three fields below are ALL frozen between GC cycles.
	// runtime/metrics fills /cpu/classes/* from a struct the runtime only
	// updates at GC mark termination (runtime/mgc.go, the single
	// work.cpuStats.accumulate call; runtime/metrics.go compute() copies it
	// and explicitly does not refresh, see the TODO there). A window with no
	// GC in it therefore yields a delta of exactly zero. CPUStatsTotalSeconds
	// exists to make that visible: it is GOMAXPROCS x wall time as of the last
	// refresh, so CPUStatsTotalSeconds/GOMAXPROCS is the instant these three
	// numbers actually describe, and UptimeSeconds minus that is the staleness.
	CPURuntimeSeconds float64 `json:"cpu_runtime_seconds"`
	CPUUserSeconds    float64 `json:"cpu_user_seconds"`
	CPUGCSeconds      float64 `json:"cpu_gc_seconds"`

	CPUStatsTotalSeconds float64 `json:"cpu_stats_total_seconds"`
	CPUStatsIdleSeconds  float64 `json:"cpu_stats_idle_seconds"`
	UptimeSeconds        float64 `json:"uptime_seconds"`
	GOMAXPROCS           int     `json:"gomaxprocs"`
	GCCycles             uint64  `json:"gc_cycles"`

	// AllocBytes and AllocObjects are cumulative totals since process start.
	// Their delta across the plateau, divided by requests completed in the
	// same window, is the headline allocs/op and B/op.
	AllocBytes   uint64 `json:"alloc_bytes"`
	AllocObjects uint64 `json:"alloc_objects"`

	// HeapObjectsBytes is live heap; TotalMemBytes is everything the Go
	// runtime has mapped. RSSBytes comes from the OS and is 0 off Linux.
	HeapObjectsBytes uint64 `json:"heap_objects_bytes"`
	TotalMemBytes    uint64 `json:"total_mem_bytes"`
	RSSBytes         uint64 `json:"rss_bytes"`

	Goroutines uint64 `json:"goroutines"`
}

// Read takes a snapshot.
func Read() Snapshot {
	samples := make([]metrics.Sample, len(sampleNames))
	for i, n := range sampleNames {
		samples[i].Name = n
	}
	metrics.Read(samples)

	get := func(name string) metrics.Sample {
		for _, s := range samples {
			if s.Name == name {
				return s
			}
		}
		return metrics.Sample{}
	}
	f := func(name string) float64 {
		s := get(name)
		if s.Value.Kind() == metrics.KindFloat64 {
			return s.Value.Float64()
		}
		return 0
	}
	u := func(name string) uint64 {
		s := get(name)
		if s.Value.Kind() == metrics.KindUint64 {
			return s.Value.Uint64()
		}
		return 0
	}

	runtimeCPU := f(mCPUTotal) - f(mCPUIdle)
	cpu := runtimeCPU
	var procUser, procSys float64
	if pu, ps, ok := processCPUSplit(); ok {
		procUser, procSys = pu, ps
		cpu = pu + ps
	}

	return Snapshot{
		At:                 time.Now(),
		CPUBusySeconds:     cpu,
		CPUProcUserSeconds: procUser,
		CPUProcSysSeconds:  procSys,
		CPURuntimeSeconds:  runtimeCPU,
		CPUUserSeconds:     f(mCPUUser),
		CPUGCSeconds:       f(mCPUGC),

		CPUStatsTotalSeconds: f(mCPUTotal),
		CPUStatsIdleSeconds:  f(mCPUIdle),
		UptimeSeconds:        time.Since(procStart).Seconds(),
		GOMAXPROCS:           runtime.GOMAXPROCS(0),
		GCCycles:             u(mGCCycles),
		AllocBytes:           u(mAllocBytes),
		AllocObjects:         u(mAllocObjs),
		HeapObjectsBytes:     u(mHeapObjects),
		TotalMemBytes:        u(mMemTotal),
		RSSBytes:             readRSS(),
		Goroutines:           u(mGoroutines),
	}
}

// readRSS reads VmRSS from /proc/self/status. Returns 0 where unavailable
// (non-Linux), in which case TotalMemBytes stands in for the memory column.
func readRSS() uint64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// Delta is the difference between two snapshots — the plateau window.
type Delta struct {
	Duration time.Duration `json:"duration"`

	// OS-sourced, and valid over any window.
	CPUBusySeconds     float64 `json:"cpu_busy_seconds"`
	CPUProcUserSeconds float64 `json:"cpu_proc_user_seconds"`
	CPUProcSysSeconds  float64 `json:"cpu_proc_sys_seconds"`

	// Runtime-sourced. CPUGCSeconds has no OS equivalent, so it is kept — but
	// it, and CPUUserSeconds, and CPURuntimeSeconds, describe the window
	// [last GC before start, last GC before end], NOT Duration. RuntimeWindow
	// is that window's actual length; GCCycles is how many GCs closed inside
	// it. Divide the runtime figures by RuntimeWindow, never by Duration, and
	// treat them as unusable when RuntimeWindow is 0 (no GC at all) or is far
	// from Duration. See RuntimeCPUUsable.
	CPURuntimeSeconds float64       `json:"cpu_runtime_seconds"`
	CPUUserSeconds    float64       `json:"cpu_user_seconds"`
	CPUGCSeconds      float64       `json:"cpu_gc_seconds"`
	RuntimeWindow     time.Duration `json:"runtime_window"`
	GCCycles          uint64        `json:"gc_cycles"`

	AllocBytes   uint64 `json:"alloc_bytes"`
	AllocObjects uint64 `json:"alloc_objects"`
}

// RuntimeCPUUsable reports whether the runtime-sourced CPU fields cover enough
// of the measured window to be worth quoting. The window they actually cover
// is bounded by where GC cycles happened to land, so a low-allocating arm can
// see it collapse — measured at 51% of a 47.7s plateau on the H1 poseidon arm,
// and at exactly 0% on an 8s one.
func (d Delta) RuntimeCPUUsable() bool {
	if d.Duration <= 0 || d.RuntimeWindow <= 0 {
		return false
	}
	r := d.RuntimeWindow.Seconds() / d.Duration.Seconds()
	return r > 0.9 && r < 1.1
}

// Sub returns end minus start for the cumulative fields.
func Sub(start, end Snapshot) Delta {
	// /cpu/classes/total is GOMAXPROCS x wall time as of the last refresh, so
	// dividing the difference by GOMAXPROCS recovers the wall interval the
	// runtime CPU figures actually span.
	var rtWindow time.Duration
	if gmp := end.GOMAXPROCS; gmp > 0 {
		secs := (end.CPUStatsTotalSeconds - start.CPUStatsTotalSeconds) / float64(gmp)
		rtWindow = time.Duration(secs * float64(time.Second))
	}
	return Delta{
		Duration:           end.At.Sub(start.At),
		CPUBusySeconds:     end.CPUBusySeconds - start.CPUBusySeconds,
		CPUProcUserSeconds: end.CPUProcUserSeconds - start.CPUProcUserSeconds,
		CPUProcSysSeconds:  end.CPUProcSysSeconds - start.CPUProcSysSeconds,
		CPURuntimeSeconds:  end.CPURuntimeSeconds - start.CPURuntimeSeconds,
		CPUUserSeconds:     end.CPUUserSeconds - start.CPUUserSeconds,
		CPUGCSeconds:       end.CPUGCSeconds - start.CPUGCSeconds,
		RuntimeWindow:      rtWindow,
		GCCycles:           end.GCCycles - start.GCCycles,
		AllocBytes:         end.AllocBytes - start.AllocBytes,
		AllocObjects:       end.AllocObjects - start.AllocObjects,
	}
}

// WritePrometheus renders the current snapshot in Prometheus text format,
// labelled with the regime and arm so a single Prometheus can scrape every run
// and Grafana can plot the two arms against each other.
func WritePrometheus(w io.Writer, regime, arm string, s Snapshot, reqs, errs uint64) {
	lbl := fmt.Sprintf(`{regime=%q,arm=%q}`, regime, arm)
	p := func(name, help, typ string, v any) {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s%s %v\n", name, help, name, typ, name, lbl, v)
	}
	p("driver_cpu_busy_seconds_total", "Non-idle CPU seconds consumed by the driver.", "counter", s.CPUBusySeconds)
	p("driver_cpu_gc_seconds_total", "CPU seconds spent in GC.", "counter", s.CPUGCSeconds)
	p("driver_alloc_bytes_total", "Cumulative bytes allocated on the heap.", "counter", s.AllocBytes)
	p("driver_alloc_objects_total", "Cumulative heap objects allocated.", "counter", s.AllocObjects)
	p("driver_heap_objects_bytes", "Live heap object bytes.", "gauge", s.HeapObjectsBytes)
	p("driver_mem_total_bytes", "Total memory mapped by the Go runtime.", "gauge", s.TotalMemBytes)
	p("driver_rss_bytes", "Process resident set size.", "gauge", s.RSSBytes)
	p("driver_goroutines", "Live goroutines.", "gauge", s.Goroutines)
	p("driver_requests_total", "Requests completed.", "counter", reqs)
	p("driver_request_errors_total", "Requests that failed at the transport level.", "counter", errs)
}
