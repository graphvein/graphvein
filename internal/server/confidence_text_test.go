package server

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestTierOfWholeNumber checks that a link tier the architecture view
// writes as text uses the same format: an inferred edge stored at 1 reads
// "inferred 1.0", not "inferred 1"; a fact stays "fact".
func TestTierOfWholeNumber(t *testing.T) {
	for _, tc := range []struct {
		e    contract.Edge
		want string
	}{
		{contract.Edge{Provenance: "inferred", Confidence: 1}, "inferred 1.0"},
		{contract.Edge{Provenance: "inferred", Confidence: 0.9}, "inferred 0.9"},
		{contract.Edge{Provenance: "inferred", Confidence: 0.875}, "inferred 0.875"},
		{contract.Edge{Provenance: "fact", Confidence: 1}, "fact"},
	} {
		if got, _ := tierOf(tc.e); got != tc.want {
			t.Errorf("tierOf(%+v) = %q, want %q", tc.e, got, tc.want)
		}
	}
}
