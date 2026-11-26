package main

import "testing"

// TestOpencodeBilledCostLaggingUsage checks that a settled OpenRouter usage
// delta of zero (the key's usage had not caught up yet) does not book the
// run at zero when opencode reported its own cost.
func TestOpencodeBilledCostLaggingUsage(t *testing.T) {
	in := RecordInput{
		Row:         Row{Agent: "opencode", Mode: "base"},
		Transcript:  canned(t, "opencode", "base"),
		CostSettled: true,
		BilledUSD:   0,
	}
	row, _, err := buildRow(in)
	if err != nil {
		t.Fatal(err)
	}
	if row.CostKind != "billed" || row.CostUSD < 0.0027 || row.CostUSD != row.HarnessCostUSD {
		t.Errorf("cost: kind %q usd %v harness %v", row.CostKind, row.CostUSD, row.HarnessCostUSD)
	}
	in.BilledUSD = 0.01
	row, _, _ = buildRow(in)
	if row.CostUSD != 0.01 {
		t.Errorf("larger usage delta not booked: %v", row.CostUSD)
	}
}
