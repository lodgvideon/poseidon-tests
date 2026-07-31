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
	// Kernel and user FILETIMEs are DURATIONS, not timestamps. Filetime.Nanoseconds()
	// subtracts the 1601->1970 epoch offset, which is correct for a wall-clock
	// FILETIME and badly wrong here — it made absolute CPUBusySeconds a large
	// negative number. Deltas still came out right, which is why the plateau
	// figures were unaffected and the bug survived, but the absolute field was
	// nonsense. Combine the raw 100-nanosecond tick counts instead.
	ticks := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	ticks += uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	return float64(ticks) / 1e7, true
}
