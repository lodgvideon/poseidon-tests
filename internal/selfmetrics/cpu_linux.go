//go:build linux

package selfmetrics

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// userHZ is the unit /proc/self/stat reports CPU time in. The kernel fixes
// this at 100 for procfs regardless of CONFIG_HZ, which is why every procfs
// reader (cAdvisor included) divides by 100 rather than calling sysconf.
const userHZ = 100.0

// processCPUSeconds returns cumulative process CPU (utime + stime) from
// /proc/self/stat. This is the source that matters: the driver does its real
// measured work in a Linux container.
//
// It exists because Go's /cpu/classes/* metrics are NOT safe to difference
// over a short window — they are only refreshed during a GC cycle. See the
// package-level note in cpu_fallback.go and TestCPUAdvancesUnderLoad.
func processCPUSeconds() (float64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// Field 2 (comm) is parenthesised and may itself contain spaces, so scan
	// from the closing paren rather than splitting the whole record.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 >= len(b) {
		return 0, false
	}
	fields := strings.Fields(string(b[i+2:]))
	// utime and stime are fields 14 and 15 of the full record, i.e. indices
	// 11 and 12 once comm and state have been consumed.
	if len(fields) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return float64(utime+stime) / userHZ, true
}
