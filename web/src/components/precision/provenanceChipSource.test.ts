import { describe, expect, it } from "vitest";

import { LABELS } from "@/lib/labels";

// T041 (LBL-C): every visible provenance or confidence label renders
// through the one shared chip (components/precision/ProvChip.tsx). This test
// reads the source of web/src and fails when a component builds a
// provenance label from the label table itself, which is how a label ends
// up as bare text with a look of its own.

// Vite reads every source file as text at test time (no Node file API).
const SOURCES = import.meta.glob<string>(["/src/**/*.ts", "/src/**/*.tsx", "!/src/**/*.test.ts", "!/src/**/*.test.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** PROVENANCE_KEYS are the label table entries that say how sure a stored
 * fact or link is (a provenance, a tier or a rule with its number). */
const PROVENANCE_KEYS = ["edge", "flowTier", "legendTier", "provenance", "projectLink", "linkTier", "chainEdge", "tableAccess", "tableLine", "shapeKind"] as const;

/** CHIP is the shared chip module, the one place that reads these keys
 * for a visible label. */
const CHIP = "/src/components/precision/ProvChip.tsx";

/** DRAWN are the files allowed to read a provenance key without the chip,
 * each for a stated reason; the entry names the keys it may read. */
const DRAWN: Readonly<Record<string, readonly string[]>> = {
  // Flow canvas: the text on a drawn line and on a table card; the line's
  // own stroke (solid, dashed) carries the tier, and the layout measures
  // the text to place the label.
  "/src/sections/api/flow/tiers.ts": ["flowTier", "tableLine"],
  "/src/sections/api/flow/graph.ts": ["tableAccess"],
  // Flow legend: filter toggle chips with a sample line and a line count,
  // not a label of one edge (ADR 0078 Revision 1, remaining gap).
  "/src/sections/api/flow/Legend.tsx": ["legendTier"],
  // Architecture map: the chip on a drawn chain line, sized by the layout.
  "/src/sections/architecture/Line.tsx": ["chainEdge", "linkTier"],
  // The Labels setting: an example of each style's wording, not a fact.
  "/src/shell/LabelControl.tsx": ["edge"],
};

/** reads lists the provenance keys (and the two table helpers) a source
 * text reads from the label table. */
function reads(text: string): string[] {
  const out = new Set<string>();
  for (const k of PROVENANCE_KEYS) {
    if (new RegExp(`\\blabel\\([^,()]*(?:\\([^()]*\\))?,\\s*"${k}"`).test(text) || new RegExp(`\\buseLabel\\(\\s*"${k}"`).test(text)) out.add(k);
  }
  // edgeLabel and factLabel imported from the label table (schema's
  // Verification.ts has a factLabel of its own: a claim's fact name).
  const imp = /import\s*\{([^}]*)\}\s*from\s*"@\/lib\/labels"/.exec(text);
  if (imp !== null) {
    for (const h of ["edgeLabel", "factLabel"]) if (new RegExp(`\\b${h}\\b`).test(imp[1] ?? "")) out.add(h);
  }
  return [...out];
}

describe("one provenance chip (T041 LBL-C)", () => {
  it("lists only keys the label table has", () => {
    for (const k of PROVENANCE_KEYS) expect(LABELS).toHaveProperty(k);
  });

  it("the shared chip module reads the provenance keys", () => {
    const chip = SOURCES[CHIP];
    expect(chip).toBeDefined();
    expect(reads(chip ?? "").sort()).toEqual(["chainEdge", "edgeLabel", "factLabel", "linkTier", "projectLink", "provenance", "shapeKind"].sort());
  });

  it("no other component reads a provenance label for display", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(50);
    const offences: string[] = [];
    for (const [file, text] of Object.entries(SOURCES)) {
      if (file === CHIP || file === "/src/lib/labels.ts") continue;
      const allowed = DRAWN[file] ?? [];
      for (const k of reads(text)) if (!allowed.includes(k)) offences.push(`${file}: reads "${k}" without the shared chip (components/precision/ProvChip.tsx)`);
    }
    expect(offences).toEqual([]);
  });

  it("each allowed file still reads what it is allowed (no stale entry)", () => {
    for (const [file, keys] of Object.entries(DRAWN)) {
      const text = SOURCES[file];
      expect(text, file).toBeDefined();
      const got = reads(text ?? "");
      for (const k of keys) expect(got, `${file} ${k}`).toContain(k);
    }
  });

  it("the detector sees the forms it must refuse", () => {
    expect(reads('const prov = label(style, "projectLink", { kind });')).toEqual(["projectLink"]);
    expect(reads('label(useLabelStyle(), "linkFamily", x); label(useLabelStyle(), "linkTier", t)')).toEqual(["linkTier"]);
    expect(reads('const l = useLabel("provenance", { provenance, confidence });')).toEqual(["provenance"]);
    expect(reads('import { confidenceNumber, edgeLabel } from "@/lib/labels";')).toEqual(["edgeLabel"]);
    expect(reads('label(style, "nodeKind", k)')).toEqual([]);
  });
});
