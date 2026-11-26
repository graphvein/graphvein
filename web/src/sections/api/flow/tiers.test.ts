import { describe, expect, it } from "vitest";

import { candidateLabel, edgeTier, tierLabel, tierText, type TierFields } from "@/sections/api/flow/tiers";

const e = (kind: string, provenance: string, confidence: number, match: string | null = null): TierFields => ({ kind, provenance, confidence, match });

describe("edge tiers (D3)", () => {
  it("maps every row of the table", () => {
    expect(edgeTier(e("exposes_route", "fact", 1))).toBe("fact");
    expect(edgeTier(e("exposes_command", "fact", 1))).toBe("fact");
    expect(edgeTier(e("invokes_command", "fact", 1))).toBe("fact");
    expect(edgeTier(e("calls_route", "inferred", 0.9, "exact"))).toBe("exact");
    expect(edgeTier(e("calls_route", "inferred", 0.7, "exact"))).toBe("exact");
    expect(edgeTier(e("calls_route", "inferred", 0.6, "suffix"))).toBe("suffix");
    expect(edgeTier(e("calls", "fact", 1))).toBe("call");
    expect(edgeTier(e("contains", "fact", 1))).toBe("contains");
  });

  it("never guesses a tier from one field", () => {
    // A null match, an unknown kind or provenance, and inferred 1.0 are
    // all "other", never a fact or a tier with a confidence label.
    expect(edgeTier(e("calls_route", "inferred", 0.9, null))).toBe("other");
    expect(edgeTier(e("calls_route", "inferred", 0.9, "fuzzy"))).toBe("other");
    expect(edgeTier(e("exposes_route", "inferred", 1))).toBe("other");
    expect(edgeTier(e("invokes_command", "fact", 0.9))).toBe("other");
    expect(edgeTier(e("renders", "fact", 1))).toBe("other");
    expect(edgeTier(e("calls", "assessment", 0.5))).toBe("other");
    expect(edgeTier(e("calls_route", "fact", 1, "exact"))).toBe("other");
  });

  it("labels each tier from the stored fields", () => {
    // T025, T041: the known tiers in plain words with the edge's stored
    // confidence beside them.
    expect(tierLabel(e("exposes_route", "fact", 1))).toBe("Fact · 1.0");
    expect(tierLabel(e("calls_route", "inferred", 0.7, "exact"))).toBe("Exact path match · 0.7");
    expect(tierLabel(e("calls_route", "inferred", 0.6, "suffix"))).toBe("Path suffix match · 0.6");
    expect(tierLabel(e("calls", "fact", 1))).toBeNull();
    expect(tierLabel(e("contains", "fact", 1))).toBe("defined in");
    expect(tierLabel(e("calls", "assessment", 0.5))).toBe("calls assessment 0.5");
    expect(tierLabel(e("calls_route", "inferred", 0.9, "fuzzy"))).toBe("calls_route inferred 0.9 fuzzy");
  });

  it("labels the worded tiers with their stored confidence in the technical style (T025b)", () => {
    expect(tierLabel(e("exposes_route", "fact", 1), undefined, "technical")).toBe("fact 1.0");
    expect(tierLabel(e("calls_route", "inferred", 0.7, "exact"), undefined, "technical")).toBe("exact 0.7");
    expect(tierLabel(e("calls_route", "inferred", 0.6, "suffix"), undefined, "technical")).toBe("suffix 0.6");
    expect(tierText(e("calls_route", "inferred", 0.6, "suffix"), undefined, "technical")?.title).toBe("Path suffix match");
    expect(tierText(e("calls_route", "inferred", 0.6, "suffix"))?.title).toBe("suffix 0.6. The number is a fixed rank per match rule, not a probability.");
    // Tiers T025 did not reword read the same in both styles.
    expect(tierLabel(e("calls", "fact", 1), undefined, "technical")).toBeNull();
    expect(tierLabel(e("contains", "fact", 1), undefined, "technical")).toBe("defined in");
    expect(tierLabel(e("calls_route", "inferred", 0.9, "fuzzy"), undefined, "technical")).toBe("calls_route inferred 0.9 fuzzy");
  });

  it("labels near-miss candidates, with at least N for a lower bound", () => {
    expect(candidateLabel("ambiguous_candidate", 2, true)).toBe("ambiguous · 1 of 2");
    expect(candidateLabel("test_only_candidate", 1, true)).toBe("test-only · 1 of 1");
    expect(candidateLabel("ambiguous_candidate", 10, false)).toBe("ambiguous · 1 of at least 10");
    expect(candidateLabel("new_relation", 3, true)).toBe("new_relation · 1 of 3");
  });
});
