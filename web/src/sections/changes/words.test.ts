// The wording tables 1:1 with the values the goldens hold: every change
// kind, label, cause, reason, wording, group, cap and relation a golden
// sends has its own words (no raw fallback), and the principle's cases
// (unknown now, known now, not extracted) never read as removed, added or
// zero.
import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/client";
import { changeStyle } from "@/sections/changes/chips";
import { dirOf, FOLD_AT, foldPaths, groupPaths, SHOW_FIRST } from "@/sections/changes/impact";
import { refusalOf } from "@/sections/changes/refusal";
import {
  baseLabel,
  CAUSE_LABEL,
  CHANGE_LABEL,
  GROUP_LABEL,
  groupEntries,
  isSeed,
  ITEM_LABEL,
  itemChangeLabel,
  itemRouteTitle,
  itemTitle,
  kindWords,
  reasonText,
  refLabel,
  routeTitle,
  shortName,
  sideMissingText,
  sideName,
  VERIFICATION_TITLE,
  verificationTitle,
  wordingText,
} from "@/sections/changes/words";
import { CHANGE_GOLDENS, impactOf, itemsOf, rawItems, summaryOf, type ChangeCase } from "@/test/changesFixtures";
import type { ImpactPath } from "@/api/changes";

const CASES = Object.keys(CHANGE_GOLDENS) as ChangeCase[];
const all = CASES.flatMap((c) => itemsOf(c, { base: "previous", tests: true, limit: 500 }).items);

describe("wording covers every golden value", () => {
  it("change kinds, labels, causes, reasons and wordings", () => {
    for (const it of all) {
      expect(CHANGE_LABEL[it.change], it.change).toBeDefined();
      expect(changeStyle(it.change).sign, it.change).not.toBe("");
      for (const l of it.labels) expect(ITEM_LABEL[l], l).toBeDefined();
      if (it.cause !== null) expect(CAUSE_LABEL[it.cause], it.cause).toBeDefined();
      if (it.reason !== null) expect(reasonText(it.reason), it.reason).not.toBe(it.reason);
      if (it.wording !== null) expect(wordingText(it.wording), it.wording).not.toBe(it.wording);
    }
  });

  it("groups", () => {
    for (const c of CASES) for (const g of summaryOf(c).summary.groups) expect(GROUP_LABEL[g.group], g.group).toBeDefined();
  });

  it("an unknown change kind is shown raw with the neutral style", () => {
    expect(changeStyle("teleported").sign).toBe("");
    expect(itemChangeLabel({ change: "teleported", wording: null })).toBe("teleported");
  });
});

describe("the principle", () => {
  it("unknown now is never removed, known now never added", () => {
    expect(sideMissingText("head", { change: "unknown_now", wording: null })).toContain("not removed");
    expect(sideMissingText("base", { change: "known_now", wording: null })).toContain("not added");
    expect(itemChangeLabel({ change: "unknown_now", wording: null })).toBe("unknown now");
  });

  it("a removed file the head tree lacks reads deleted, else no longer in the index", () => {
    expect(itemChangeLabel({ change: "removed", wording: "deleted" })).toBe("deleted");
    expect(itemChangeLabel({ change: "removed", wording: "no_longer_in_index" })).toBe("no longer in the index");
  });

  it("not extracted groups carry no count", () => {
    const s = summaryOf("previous-rev1-rev2").summary;
    const arch = groupEntries(s.groups, s.not_extracted).find((e) => e.id === "architecture");
    expect(arch).toEqual({ id: "architecture", label: "Architecture", count: null, text: "not extracted" });
    // A proven zero stays a zero.
    expect(groupEntries(s.groups, s.not_extracted).find((e) => e.id === "angular")?.text).toBe("0");
  });
});

