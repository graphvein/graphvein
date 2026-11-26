// T026 wording rule (AG7): no string of the architecture UI says
// protected, secure, authenticated, encrypted or enforces. The server's
// own strings are checked by its wording test; this one reads the UI
// sources and the label table, so a new string cannot slip in.
import { describe, expect, it } from "vitest";

const sources = import.meta.glob(["./*.ts", "./*.tsx", "../../lib/labels.ts", "../../api/architecture.ts", "!./*.test.ts", "!./*.test.tsx"], { query: "?raw", import: "default", eager: true });

describe("architecture wording", () => {
  it("never claims protection or encryption", () => {
    const files = Object.entries(sources);
    expect(files.length).toBeGreaterThan(10);
    for (const [file, text] of files) expect(text, file).not.toMatch(/protected|secure|authenticated|encrypted|enforces/i);
  });
});
