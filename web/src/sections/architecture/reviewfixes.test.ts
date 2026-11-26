// T026 AG-RF review fixes in the UI: one wording for a query builder's
// table access (C2), the deploy-tied note (C8), served paths in the Links
// CSV (B3) and the unassigned root's kind row (C10).
import { describe, expect, it } from "vitest";

import type { ArchitectureResponse, Link } from "@/api/architecture";
import { label } from "@/lib/labels";
import { kindRow } from "@/sections/architecture/kindText";
import { linksCsv, memberPath } from "@/sections/architecture/LinksTab";

describe("AG-RF review fixes", () => {
  it("words a query builder's access as builds a query on, in both styles (C2)", () => {
    expect(label("plain", "dbAccess", { reads: 2, builds: 13, writes: 4, mayAlsoUse: 0 }).text).toBe("reads 2 · builds a query on 13 · writes 4");
    expect(label("technical", "dbAccess", { reads: 2, builds: 13, writes: 4, mayAlsoUse: 1 }).text).toBe("R 2 · B 13 · W 4 · ? 1");
    expect(label("plain", "tableAccess", { reads: [], writes: [], builds: ["fact"], detail: "" }).text).toBe("Builds a query on · 1.0");
  });

  it("separates services without an image of their own in the tied note (C8)", () => {
    const text = label("plain", "deployTied", { services: ["db (compose.yml)"], followers: ["db (compose.deploy.yml)", "db (compose.override.yml)"], cards: ["SQLModel schema"] }).text;
    expect(text).toBe("Not listed: db (compose.yml), the database service whose image is on the SQLModel schema card; db (compose.deploy.yml), db (compose.override.yml), same-name services without an image of their own");
  });

  it("lists the served path, the pattern as written secondary (B3)", () => {
    const m = { source: "client_call:a", target: "route:b", method: "GET", path: "/", full_path: "/api/v1/items/", prefix_path: null, assumptions: [], command: null, tier: "fact", auth: null, source_repository: null, target_repository: null };
    expect(memberPath(m)).toBe("/api/v1/items/");
    const cell = { value: "", state: "unknown", source: "", rows: [] };
    const link = { id: "l", family: "http", row: "CE1", source: "a", target: "b", count: 1, back_count: 0, by_kind: {}, tiers: {}, weakest: null, methods: ["GET"], members: [m], origins: [], cells: { protocol: cell, auth: cell, tls: cell } } as unknown as Link;
    const data = { components: [], databases: [] } as unknown as ArchitectureResponse;
    expect(linksCsv(data, [link])).toContain("GET /api/v1/items/ (as written /)");
  });

  it("gives the unassigned root its own kind row (C10)", () => {
    expect(kindRow("unassigned_files")).toBe("CK7");
    expect(label("plain", "archKind", { kind: "unassigned_files", framework: null, row: "CK7" }).text).toBe("Unassigned files");
  });
});
