package engine

import (
	"math"
	"strconv"
	"strings"
)

// parseZoomPercent parses a zoom percentage from a flag or about:config field.
// A trailing % is allowed; the value must be between 25 and 200.
func parseZoomPercent(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 25 || v > 200 {
		return 0, false
	}
	return v, true
}

// zoomPercentLabel formats a zoom factor for the config page.
func zoomPercentLabel(z float64) string {
	if z <= 0 {
		z = 1
	}
	return strconv.FormatFloat(math.Round(z*100), 'f', 0, 64) + "%"
}

// cssWidthPx converts terminal columns to CSS pixels at the current zoom.
func cssWidthPx(cols int) int {
	return int(math.Round(float64(cols) * cellW))
}

// cssHeightPx converts terminal rows to CSS pixels at the current zoom.
func cssHeightPx(rows int) int {
	return int(math.Round(float64(rows) * cellH))
}
