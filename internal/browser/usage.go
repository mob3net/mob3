package browser

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// procUsage tracks the browser's memory footprint for the footer.
type procUsage struct {
	rss uint64
}

func (u *procUsage) sample() {
	u.rss = processRSS()
}

func (b *Browser) sampleUsage() {
	b.usage.sample()
}

// footer returns the compact footer readout.
func (u procUsage) footer() string {
	return fmt.Sprintf("MEM %d MB", (u.rss+(1<<19))>>20)
}

// processRSS returns the current resident set size, falling back to Go's total
// OS memory when the platform has no /proc filesystem.
func processRSS() uint64 {
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return pages * uint64(os.Getpagesize())
			}
		}
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}
