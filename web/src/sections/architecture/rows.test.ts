import { describe, expect, it } from "vitest";

import type { ComponentResponse } from "@/api/architecture";
import type { PresentedNode } from "@/api/endpoints";
import { LIST_ROWS, buildRows, filterRows, isList, relative } from "@/sections/architecture/rows";
import { archGolden, componentGolden } from "@/test/architectureFixtures";

function route(file: string, method: string, template: string, slash = false): PresentedNode {
  return {
    identity: `route:${file}#${method}%20${encodeURIComponent(template)}`,
    kind: "route",
    level: 4,
    name: `${method} ${template}`,
    qualified_name: `${method} ${template}`,
    file,
    span: { start_line: 3, start_column: 1, end_line: 3, end_column: 9 },
    content_hash: "sha256:x",
    attributes: { method, paths: [{ template: `/api${template}`, trailing_slash: slash }] },
    ide: null,
    file_state: "unchanged",
    source_root: null,
  };
}

describe("expanded rows (AG14)", () => {
  it("groups by family and file, with levels 3 and 4", () => {
    const rows = buildRows(componentGolden(), []);
    expect(rows.map((r) => r.type)).toEqual(["group", "file", "node"]);
    expect(rows[1]).toMatchObject({ level: 3 });
    expect(relative("crates/app/src/lib.rs", "crates/app")).toBe("src/lib.rs");
    expect(relative("other/x.rs", "crates/app")).toBe("other/x.rs");
    expect(relative("x.rs", ".")).toBe("x.rs");
    expect(rows[2]).toMatchObject({ level: 4, text: "items" });
  });

  it("shows the served path, counts linked callers and filters with headers", () => {
    const base = componentGolden();
    const routes = [route("svc/a.py", "GET", "/items", true), ...Array.from({ length: 14 }, (_, i) => route("svc/b.py", "POST", `/x${String(i)}`))];
    const d: ComponentResponse = { ...base, routes, tables: [] };
    const link = archGolden("synthetic").links[0];
    if (link === undefined) throw new Error("fixture");
    const rows = buildRows(d, [{ ...link, members: [{ source: "client_call:a", target: routes[0]?.identity ?? "", method: "GET", path: "/", full_path: null, prefix_path: null, assumptions: [], command: null, tier: "fact", auth: null, source_repository: null, target_repository: null }] }]);
    const first = rows.find((r) => r.type === "node");
    expect(first).toMatchObject({ text: "/api/items/", note: "1 in", method: "GET" });
    // Rows without a member show no count (never "0").
    expect(rows.filter((r) => r.type === "node" && r.note === null)).toHaveLength(14);
    expect(rows.length).toBeGreaterThan(LIST_ROWS);
    expect(isList(rows)).toBe(true);
    const f = filterRows(rows, "x13");
    expect(f.map((r) => r.type)).toEqual(["group", "file", "node"]);
    expect(filterRows(rows, "")).toHaveLength(rows.length);
  });
});
