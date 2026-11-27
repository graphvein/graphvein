import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { UnreadFetcher, UnreadPage } from "@/api/unread";
import { UnreadSection } from "@/sections/unread/UnreadSection";
import { craftedSummary } from "@/test/schemaFixtures";

function list<T>(items: T[]) {
  return { total: items.length, truncated: false, items };
}

// A page with no route path gap, or with the one given.
function page(gap: "none" | "failed" | "mount" | "withheld"): UnreadPage {
  return {
    repository: "app",
    scope: "production",
    ide: { available: false, error: null, label: null, editor: null, source: null },
    index: { finished_at: "2026-11-24T21:50:00Z", indexer: { name: "graphvein", version: "x" }, contract_version: 1 },
    builtin_run: true,
    config: "repository",
    config_error: null,
    counts: {},
    route_paths: {
      document: { available: true, reason: null, error: null },
      unplaced: list([]),
      mounts: list([]),
      dynamic: list([]),
      router_forms: list([]),
      failed: list(gap === "failed" ? [{ code: "go_routes_failed", file: "routes.go", line: null, ide: null, expression: null, message: "routes.go: Go route linking failed: boom" }] : []),
      mounts_unresolved: list(gap === "mount" ? [{ code: "route_mount_unresolved", file: "app.js", line: 7, ide: null, expression: "app.use(r)", message: "app.use(r) at 7:1-7:10" }] : []),
      routes_withheld: list(gap === "withheld" ? [{ code: "route_mount_unresolved", file: "webserver/router/router.go", line: 90, ide: null, expression: 'ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it)', message: "m" }] : []),
      forms: 0,
      form_files_not_counted: 0,
    },
    client_calls: { dynamic: list([]), wrapper: list([]) },
    shapes: { routes: 3, without_shape: list([]), shape_invalid: list([]), sections: {}, sections_not_read: list([]) },
    depth_caps: { routes: list([]) },
    files: {
      scope_applied: true,
      not_read: { ...list([]), by_category: [] },
      syntax_errors: list([]),
      records_dropped: list([]),
      structure_outside_contract: list([]),
      scanner_gaps: list([]),
      without_facts: { available: true, reason: null, error: null, by_extension: [], by_category: [], files: list([]) },
    },
    code_calls: { available: true, reason: null, error: null, languages: ["go"], external: 0, unknown: 0, files_counted: 0, files_not_counted: 0, files: list([]) },
  };
}

function fetcher(p: UnreadPage): UnreadFetcher {
  return () => Promise.resolve({ kind: "page", page: p });
}

