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
)

var sampleNames = []string{
	mCPUTotal, mCPUIdle, mCPUUser, mCPUGC,
	mAllocBytes, mAllocObjs, mHeapObjects, mMemTotal, mGoroutines,
}

// Snapshot is one point-in-time reading of the driver's own resource use.
type Snapshot struct {
	At time.Time `json:"at"`

	// CPUBusySeconds is cumulative CPU time consumed by this process. On
	// Linux it comes from /proc/self/stat; elsewhere it falls back to Go's
	// runtime metrics. See readProcCPU for why the fallback is a last resort.
	CPUBusySeconds float64 `json:"cpu_busy_seconds"`
	// CPURuntimeSeconds is the runtime/metrics view (total minus idle), kept
	// alongside so the two can be compared and the fallback audited.
	CPURuntimeSeconds float64 `json:"cpu_runtime_seconds"`
	CPUUserSeconds    float64 `json:"cpu_user_seconds"`
	CPUGCSeconds      float64 `json:"cpu_gc_seconds"`

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
	if proc, ok := processCPUSeconds(); ok {
		cpu = proc
	}

	return Snapshot{
		At:                time.Now(),
		CPUBusySeconds:    cpu,
		CPURuntimeSeconds: runtimeCPU,
		CPUUserSeconds:    f(mCPUUser),
		CPUGCSeconds:      f(mCPUGC),
		AllocBytes:       u(mAllocBytes),
		AllocObjects:     u(mAllocObjs),
		HeapObjectsBytes: u(mHeapObjects),
		TotalMemBytes:    u(mMemTotal),
		RSSBytes:         readRSS(),
		Goroutines:       u(mGoroutines),
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
	Duration       time.Duration `json:"duration"`
	CPUBusySeconds float64       `json:"cpu_busy_seconds"`
	CPUUserSeconds float64       `json:"cpu_user_seconds"`
	CPUGCSeconds   float64       `json:"cpu_gc_seconds"`
	AllocBytes     uint64        `json:"alloc_bytes"`
	AllocObjects   uint64        `json:"alloc_objects"`
}

// Sub returns end minus start for the cumulative fields.
func Sub(start, end Snapshot) Delta {
	return Delta{
		Duration:       end.At.Sub(start.At),
		CPUBusySeconds: end.CPUBusySeconds - start.CPUBusySeconds,
		CPUUserSeconds: end.CPUUserSeconds - start.CPUUserSeconds,
		CPUGCSeconds:   end.CPUGCSeconds - start.CPUGCSeconds,
		AllocBytes:     end.AllocBytes - start.AllocBytes,
		AllocObjects:   end.AllocObjects - start.AllocObjects,
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