describe("names", () => {
  it("shortens identities and edge keys", () => {
    expect(shortName("route:server/router.ts#GET%20%2Fitems%2F%3Aid")).toBe("GET /items/:id");
    expect(shortName("file:server/text.ts#server%2Ftext.ts")).toBe("server/text.ts");
    const edge = all.find((i) => i.record === "edge" && i.kind === "calls_route");
    expect(edge === undefined ? "" : itemTitle(edge)).toMatch(/^.+ → .+$/);
  });

  it("bases", () => {
    expect(baseLabel("previous")).toBe("previous run");
    expect(baseLabel("pin:release-1")).toBe("pin release-1");
    // R5-6: one "base..head" form for both ref forms and every chip state.
    expect(baseLabel("ref:main")).toBe("main..current index");
    expect(baseLabel("ref:main..feature")).toBe("main..feature");
    expect(refLabel("main", null)).toBe(baseLabel("ref:main"));
    expect(refLabel("main", "feature")).toBe(baseLabel("ref:main..feature"));
  });

  it("migrations by file, the merge base named (R5-5)", () => {
    const migrations = all.filter((i) => i.kind === "migration" && i.record === "node");
    expect(migrations.length).toBeGreaterThan(0);
    for (const m of migrations) {
      const file = m.after?.file ?? m.before?.file ?? "";
      expect(itemTitle(m), m.identity).toBe(file.slice(file.lastIndexOf("/") + 1));
      expect(itemTitle(m)).toMatch(/\.sql$/);
    }
    const ref = summaryOf("ref-rev1-rev3").summary;
    expect(sideName(ref.base, "ref:rev1..rev3", [], "base")).toBe(`merge base ${(ref.base.commit ?? "").slice(0, 7)}`);
    expect(sideName(ref.head, "ref:rev1..rev3", [], "head")).toBe(`commit ${(ref.head.commit ?? "").slice(0, 7)}`);
  });

  it("verification titles are the documented ones (R5-4)", () => {
    expect(verificationTitle("oracle")).toBe("compared by an oracle on fixtures");
    expect(verificationTitle("mapped")).toBe("mapped, not verified");
    expect(verificationTitle("someday")).toBe("verification someday: not verified");
    for (const t of Object.values(VERIFICATION_TITLE)) expect(t).not.toMatch(/real database|checked against/);
    // Every verification value the goldens' impacts send has its title.
    const values = new Set<string>();
    for (const c of CASES) {
      for (const it of itemsOf(c, { base: "previous", tests: true, limit: 500 }).items) {
        for (const p of impactOf(c, it.id)?.paths ?? []) for (const st of p.steps) if (st.access !== null) values.add(st.access.verification);
      }
    }
    expect(values.size).toBeGreaterThan(0);
    for (const v of values) expect(VERIFICATION_TITLE[v], v).toBeDefined();
  });

  it("seeds are node items of C2-C13 and C16", () => {
    expect(isSeed({ record: "node", row: "C2" })).toBe(true);
    expect(isSeed({ record: "node", row: "C1" })).toBe(false);
    expect(isSeed({ record: "edge", row: "C5" })).toBe(false);
    expect(isSeed({ record: "gap", row: "C15" })).toBe(false);
  });
});

describe("refusals", () => {
  const ctx = { base: "previous", baseText: "previous run", runs: null, retention: null };
  const err = (status: number, code: string) => new ApiError("http", `${code}: x`, status, code);

  it("the first run's base_unknown is 'No comparison yet', a later one 'Base not known'", () => {
    expect(refusalOf(err(404, "base_unknown"), ctx).code).toBe("first_run");
    expect(refusalOf(err(404, "base_unknown"), { ...ctx, base: "pin:gone", runs: [] }).title).toBe("Base not known");
  });

  it.each([
    ["base_expired", 410, "Base run no longer covered"],
    ["reindex_needed", 409, "Re-index before comparing branches"],
    ["git_unavailable", 503, "git not found"],
    ["ref_unknown", 404, "Ref not known or not indexed"],
    ["ref_objects_missing", 422, "Objects for the ref are missing"],
    ["index_changed", 409, "The index changed while loading"],
  ])("%s", (code, status, title) => {
    expect(refusalOf(err(status, code), ctx).title).toBe(title);
  });

  it("an unknown code keeps the server's message", () => {
    expect(refusalOf(err(500, "boom"), ctx).body).toBe("boom: x");
  });

  it("offers only actions that can work (R5-6)", () => {
    const run = (seq: number, commit: string | null) => ({ seq, run_id: `R${String(seq)}`, previous_run: null, initial: seq === 1, indexer: { name: "graphvein", version: "x" }, commit, dirty: commit === null ? null : false, finished_at: "2026-11-14T10:00:00Z", gaps: [], gaps_truncated: false, gaps_fileless: false });
    const one = [run(1, "c4b7a95")];
    const two = [run(2, "429e3b4"), run(1, "c4b7a95")];
    // A deleted pin on a one-run index: no "previous run" (it is refused too).
    expect(refusalOf(err(404, "base_unknown"), { ...ctx, base: "pin:gone", runs: one }).actions).toEqual(["pick"]);
    expect(refusalOf(err(404, "base_unknown"), { ...ctx, base: "pin:gone", runs: two }).actions).toEqual(["pick", "previous"]);
    // The first run outside git: no branch comparison either.
    const first = refusalOf(err(404, "base_unknown"), { ...ctx, runs: [run(1, null)] });
    expect(first.code).toBe("first_run");
    expect(first.actions).toEqual([]);
    expect(first.body).not.toMatch(/branch/);
    expect(refusalOf(err(404, "base_unknown"), { ...ctx, runs: one }).actions).toEqual(["branch"]);
    // git not found never offers a branch; on one run not the previous run.
    expect(refusalOf(err(503, "git_unavailable"), { ...ctx, base: "ref:main", runs: one }).actions).toEqual(["pick"]);
    expect(refusalOf(err(410, "base_expired"), { ...ctx, base: "run:R0", runs: [run(2, null), run(1, null)] }).actions).toEqual(["pick"]);
  });
});

