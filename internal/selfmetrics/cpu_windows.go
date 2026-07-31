//go:build windows

package selfmetrics

import "syscall"

// processCPUSeconds returns cumulative process CPU (kernel + user) from
// GetProcessTimes.
//
// Windows is not where the benchmark runs, but it is where the harness is
// developed and where local diagnostic runs happen (`driver -profile-dir`
// against a host target). Without this, those local runs would fall back to
// Go's runtime metrics, which freeze between GC cycles and can report a CPU
// delta of exactly zero — see cpu_fallback.go.
func processCPUSeconds() (float64, bool) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, false
	}
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	// Filetime counts 100-nanosecond intervals; Nanoseconds() converts.
	return float64(kernel.Nanoseconds()+user.Nanoseconds()) / 1e9, true
}
