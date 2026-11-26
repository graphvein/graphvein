package mcp

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/traverse"
)

// TestImpactCapDepthHint checks that change_impact's depth bound proposes a
// larger depth only below the maximum (T042 review): at depth 10 a
// larger depth is no way on, so the text points to a dependent at the last
// step, as the chain and callers/impact tools do.
func TestImpactCapDepthHint(t *testing.T) {
	below := impactCap(newEnvelope("change_impact"), traverse.CapDepth, 3)
	if !strings.HasSuffix(below.Text, "; call again with depth up to 10") || below.Limit != 3 {
		t.Errorf("depth 3: got limit %d, text %q", below.Limit, below.Text)
	}
	atMax := impactCap(newEnvelope("change_impact"), traverse.CapDepth, changes.MaxDepth)
	if strings.Contains(atMax.Text, "depth up to") || !strings.HasSuffix(atMax.Text, "; call again from a dependent at the last step to see further") {
		t.Errorf("depth %d: text %q proposes a larger depth or lacks the last-step hint", changes.MaxDepth, atMax.Text)
	}
	if atMax.Limit != changes.MaxDepth {
		t.Errorf("depth %d: limit %d", changes.MaxDepth, atMax.Limit)
	}
	// Other caps carry no depth hint.
	fan := impactCap(newEnvelope("change_impact"), traverse.CapFanOut, changes.MaxDepth)
	if strings.Contains(fan.Text, "call again") {
		t.Errorf("fan-out cap: unexpected hint %q", fan.Text)
	}
}
