package contract

import (
	"math"
	"strconv"
)

// ConfidenceText writes a confidence as human-readable text. The CLI and
// the server share it so every text a person reads shows a confidence the
// same way the web UI does (confidenceNumber in web/src/lib/labels.ts): a
// whole number with one decimal ("1.0"), every other value in its shortest
// exact form ("0.9", "0.85", "0.875", never rounded). JSON output does not
// go through it; there a confidence stays a number.
func ConfidenceText(c float64) string {
	if c == math.Trunc(c) && !math.IsInf(c, 0) {
		return strconv.FormatFloat(c, 'f', 1, 64)
	}
	return strconv.FormatFloat(c, 'f', -1, 64)
}
