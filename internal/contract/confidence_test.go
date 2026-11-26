package contract

import "testing"

// TestConfidenceText checks the shared text form of a confidence against
// the web UI's one number format (confidenceNumber, T041): whole numbers
// with one decimal, every other value in its shortest exact form.
func TestConfidenceText(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{1, "1.0"},
		{0, "0.0"},
		{0.9, "0.9"},
		{0.7, "0.7"},
		{0.6, "0.6"},
		{0.85, "0.85"},
		{0.875, "0.875"},
	} {
		if got := ConfidenceText(tc.in); got != tc.want {
			t.Errorf("ConfidenceText(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