describe("UnreadSection route paths", () => {
  // JX-F2: dozzle's shape, a route below r.Route(base, ...) with pattern
  // "/rules": listed as the API view shows it, the pattern in the tooltip.
  it("lists a route without a full path by its unknown-prefix path", async () => {
    const p = page("none");
    p.route_paths.unplaced = list([
      {
        route: "route:internal/web/routes.go#GET%20%2Frules",
        method: "GET",
        pattern: "/rules",
        reason: "no_path",
        unknown: null,
        prefix_unknown: { path: "…/api/notifications/rules", reason_text: "prefix unknown: a prefix on the way to the route is not a constant (for example read from configuration)" },
        file: "internal/web/routes.go",
        line: 253,
        ide: null,
      },
    ]);
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    const row = await screen.findByTestId("unplaced-route");
    const link = within(row).getByRole("link");
    expect(link.textContent).toContain("…/api/notifications/rules");
    expect(link.textContent).not.toMatch(/GET\s*\/rules/);
    expect(link.getAttribute("title")).toContain("pattern as written: /rules");
    expect(within(row).getByTestId("unplaced-marker").textContent).toBe("prefix unknown");
  });

  it("says every production route has a full path when no gap is listed", async () => {
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(page("none"))} />);
    expect((await screen.findByTestId("unread-paths-complete")).textContent).toContain("Every production route has a full path");
    expect(screen.getByTestId("unread-document-scope").textContent).toContain("Production code");
  });

  // T066 (baseline defect 12, pdfcraft): with no route in scope the card
  // says there is none, never "every production route has a full path";
  // with files not read it speaks only of the files read.
  it("says no production route was found when the index holds none", async () => {
    const p = page("none");
    p.shapes.routes = 0;
    const { unmount } = render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    const sentence = await screen.findByTestId("unread-paths-complete");
    expect(sentence.textContent).toBe("No production route found, and no route pattern, router form, mount or Go route recognition is recorded as not read or failed.");
    expect(sentence.textContent).not.toContain("full path");
    unmount();

    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "file is not valid UTF-8", test: false }]), by_category: [{ category: "code_with_reader" as const, count: 1 }] };
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    expect((await screen.findByTestId("unread-paths-complete")).textContent).toBe("No production route found in the files read; 1 file was not read.");
  });

  for (const gap of ["failed", "mount", "withheld"] as const) {
    it(`does not say it with a ${gap} gap, and lists it`, async () => {
      render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(page(gap))} />);
      await screen.findByTestId("unread-document-scope");
      expect(screen.queryByTestId("unread-paths-complete")).toBeNull();
      expect(screen.getByText(gap === "failed" ? /Go route recognition failed/ : gap === "mount" ? /no path claimed through them/ : /Routes whose path is withheld/)).toBeTruthy();
    });
  }

  // JX-R2 A1 (owncast): the withheld registration is listed as a route,
  // not under the mounts.
  it("lists a withheld route registration apart from the mounts", async () => {
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(page("withheld"))} />);
    await screen.findByTestId("unread-document-scope");
    expect(screen.queryByText(/Mounts whose router is not known/)).toBeNull();
    expect(screen.getByText(/ANY "\/\*" \(another pattern of the ServeMux/)).toBeTruthy();
    expect(screen.getByText("path withheld")).toBeTruthy();
  });

  // JX-R2 B3: b.js has one Latin-1 byte (file_not_utf8); every list of
  // route path gaps is empty, but GET /b is not known.
  it("speaks only of the files read when a code file was not read", async () => {
    const p = page("none");
    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "file is not valid UTF-8", test: false }]), by_category: [{ category: "code_with_reader" as const, count: 1 }] };
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    const sentence = await screen.findByTestId("unread-paths-complete");
    expect(sentence.textContent).toBe("Every production route in the files read has a full path; 1 file was not read.");
    expect(sentence.textContent).not.toContain("Every production route has a full path");
    expect(screen.getByTestId("unread-files-not-read-link").textContent).toBe("1 file was not read");
    expect(document.getElementById("files-not-read")).not.toBeNull();
  });

  // JX-F4: a file read with syntax errors may hide routes in the part
  // tree-sitter recovered, so the sentence speaks only of the files read
  // without errors and links to that list; with files not read too, both
  // gaps are named, each linking to its list.
  it("speaks only of the files read without errors when files had syntax errors", async () => {
    const p = page("none");
    p.files.syntax_errors = list([{ file: "src/routes.ts", line: 12, ide: null, category: "code_with_reader" as const, stored: true, code: "syntax_error", message: "syntax error", test: false }]);
    const { unmount } = render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    const sentence = await screen.findByTestId("unread-paths-complete");
    expect(sentence.textContent).toBe("Every production route in the files read without errors has a full path; 1 file had syntax errors.");
    expect(screen.getByTestId("unread-syntax-errors-link").textContent).toBe("1 file had syntax errors");
    expect(screen.queryByTestId("unread-files-not-read-link")).toBeNull();
    const target = document.getElementById("syntax-errors");
    expect(target?.textContent).toContain("Files read with syntax errors");
    const scrolled = vi.fn();
    if (target !== null) target.scrollIntoView = scrolled;
    fireEvent.click(screen.getByTestId("unread-syntax-errors-link"));
    expect(scrolled).toHaveBeenCalledTimes(1);
    unmount();

    p.files.not_read = { ...list([{ file: "b.js", line: null, ide: null, category: "code_with_reader" as const, stored: false, code: "file_not_utf8", message: "file is not valid UTF-8", test: false }]), by_category: [{ category: "code_with_reader" as const, count: 1 }] };
    p.files.syntax_errors = list([
      { file: "src/a.ts", line: 3, ide: null, category: "code_with_reader" as const, stored: true, code: "syntax_error", message: "syntax error", test: false },
      { file: "src/b.ts", line: 4, ide: null, category: "code_with_reader" as const, stored: true, code: "syntax_error", message: "syntax error", test: false },
    ]);
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(p)} />);
    expect((await screen.findByTestId("unread-paths-complete")).textContent).toBe("Every production route in the files read without errors has a full path; 1 file was not read; 2 files had syntax errors.");
  });
});

describe("UnreadSection schema part (T070)", () => {
  it("lists the model sources the schema summary names as refused", async () => {
    const schema = craftedSummary({
      repository: "app",
      found: "none",
      refused_sources: [{ tool: "gorm", file: "go.mod", code: "schema_model_not_proven", message: "go.mod: GORM module example.com/app is not read: no gorm.Open site is read" }],
    });
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(page("none"))} schema={schema} />);
    const part = await screen.findByTestId("unread-schema-refused");
    expect(document.getElementById("schema-refused")?.textContent).toContain("Schema: model sources found, not read");
    expect(within(part).getByTestId("unread-schema-item").textContent).toBe("GORM models found, not read · go.modGORM module example.com/app is not read: no gorm.Open site is read");
  });

  it("shows no schema part without a refusal", async () => {
    render(<UnreadSection status={{ kind: "loading" }} fetcher={fetcher(page("none"))} schema={craftedSummary({ refused_sources: [] })} />);
    await screen.findByTestId("unread-paths-complete");
    expect(screen.queryByTestId("unread-schema-refused")).toBeNull();
  });
});
