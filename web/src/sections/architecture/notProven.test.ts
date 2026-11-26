import { describe, expect, it } from "vitest";

import { DEFAULT_SHOW } from "@/sections/architecture/model";
import { arrangeNotProven, notProvenCount } from "@/sections/architecture/notProven";
import { archGolden } from "@/test/architectureFixtures";

describe("Not proven list (AG-C2 fixes)", () => {
  it("groups compose services into one item and leaves out services whose image is on a database card", () => {
    const r = archGolden("synthetic");
    const items = arrangeNotProven(r, r.not_proven);
    const groups = items.filter((i) => i.type === "deploy");
    expect(groups).toHaveLength(1);
    const g = groups[0];
    if (g?.type !== "deploy") throw new Error("no group");
    expect(g.tied.map((t) => t.entry.service).sort()).toEqual(["cache", "db"]);
    expect(g.entries.map((e) => e.service ?? e.kind).sort()).toEqual(["docs", "edge", "ingress_not_mapped", "lonely", "mig", "multi", "proxy"]);
    expect(notProvenCount(items)).toBe(4 + 6 + 3);
    // may also use names its card (set added: the three cards share a name).
    const may = items.find((i) => i.type === "entry" && i.entry.kind === "may_also_use");
    expect(may?.type === "entry" ? may.card : null).toBe("sql schema (db)");
  });

  it("leaves out same-name services of the directory that write no image of their own", () => {
    const r = archGolden("synthetic");
    const base = r.not_proven.find((n) => n.service === "db");
    if (base === undefined) throw new Error("fixture");
    const extra = { ...base, file: "deploy/compose.override.yml", database: null, reason: "no build or image of its own, and no same-name service of another compose file in its directory maps" };
    const items = arrangeNotProven(r, [...r.not_proven, extra]);
    const g = items.find((i) => i.type === "deploy");
    expect(g?.type === "deploy" ? g.tied.map((t) => t.entry.file) : []).toContain("deploy/compose.override.yml");
    // A same-name service with an image of its own stays listed.
    const own = { ...extra, file: "deploy/compose.prod.yml", reason: "image only, no build" };
    const items2 = arrangeNotProven(r, [...r.not_proven, own]);
    const g2 = items2.find((i) => i.type === "deploy");
    expect(g2?.type === "deploy" ? g2.entries.map((e) => e.file) : []).toContain("deploy/compose.prod.yml");
  });

  it("says when an entry comes from a component hidden by default", () => {
    const r = archGolden("components");
    const docs = r.components.find((c) => c.hidden_by === "docs");
    if (docs === undefined) throw new Error("fixture");
    const entry = { kind: "calls_not_linked", row: "CE1", component: docs.id, text: "not linked (409): http calls without a stored link", count: 409, service: null, file: null, reason: null, database: null };
    const [hidden] = arrangeNotProven(r, [entry], DEFAULT_SHOW);
    expect(hidden?.type === "entry" ? hidden.hidden : null).toEqual({ hiddenBy: "docs", name: docs.name });
    const [shown] = arrangeNotProven(r, [entry], { ...DEFAULT_SHOW, docs: true });
    expect(shown?.type === "entry" ? shown.hidden : "x").toBeNull();
  });
});
