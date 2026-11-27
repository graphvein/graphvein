import { describe, expect, it } from "vitest";

import { parseUnread, unreadPath, type FileWithoutFacts, type UnreadPage } from "@/api/unread";
import { cards, codeFilesNotRead, DOCUMENT_SCOPE_PRODUCTION, DOCUMENT_SCOPE_TESTS, fileGroups, filesNotReadText, NO_ROUTES, NO_ROUTES_IN_FILES_READ, NO_ROUTES_IN_FILES_READ_WITHOUT_ERRORS, pageText, parseUnreadParams, PATHS_COMPLETE, PATHS_COMPLETE_IN_FILES_READ, PATTERN_ONLY, pathsSentence, syntaxErrorsText, projectText, ROUTES_WITHHELD_TITLE, routePathsComplete, unplacedText, unreadHash } from "@/sections/unread/model";

const COUNTS = {
  unplaced_routes: 85,
  mounts: 1,
  dynamic_patterns: 3,
  router_form_files: 1,
  router_forms: 7,
  router_form_files_not_counted: 0,
  dynamic_calls: 4,
  wrapper_calls: 0,
  routes_without_shape: 4,
  routes_shape_invalid: 0,
  routes_with_sections_not_read: 81,
  routes_depth_cap: 0,
  files_not_read: 92,
  files_syntax_errors: 10,
  scanner_gaps: 0,
  files_without_facts: 1020,
  unresolved_calls_external: 4846,
  unresolved_calls_unknown: 9938,
  files_counted: 366,
  files_not_counted: 0,
  route_failures: 0,
  mounts_unresolved: 0,
  routes_withheld: 0,
  files_records_dropped: 0,
  files_structure_outside_contract: 0,
};

function list<T>(items: T[]) {
  return { total: items.length, truncated: false, items };
}

function file(path: string, ext: string, category: FileWithoutFacts["category"]): FileWithoutFacts {
  return { file: path, line: null, ide: null, extension: ext, category, language: "unknown", test: false };
}

const FILES: FileWithoutFacts[] = [
  file("web/a.svg", ".svg", "not_code"),
  file("web/App.vue", ".vue", "code_no_reader"),
  file("docs/b.md", ".md", "not_code"),
  file("LICENSE", "", "other"),
  file("web/List.vue", ".vue", "code_no_reader"),
  file("proto/x.proto", ".proto", "code_no_reader"),
];

function page(): UnreadPage {
  const route = { id: "route:r#GET%20%2Fx", label: "…/x", method: "GET", file: "routes.go", line: 9, ide: null, sections: ["security"], depth_cap: null, depth_limit: null };
  return {
    repository: "dozzle",
    scope: "production",
    ide: { available: true, error: null, label: null, editor: null, source: null },
    index: { finished_at: "2026-11-24T21:50:00Z", indexer: { name: "graphvein", version: "extractors-60" }, contract_version: 1 },
    builtin_run: true,
    config: "repository",
    config_error: null,
    counts: COUNTS,
    route_paths: {
      document: { available: true, reason: null, error: null },
      unplaced: list([{ route: "route:r#GET%20%2Fx", method: "GET", pattern: "/x", reason: "no_path", unknown: null, prefix_unknown: null, file: "routes.go", line: 9, ide: null }]),
      mounts: list([{ key: "mount:k", hop: "r.Route(base,func(...){...})", routes: 85, framework: "chi", file: "routes.go", line: 284, ide: null }]),
      dynamic: list([]),
      router_forms: list([{ file: "routes.go", line: null, ide: null, message: "7 Go router forms are outside the route claim contract", forms: 7, by_form: { escaped_router: 1, dynamic_pattern: 6 } }]),
      failed: list([]),
      mounts_unresolved: list([]),
      routes_withheld: list([]),
      forms: 7,
      form_files_not_counted: 0,
    },
    client_calls: { dynamic: list([{ id: "c", name: "GET url", status: "dynamic", owner: "load", file: "a.ts", line: 1, ide: null }]), wrapper: list([]) },
    shapes: { routes: 85, without_shape: list([]), shape_invalid: list([]), sections: { security: 81 }, sections_not_read: list([route]) },
    depth_caps: { routes: list([]) },
    files: {
      scope_applied: true,
      not_read: { ...list([{ file: "logo.webp", line: null, ide: null, category: "not_code" as const, stored: false, code: "file_binary", message: "binary", test: false }]), by_category: [] },
      syntax_errors: list([]),
      records_dropped: list([]),
      structure_outside_contract: list([]),
      scanner_gaps: list([]),
      without_facts: {
        available: true,
        reason: null,
        error: null,
        by_extension: [
          { extension: ".vue", count: 2 },
          { extension: ".svg", count: 1 },
        ],
        by_category: [
          { category: "code_with_reader", count: 0 },
          { category: "code_no_reader", count: 3 },
          { category: "not_code", count: 2 },
          { category: "other", count: 1 },
        ],
        files: list(FILES),
      },
    },
    code_calls: { available: true, reason: null, error: null, languages: ["go"], external: 4846, unknown: 9938, files_counted: 366, files_not_counted: 0, files: list([]) },
  };
}