describe("impact grouping and folding", () => {
  const imp = impactOf("previous-rev1-rev2", "function:server/items.ts#formatRow");

  it("groups by relation and wording without losing a path", () => {
    const groups = groupPaths(imp?.paths ?? []);
    expect(groups.reduce((n, g) => n + g.paths.length, 0)).toBe(imp?.paths.length);
    expect(groups.map((g) => g.wording)).toContain("calls the changed code");
  });

  it("folds a long fan-out by directory, every path still counted", () => {
    const base = imp?.paths[0];
    if (base === undefined) throw new Error("golden has no path");
    const many: ImpactPath[] = Array.from({ length: 23 }, (_, i) => ({ ...base, node: `function:${i % 3 === 0 ? "a" : "b/c"}/f${String(i)}.ts#f${String(i)}` }));
    const { shown, folds } = foldPaths(many);
    expect(shown.length).toBe(SHOW_FIRST);
    expect(shown.length + folds.reduce((n, f) => n + f.paths.length, 0)).toBe(23);
    expect(folds.map((f) => f.dir)).toEqual(["b/c/", "a/"]);
    expect(foldPaths(many.slice(0, FOLD_AT)).folds).toEqual([]);
    expect(dirOf("function:main.go#main")).toBe("(root)");
  });
});

