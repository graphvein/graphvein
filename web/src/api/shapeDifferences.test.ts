// The shape_differences parser (T029 W4) reads every comparison the Go
// server serves for the fixtures (their goldens), so Go and TypeScript
// agree, and refuses a direction it does not know.
import { describe, expect, it } from "vitest";

import project from "../../../testdata/fixtures/shape-differences-project.golden.json";
import fixture from "../../../testdata/fixtures/shape-differences.golden.json";

import { ApiFormatError } from "@/api/endpoints";
import { hasShapeDifferences, parseShapeDifferences } from "@/api/shapeDifferences";

describe("parseShapeDifferences", () => {
  it("reads every served comparison of the fixtures", () => {
    let compared = 0;
    let shown = 0;
    let absent = 0;
    for (const callers of Object.values(fixture.endpoints as Record<string, Record<string, unknown>>)) {
      for (const value of Object.values(callers)) {
        const d = parseShapeDifferences(value, "x");
        if (d === null) {
          absent++;
          continue;
        }
        compared++;
        if (hasShapeDifferences(d)) shown++;
        for (const x of d.differences) {
          expect(x.text).toBe(x.direction === "sent_not_read" ? "sent by the frontend, not read by the handler" : "read by the handler, not sent by this call");
          expect(x.text).not.toMatch(/unused|broken|error/);
        }
        expect(d.crossRepository).toBe(false);
      }
    }
    expect(compared).toBeGreaterThan(shown);
    expect(shown).toBeGreaterThan(0);
    expect(absent).toBeGreaterThan(0);
    const cross = Object.values(project as Record<string, unknown>).map((v) => parseShapeDifferences(v, "p"));
    expect(cross.every((d) => d?.crossRepository === true && d.differences.length === 2)).toBe(true);
    expect(Object.keys(cross[0]?.callLinks ?? {})).toEqual(["src/api.ts:5", "src/api.ts:8"]);
  });

  it("is null when absent, and refuses an unknown direction", () => {
    expect(parseShapeDifferences(undefined, "x")).toBeNull();
    expect(parseShapeDifferences(null, "x")).toBeNull();
    const bad = {
      compared: ["body"],
      differences: [{ row: "SD-8", section: "body", field: "a", direction: "maybe", text: "t", call: { file: "a", line: 1, rule: "SD-2", kind: "literal" }, route: { file: "b", line: 2, rule: "SH-F1", kind: "fact" } }],
    };
    expect(() => parseShapeDifferences(bad, "x")).toThrow(ApiFormatError);
  });
});