describe("couldn't-read model", () => {
  it("builds five cards that never add different units", () => {
    const c = cards(COUNTS);
    expect(c.map((x) => x.id)).toEqual(["route-paths", "calls", "shapes", "depth", "files"]);
    expect(c.map((x) => x.value)).toEqual([85, 4, 81, 0, 1020]);
    // Files without facts are counted apart from files not read.
    expect(c[4]?.title).toBe("files with no fact stored");
    expect(c[4]?.lines).toContain("92 files not read");
    // In-code calls are named, never summed with client calls.
    expect(c[1]?.lines.join(" ")).toContain("4,846 to external code, 9,938 unknown");
  });

  it("says not available, never zero, for a missing count, and names the members", () => {
    const c = cards({ ...COUNTS, files_without_facts: null }, { files_without_facts: ["react"] });
    expect(c[4]?.value).toBeNull();
    expect(c[4]?.missing).toBe("not available for react");
  });

  it("never says unread", () => {
    const text = [...cards(COUNTS).flatMap((x) => [x.title, ...x.lines]), ...pageText(page())].join("\n");
    expect(text).not.toMatch(/\bunread\b/i);
    expect(text).not.toMatch(/skipped/i);
  });

  // B5, B7: the new gaps are named on the cards, listed in the copied
  // text, and each one keeps the full-path sentence away.
  it("names records dropped, route failures, unresolved mounts and structure outside the contract", () => {
    const c = cards({ ...COUNTS, route_failures: 2, mounts_unresolved: 3, files_records_dropped: 1, files_structure_outside_contract: 4 });
    expect(c[0]?.lines).toContain("3 mounts whose router is not known (no path claimed)");
    expect(c[0]?.lines).toContain("2 Go route recognitions failed");
    expect(c[4]?.lines).toContain("1 record dropped as invalid");
    expect(c[4]?.lines).toContain("4 files with TypeScript structure outside the contract");
    const p = page();
    p.route_paths.failed = list([
      { code: "go_routes_failed", file: "", line: null, ide: null, expression: null, message: "Go generated-server recognition failed: boom" },
      { code: "go_routes_failed", file: "routes.go", line: null, ide: null, expression: null, message: "routes.go: Go route linking failed: boom" },
    ]);
    p.route_paths.mounts_unresolved = list([{ code: "route_mount_unresolved", file: "app.js", line: 7, ide: null, expression: "app.use(r)", message: "app.use(r) at 7:1-7:10" }]);
    p.files.records_dropped = list([{ file: "a.ts", line: null, ide: null, category: "code_with_reader" as const, stored: true, code: "extract_invalid_record", message: "a.ts: node: bad", test: false }]);
    p.files.structure_outside_contract = list([{ file: "b.ts", line: null, ide: null, category: "code_with_reader" as const, stored: true, code: "ts_structure_unread", message: "3 declarations outside", test: false }]);
    const text = pageText(p).join("\n");
    expect(text).toContain("Go generated-server recognition failed: boom");
    expect(text).toContain("app.use(r) at app.js:7");
    expect(text).toContain("a.ts: a.ts: node: bad");
    expect(text).toContain("b.ts: 3 declarations outside");
    expect(text).not.toMatch(/\bunread\b/i);
  });

  it("says every production route has a full path only without any route path gap", () => {
    const clean = page();
    clean.route_paths.unplaced = list([]);
    clean.route_paths.mounts = list([]);
    clean.route_paths.router_forms = list([]);
    expect(routePathsComplete(clean)).toBe(true);
    const gaps: ((p: UnreadPage) => void)[] = [
      (p) => {
        p.route_paths.failed = list([{ code: "go_routes_failed", file: "a.go", line: null, ide: null, expression: null, message: "failed" }]);
      },
      (p) => {
        p.route_paths.mounts_unresolved = list([{ code: "route_mount_unresolved", file: "a.js", line: 1, ide: null, expression: "x", message: "x at 1:1-1:2" }]);
      },
      (p) => {
        p.route_paths.routes_withheld = list([{ code: "route_mount_unresolved", file: "a.go", line: 90, ide: null, expression: 'ANY "/*" (another pattern of the ServeMux, ...)', message: "x" }]);
      },
      (p) => {
        p.files.records_dropped = list([{ file: "a.go", line: null, ide: null, category: "code_with_reader" as const, stored: true, code: "extract_invalid_record", message: "bad", test: false }]);
      },
      (p) => {
        p.route_paths.dynamic = list([{ code: "route_dynamic_pattern", file: "a.go", line: 1, ide: null, expression: "x", message: "x" }]);
      },
      (p) => {
        p.route_paths.document = { available: false, reason: "invalid_config", error: null };
      },
    ];
    for (const add of gaps) {
      const p: UnreadPage = { ...clean, route_paths: { ...clean.route_paths }, files: { ...clean.files } };
      add(p);
      expect(routePathsComplete(p)).toBe(false);
    }
    expect(routePathsComplete(page())).toBe(false);
  });

  // JX-R2 A1 (owncast): a route registration whose path is withheld is
  // counted and listed as a route, never as a mount.
  it("words withheld route registrations as routes, apart from mounts", () => {
    const c = cards({ ...COUNTS, mounts_unresolved: 0, routes_withheld: 1 });
    expect(c[0]?.lines).toContain("1 route whose path is withheld (another pattern or route may answer it; no path claimed)");
    expect(c[0]?.lines.join(" ")).not.toMatch(/mount whose router/);
    const p = page();
    p.route_paths.routes_withheld = list([{ code: "route_mount_unresolved", file: "webserver/router/router.go", line: 90, ide: null, expression: 'ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it)', message: "m" }]);
    const text = pageText(p).join("\n");
    expect(text).toContain(`${ROUTES_WITHHELD_TITLE} (1):`);
    expect(text).toContain('  - ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it) at webserver/router/router.go:90');
    expect(text).not.toContain("Mounts whose router is not known");
  });

  // JX-R2 B3: a code file not read (file_not_utf8) may hold routes, so the
  // full-path sentence then speaks only of the files read.
  it("counts the code files not read for the full-path sentence", () => {
    const p = page();
    expect(codeFilesNotRead(p)).toBe(0);
    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "not UTF-8", test: false }]), by_category: [{ category: "code_with_reader", count: 1 }] };
    expect(codeFilesNotRead(p)).toBe(1);
    expect(`${PATHS_COMPLETE_IN_FILES_READ}; ${filesNotReadText(1)}`).toBe("Every production route in the files read has a full path; 1 file was not read");
    expect(filesNotReadText(3)).toBe("3 files were not read");
    // A file of another category is not counted.
    p.files.not_read = { ...list([{ file: "logo.webp", line: null, ide: null, category: "not_code" as const, stored: false, code: "file_binary", message: "binary", test: false }]), by_category: [{ category: "not_code", count: 1 }, { category: "code_with_reader", count: 0 }] };
    expect(codeFilesNotRead(p)).toBe(0);
  });

  // JX-F4: files read with syntax errors limit the sentence too.
  it("names files read with syntax errors in the full-path sentence", () => {
    const p = page();
    expect(pathsSentence(p)).toEqual({ opening: PATHS_COMPLETE, gaps: [] });
    p.files.syntax_errors = list([{ file: "src/a.ts", line: 3, ide: null, category: "code_with_reader" as const, stored: true, code: "syntax_error", message: "syntax error", test: false }]);
    expect(pathsSentence(p)).toEqual({ opening: "Every production route in the files read without errors has a full path", gaps: [{ list: "syntax-errors", text: "1 file had syntax errors" }] });
    expect(syntaxErrorsText(3)).toBe("3 files had syntax errors");
    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "not UTF-8", test: false }]), by_category: [{ category: "code_with_reader", count: 1 }] };
    expect(pathsSentence(p).gaps.map((g) => g.list)).toEqual(["files-not-read", "syntax-errors"]);
    p.files.syntax_errors = list([]);
    expect(pathsSentence(p)).toEqual({ opening: PATHS_COMPLETE_IN_FILES_READ, gaps: [{ list: "files-not-read", text: "1 file was not read" }] });
  });

  // T066 (defect 12): with no route in scope the sentence says so.
  it("says no production route was found when shapes.routes is 0", () => {
    const p = page();
    p.shapes.routes = 0;
    expect(pathsSentence(p)).toEqual({ opening: NO_ROUTES, gaps: [] });
    p.files.syntax_errors = list([{ file: "src/a.ts", line: 3, ide: null, category: "code_with_reader" as const, stored: true, code: "syntax_error", message: "syntax error", test: false }]);
    expect(pathsSentence(p).opening).toBe(NO_ROUTES_IN_FILES_READ_WITHOUT_ERRORS);
    p.files.syntax_errors = list([]);
    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "not UTF-8", test: false }]), by_category: [{ category: "code_with_reader", count: 1 }] };
    expect(pathsSentence(p)).toEqual({ opening: NO_ROUTES_IN_FILES_READ, gaps: [{ list: "files-not-read", text: "1 file was not read" }] });
  });

  // B6: what the route lists cover, in each scope; the count is the same.
  it("says the route lists cover production routes in every scope", () => {
    expect(cards(COUNTS)[0]?.lines[0]).toBe(DOCUMENT_SCOPE_PRODUCTION);
    const all = cards(COUNTS, {}, "all")[0];
    expect(all?.lines[0]).toBe(DOCUMENT_SCOPE_TESTS);
    expect(all?.value).toBe(85);
    expect(cards(COUNTS, {}, "test")[0]?.lines[0]).toBe(DOCUMENT_SCOPE_TESTS);
  });

  it("puts code first and folds the not-code group", () => {
    const g = fileGroups(FILES, page().files.without_facts.by_category, "");
    expect(g.map((x) => x.category)).toEqual(["code_no_reader", "other", "not_code"]);
    expect(g.map((x) => x.collapsed)).toEqual([false, false, true]);
    expect(g[0]?.count).toBe(3);
    expect(g[0]?.extensions.map((e) => e.extension)).toEqual([".vue", ".proto"]);
  });

  it("filters the long list by path, keeping the category counts", () => {
    const g = fileGroups(FILES, page().files.without_facts.by_category, "WEB/");
    expect(g.map((x) => x.shown)).toEqual([2, 0, 1]);
    expect(g.map((x) => x.count)).toEqual([3, 1, 2]);
  });

  it("copies the page as text with every item's place", () => {
    const text = pageText(page()).join("\n");
    expect(text).toContain("r.Route(base,func(...){...}) at routes.go:284: 85 routes below it");
    expect(text).toContain("GET …/x (security) at routes.go:9");
    expect(text).toContain("logo.webp: file_binary");
    expect(text).toContain(".vue: 2");
  });

  it("reads both bodies and keeps the page state in the hash", () => {
    const p = page();
    // The wire form names a call's owner as an object.
    const wire = { ...p, client_calls: { ...p.client_calls, dynamic: list(p.client_calls.dynamic.items.map((c) => ({ ...c, owner: { id: "function:a.ts#load", kind: "function", qualified_name: c.owner } }))) } };
    const raw = { api_version: 0, ...(JSON.parse(JSON.stringify(wire)) as Record<string, unknown>) };
    const parsed = parseUnread(raw);
    expect(parsed.kind).toBe("page");
    if (parsed.kind === "page") expect(parsed.page).toEqual(p);
    const proj = parseUnread({ api_version: 0, project: "rw", scope: "production", limit: 2000, totals: COUNTS, not_available: { files_without_facts: ["react"] }, members: [raw] });
    expect(proj.kind).toBe("project");
    if (proj.kind === "project") {
      expect(proj.project.members).toHaveLength(1);
      expect(projectText(proj.project).join("\n")).toContain("Project rw, scope production");
    }
    expect(unreadPath({ scope: "all", member: "express" })).toBe("/api/v0/unread?scope=all&limit=2000&repository=express");
    expect(unreadPath({ scope: "production", member: "all" })).toBe("/api/v0/unread?scope=production&limit=2000");
    expect(unreadHash({ tests: true, member: "react" })).toBe("#/unread?tests=1&m=react");
    expect(parseUnreadParams("#/unread?tests=1&m=react")).toEqual({ tests: true, member: "react" });
    expect(unreadHash({ tests: false, member: null })).toBe("#/unread");
  });
});

