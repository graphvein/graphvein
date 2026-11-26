import { describe, expect, it } from "vitest";

import { parseSource, sourcePath, SOURCE_STATES } from "@/api/source";
import { goldenNode, noSource, sourceBody } from "@/test/sourceFixtures";

describe("parseSource", () => {
  it("reads an exact answer with its highlight, node and ide label", () => {
    const r = parseSource(sourceBody());
    expect(r.source.state).toBe("exact");
    expect(r.source.highlight).toEqual({ start_line: 4, start_column: 9, end_line: 4, end_column: 30 });
    expect(r.node.identity).toBe(goldenNode().identity);
    expect(r.ide.label).toBe("Open in VS Code");
    expect(r.source.first_line).toBe(1);
  });

  it("keeps the highlight only for exact and text_match", () => {
    for (const state of SOURCE_STATES) {
      const r = parseSource(sourceBody({ state }));
      expect(r.source.highlight !== null).toBe(state === "exact" || state === "text_match");
    }
    // A future state keeps its raw name and never a highlight.
    const future = parseSource(sourceBody({ state: "moved" }));
    expect(future.source.state).toBe("moved");
    expect(future.source.highlight).toBeNull();
  });

  it("reads a missing ide label as null and no-source answers", () => {
    const body = sourceBody(noSource("missing")) as { ide: Record<string, unknown> };
    delete body.ide.label;
    const r = parseSource(body);
    expect(r.ide.label).toBeNull();
    expect(r.source.lines).toEqual([]);
    expect(r.source.first_line).toBeNull();
    expect(r.source.file.line_count).toBeNull();
  });

  it("rejects malformed bodies", () => {
    type Body = { source: Record<string, unknown>; ide: Record<string, unknown> };
    const bad: [string, (b: Body) => void][] = [
      ["lines", (b) => (b.source.lines = [1])],
      ["cut", (b) => (b.source.cut_lines = ["3"])],
      ["state", (b) => delete b.source.state],
      ["backwards", (b) => (b.source.highlight = { start_line: 4, start_column: 9, end_line: 3, end_column: 0 })],
      ["columns", (b) => (b.source.highlight = { start_line: 4, start_column: 9, end_line: 4, end_column: 2 })],
      ["fraction", (b) => (b.source.highlight = { start_line: 4, start_column: 1.5, end_line: 4, end_column: 2 })],
      ["first_line", (b) => (b.source.first_line = null)],
      ["label", (b) => (b.ide.label = 3)],
    ];
    for (const [name, mutate] of bad) {
      const b = sourceBody() as Body;
      mutate(b);
      expect(() => parseSource(b), name).toThrow(/malformed API response/);
    }
  });

  it("builds the request path with an encoded identity", () => {
    expect(sourcePath({ id: "client_call:a b.ts#x?y", context: 16 })).toBe("/api/v0/source?id=client_call%3Aa%20b.ts%23x%3Fy&context=16");
  });
});
