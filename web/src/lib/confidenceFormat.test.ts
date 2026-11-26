import { describe, expect, it } from "vitest";

import { confidenceNumber } from "@/lib/labels";

// T041: one number format for every confidence the UI prints. Every
// displayed confidence goes through confidenceNumber (lib/labels.ts); this
// test reads the source of web/src and fails when a component formats a
// confidence another way (toFixed, String(), a bare interpolation or a
// bare JSX child).

// Vite reads every source file as text at test time (no Node file API, so
// the test runs in the same environment as the others).
const SOURCES = import.meta.glob<string>(["/src/**/*.ts", "/src/**/*.tsx", "!/src/**/*.test.ts", "!/src/**/*.test.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** FORBIDDEN are the ways of printing a confidence other than
 * confidenceNumber. The first five patterns match a value whose name ends in
 * "confidence" (conf, c.confidence, item.confidence ...).
 *
 * Limit (T041 review): the name-keyed patterns catch only identifiers
 * named confidence or conf (bare or as the last member of a path). A
 * confidence held under another name (`c`, `weakest`, `rank`, a tier
 * weight) and printed raw would pass. That covers today's code because
 * every confidence the web UI prints is an API or model field named
 * `confidence` (the provenance's weakest step arrives as one too), read
 * where it is printed; where a value is bound to a short name first (Flow
 * tiers.ts `c`), it is already the confidenceNumber string. A grep of
 * non-test web/src found no other holder at review time. Three patterns
 * are name-free: toFixed(2), a rule word before confidenceNumber without
 * the middle dot, and a bare Fact/Inferred chip. A new holder under
 * another name needs review or a new pattern here. */
const FORBIDDEN: readonly { name: string; re: RegExp }[] = [
  { name: "toFixed on a confidence", re: /\b(?:\w+\.)*(?:confidence|conf)\b\)?\.toFixed\(/i },
  { name: "String() of a confidence", re: /\bString\(\s*(?:\w+\.)*(?:confidence|conf)\s*\)/i },
  { name: "a confidence interpolated raw", re: /\$\{\s*(?:\w+\.)*(?:confidence|conf)\s*\}/i },
  { name: "a confidence as a raw JSX child", re: /(?<![=$\w])\{\s*(?:\w+\.)+(?:confidence|conf)\s*\}/i },
  { name: "a confidence concatenated into text", re: /["'`]\s*\+\s*(?:\w+\.)*(?:confidence|conf)\b/i },
  // The old two-decimal form (0.90, 0.88) under any name: no number in the
  // UI is printed with two fixed decimals.
  { name: "toFixed(2)", re: /\.toFixed\(\s*2\s*\)/ },
  // T041 follow-up: a plain chip says "rule · number". A capitalised rule
  // word written right before the number has no middle dot ("Inferred
  // 0.9"); a bare "Fact" or "Inferred" JSX text is a chip without its
  // number (lib/labels.ts factLabel and withNumber write both).
  { name: "a plain rule word before its number without the middle dot", re: /\b[A-Z][a-z]+\s+\$?\{\s*confidenceNumber\(/ },
  { name: "a fact or inferred chip without its number", re: /(?:>\s*(?:Fact|Inferred)\s*<|^\s*(?:Fact|Inferred)\s*$)/ },
];

/** offences lists "file:line: rule" for every forbidden form in a text. */
function offences(file: string, text: string): string[] {
  const out: string[] = [];
  text.split("\n").forEach((line, i) => {
    for (const f of FORBIDDEN) if (f.re.test(line)) out.push(`${file}:${String(i + 1)}: ${f.name}`);
  });
  return out;
}

describe("one confidence number format (T041)", () => {
  it("prints the stored value in its shortest exact form", () => {
    expect(confidenceNumber(1)).toBe("1.0");
    expect(confidenceNumber(0.9)).toBe("0.9");
    expect(confidenceNumber(0.7)).toBe("0.7");
    expect(confidenceNumber(0.6)).toBe("0.6");
    expect(confidenceNumber(0.85)).toBe("0.85");
    expect(confidenceNumber(0.875)).toBe("0.875");
    expect(confidenceNumber(0)).toBe("0.0");
  });

  it("reads the sources", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(50);
    expect(Object.keys(SOURCES)).toContain("/src/lib/labels.ts");
  });

  it("finds no other confidence format in web/src", () => {
    const found = Object.entries(SOURCES).flatMap(([file, text]) => offences(file, text));
    expect(found).toEqual([]);
  });

  it("catches each forbidden form", () => {
    const bad = [
      "const s = item.confidence.toFixed(2);",
      "return c.confidence.toFixed(1);",
      "title={String(d.confidence)}",
      "text: `inferred ${e.confidence}`,",
      "<span>Inferred {d.confidence}</span>",
      'const t = "inferred " + p.confidence;',
      "return c.toFixed(2);",
      "<span>Inferred {confidenceNumber(d.confidence)}</span>",
      "text: `Inferred ${confidenceNumber(c)}`,",
      "<span>Fact</span>",
      "          Fact",
    ];
    for (const line of bad) expect(offences("x.tsx", line), line).not.toEqual([]);
    const fine = ["confidence={p.confidence}", "data-confidence={confidence ?? undefined}", "text: `inferred ${confidenceNumber(e.confidence)}`,", "{ provenance, confidence }", "<span>{l.text}</span>", "Facts: {facts.join(\", \")}", "`${words} · ${confidenceNumber(c)}`"];
    for (const line of fine) expect(offences("x.tsx", line), line).toEqual([]);
  });
});