// JX-F2: a route without a full path is written as the API view writes
// it, in dozzle's shape: the pattern "/rules" below r.Route(base, ...) is
// "…/api/notifications/rules", never "/rules".
describe("routes without a full path", () => {
  const dozzle = {
    route: "route:internal/web/routes.go#GET%20%2Frules",
    method: "GET",
    pattern: "/rules",
    reason: "no_path",
    unknown: "mount or prefix",
    prefix_unknown: { path: "…/api/notifications/rules", reason_text: "prefix unknown: a prefix on the way to the route is not a constant (for example read from configuration)" },
    file: "internal/web/routes.go",
    line: 253,
    ide: null,
  };

  it("shows the unknown-prefix path, with the pattern in the tooltip", () => {
    const u = unplacedText(dozzle);
    expect(u.text).toBe("…/api/notifications/rules");
    expect(u.marker).toBe("prefix unknown");
    expect(u.title).toContain("pattern as written: /rules");
  });

  it("marks a route with no path at all as its pattern, not its path", () => {
    const u = unplacedText({ ...dozzle, prefix_unknown: null });
    expect(u.text).toBe("/rules");
    expect(u.marker).toBe(PATTERN_ONLY);
  });

  it("reads prefix_unknown from the wire, and null from an older server", () => {
    const p = page();
    const wire = { ...p, client_calls: { dynamic: list([]), wrapper: list([]) }, route_paths: { ...p.route_paths, unplaced: list([{ ...dozzle, unknown: { part: "mount_or_prefix", text: "mount or prefix" }, prefix_unknown: { ...dozzle.prefix_unknown, reason: "route_prefix_unresolved", hop: "mount:k", count: 1 } }]) } };
    const parsed = parseUnread({ api_version: 0, ...(JSON.parse(JSON.stringify(wire)) as Record<string, unknown>) });
    if (parsed.kind !== "page") throw new Error("not a page");
    expect(parsed.page.route_paths.unplaced.items[0]?.prefix_unknown).toEqual(dozzle.prefix_unknown);
    const old = { ...p, client_calls: { dynamic: list([]), wrapper: list([]) }, route_paths: { ...p.route_paths, unplaced: list([{ ...dozzle, unknown: null, prefix_unknown: undefined }]) } };
    const parsedOld = parseUnread({ api_version: 0, ...(JSON.parse(JSON.stringify(old)) as Record<string, unknown>) });
    if (parsedOld.kind !== "page") throw new Error("not a page");
    expect(parsedOld.page.route_paths.unplaced.items[0]?.prefix_unknown).toBeNull();
  });

  it("copies the unknown-prefix path as text", () => {
    const p = page();
    p.route_paths.unplaced = list([dozzle]);
    const text = pageText(p).join("\n");
    expect(text).toContain("GET …/api/notifications/rules (prefix unknown) at internal/web/routes.go:253");
    expect(text).not.toContain("GET /rules");
  });
});
