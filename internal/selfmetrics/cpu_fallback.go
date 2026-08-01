//go:build !linux && !windows

package selfmetrics

// processCPUSeconds has no OS-level source on this platform, so Read falls
// back to Go's runtime metrics (`/cpu/classes/total` minus `/cpu/classes/idle`).
//
// That fallback is genuinely unreliable and the reason the Linux and Windows
// implementations exist. Those metrics are only refreshed during a GC cycle,
// so a process that allocates little can run an entire measurement window
// without one and report a CPU delta of exactly zero. It bit this harness on
// two cells of an eight-cell matrix once payloads were precomputed.
//
// The failure direction is the bad one: GC frequency tracks allocation rate,
// so the arm allocating LESS — the one doing better — gets the more broken
// number. If you are running the driver on a platform that lands here, treat
// the CPU column with suspicion and cross-check against `cpu_runtime_seconds`
// in the report. TestCPUAdvancesUnderLoad will fail here by design.
func processCPUSeconds() (float64, bool) { return 0, false }

// processCPUSplit likewise has no source here.
func processCPUSplit() (user, sys float64, ok bool) { return 0, 0, false }