describe("route titles (T025)", () => {
  it("shows a path whose prefix is unknown with its marker, never as a full path (T033 UP-29)", () => {
    const prefixed = [{ path: "…/api/version", reason: "route_prefix_unresolved", hop: null, scope: "production" as const }];
    const t = routeTitle("GET /version", [], prefixed);
    expect(t).toMatchObject({ text: "GET …/api/version", marker: "prefix unknown", more: null });
    expect(t.title).toContain("prefix unknown: a prefix on the way to the route is not a constant");
    // A Next.js entry whose basePath is not read is not a full path.
    const next = routeTitle("GET /api/search", [{ template: "/api/search", resolution: "unresolved" }], [{ path: "…/api/search", reason: "nextjs_base_path_unresolved", hop: null, scope: "production" }]);
    expect(next).toMatchObject({ text: "GET …/api/search", marker: "prefix unknown" });
    // A full path still wins.
    expect(routeTitle("GET /", [{ template: "/api/x" }], prefixed)).toMatchObject({ text: "GET /api/x", marker: null });
  });

  it("shows the composed path with the method, as the API section does", () => {
    const t = routeTitle("GET /", [{ template: "/api/v1/items", scope: null }]);
    expect(t).toMatchObject({ text: "GET /api/v1/items", marker: null, more: null });
    expect(t.title).toContain("as written in its file: GET /");
  });

  it("prefers a production path and counts the others", () => {
    const t = routeTitle("GET /x", [
      { template: "/test/x", scope: "test" },
      { template: "/api/x", scope: null },
    ]);
    expect(t).toMatchObject({ text: "GET /api/x", marker: null, more: "1 of 2 paths" });
  });

  it("restores a FastAPI trailing slash and keeps flag variants distinct (ADR 0019)", () => {
    expect(routeTitle("GET /", [{ template: "/api/v1/items", scope: null, trailing_slash: true }]).text).toBe("GET /api/v1/items/");
    const both = routeTitle("GET /", [
      { template: "/api/v1/items", trailing_slash: false },
      { template: "/api/v1/items", trailing_slash: true },
    ]);
    expect(both).toMatchObject({ text: "GET /api/v1/items", more: "1 of 2 paths" });
    expect(both.title).toContain("composed paths: /api/v1/items, /api/v1/items/");
  });

  it("never shows a path as written as the full path", () => {
    // No composed path: the API section's words, never "not mounted".
    expect(routeTitle("GET /", [])).toMatchObject({ text: "GET /", marker: "path not known" });
    // No composed paths in the response at all.
    expect(routeTitle("GET /", undefined)).toMatchObject({ text: "GET /", marker: "path as written" });
    expect(routeTitle("GET /", [{ template: null }])).toMatchObject({ text: "GET /", marker: "path not known" });
  });

  it("a shadowed route is marked shadowed, never not mounted", () => {
    // Every composed path withheld because an earlier mount takes it
    // (shadowed_paths): the route is mounted.
    const t = routeTitle("GET /later/path", [], [], [{ path: "/later/path", by: "app.mount(/later) (app.py:9)" }]);
    expect(t).toMatchObject({ text: "GET /later/path", marker: "shadowed", more: null });
    expect(t.title).toContain("/later/path by app.mount(/later) (app.py:9)");
    // An impact path carries only the flag.
    expect(routeTitle("GET /later/path", [], [], true)).toMatchObject({ marker: "shadowed" });
    expect(routeTitle("GET /later/path", [], [], false)).toMatchObject({ marker: "path not known" });
    // A full path wins over the flag.
    expect(routeTitle("GET /x", [{ template: "/x" }], [], true)).toMatchObject({ text: "GET /x", marker: null });
    const item = { record: "node", kind: "route", name: "GET /later/path", before: null, after: { file: "app.py", span: null, fields: { paths: [], shadowed_paths: [{ path: "/later/path", by: "m" }] } } };
    expect(itemRouteTitle(item as unknown as Parameters<typeof itemRouteTitle>[0])).toMatchObject({ marker: "shadowed" });
  });

  // JX-F4: a registration whose path the Go linker withholds (row CR-44,
  // owncast's ANY /* on the root router) is "path withheld" from the
  // server's mark; without it "path not known"; never "not mounted".
  it("a withheld route is marked path withheld from the server's mark", () => {
    const t = routeTitle("ANY /*", [], [], false, true);
    expect(t).toMatchObject({ text: "ANY /*", marker: "path withheld", more: null });
    expect(t.title).toContain("another pattern of the ServeMux");
    expect(routeTitle("ANY /*", [], [], false, false).title).toContain("this is not proof that it is unreachable");
    // Shadowed wins (stored on the route), a full path wins over both.
    expect(routeTitle("ANY /*", [], [], true, true)).toMatchObject({ marker: "shadowed" });
    expect(routeTitle("ANY /x", [{ template: "/x" }], [], false, true)).toMatchObject({ text: "ANY /x", marker: null });
    const side = { file: "router.go", span: null, fields: { paths: [] }, ide: null };
    const item = { record: "node", kind: "route", name: "ANY /*", before: { ...side, path_withheld: false }, after: { ...side, path_withheld: true } };
    expect(itemRouteTitle(item as unknown as Parameters<typeof itemRouteTitle>[0])).toMatchObject({ marker: "path withheld" });
    expect(itemRouteTitle({ ...item, after: null })).toMatchObject({ marker: "path not known" });
  });

  it("a route item of the goldens takes its head side's stored paths", () => {
    const item = rawItems("previous-rev1-rev2").find((i) => i.identity === "route:server/router.ts#GET%20%2Fitems%2F%3Aid");
    expect(item).toBeDefined();
    const t = itemRouteTitle(item as unknown as Parameters<typeof itemRouteTitle>[0]);
    expect(t?.text).toBe("GET /api/v2/items/{id}");
    expect(itemRouteTitle({ record: "node", kind: "function", name: "f", before: null, after: null })).toBeNull();
  });

  it("says a stored kind in words", () => {
    expect(kindWords("client_call")).toBe("client call");
    expect(kindWords("exposes_route")).toBe("exposes route");
  });
});
