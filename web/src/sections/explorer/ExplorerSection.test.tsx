// The Explorer section (T029) rendered from the generated document's
// golden with a fake client.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import synthetic from "../../../../internal/openapi/testdata/synthetic.golden.json";

import { parseOpenApi, type ExplorerClient, type PathValueOp } from "@/api/openapi";
import type { StatusView } from "@/hooks/useStatus";
import { ExplorerSection } from "@/sections/explorer/ExplorerSection";
import { goldenStatus } from "@/test/fixtures";
import { project } from "@/test/projectFixtures";
import { setRequestRepository } from "@/api/client";
import { ProjectProvider } from "@/shell/project";

function ok(): StatusView {
  return { kind: "ok", status: goldenStatus("fresh") };
}

interface Edit {
  body: { base_hash: string; operations: PathValueOp[]; apply?: boolean; result_hash?: string };
  token: string;
}

function fakeClient(edits: Edit[] = [], tagReads: string[][] = [], documents: number[] = []): ExplorerClient {
  return {
    document: () => {
      documents.push(1);
      return Promise.resolve({ kind: "document", document: parseOpenApi(structuredClone(synthetic)) });
    },
    memberDocument: (repository) => {
      const d = parseOpenApi(structuredClone(synthetic));
      if (repository === "react") d.counts.operations = 0;
      if (repository === "axum-ddd") return Promise.reject(new Error("no checkout"));
      return Promise.resolve(d);
    },
    text: (format) => Promise.resolve(format === "json" ? JSON.stringify(synthetic, null, 2) : "openapi: 3.1.1\ninfo:\n  title: synthetic\n"),
    pathValues: () =>
      Promise.resolve({ file: "graphvein.yaml", exists: true, hash: "sha256:base", editable: true, reason: null, token: "tok", values: [], unresolved: [], mounts: [] }),
    editPathValues: (body, token) => {
      edits.push({ body, token });
      return Promise.resolve({
        applied: body.apply === true,
        baseHash: "sha256:base",
        resultHash: "sha256:result",
        diff: "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n+  path_values:\n+    - key: x",
        valid: true,
        problems: [],
        changes: ["sets the path value (shown as set by you)"],
      });
    },
    operationTags: (ids) => {
      tagReads.push([...ids]);
      return Promise.resolve(new Map(ids.map((id) => [id, ["items"]])));
    },
  };
}

beforeEach(() => {
  window.history.replaceState(null, "", "#/explorer");
});

async function expand(opId: string) {
  const row = (await screen.findAllByTestId("operation")).find((r) => r.getAttribute("data-op") === opId);
  if (row === undefined) throw new Error(`no row ${opId}`);
  const button = row.querySelector("button");
  if (button === null) throw new Error("no button");
  fireEvent.click(button);
  return row;
}

describe("ExplorerSection (T029)", () => {
  it("answers a Settings link to the path value editors (edit=path-values, T039 ST-F)", async () => {
    window.history.replaceState(null, "", "#/explorer?edit=path-values");
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    expect((await screen.findByTestId("path-values-target")).textContent).toBe('Path values are entered below, under "Unknown path part": 6 routes have a part of the path that is not read.');
    expect(screen.getByTestId("not-in-document").getAttribute("tabindex")).toBe("-1");
  });

  it("says a document has no path value to enter when no route has an unknown part", async () => {
    window.history.replaceState(null, "", "#/explorer?edit=path-values");
    const client = fakeClient();
    client.document = () => {
      const d = parseOpenApi(structuredClone(synthetic));
      d.unresolved = [];
      return Promise.resolve({ kind: "document", document: d });
    };
    render(<ExplorerSection status={ok()} client={client} />);
    expect((await screen.findByTestId("path-values-target")).textContent).toMatch(/^No route of .+ has an unknown path part, so there is no path value to enter here/);
  });

  it("says why a document has no operation from its own counts", async () => {
    const d = parseOpenApi(structuredClone(synthetic));
    d.operations = [];
    d.counts = { ...d.counts, routes: 6, production_routes: 4, any_method: 1, conflicts: 2, unresolved: 0, unrepresentable: 1 };
    d.excludedTest = { routes: 2, pathEntries: 0 };
    render(<ExplorerSection status={ok()} client={{ ...fakeClient(), document: () => Promise.resolve({ kind: "document", document: d }) }} />);
    expect((await screen.findByTestId("no-operations")).textContent).toBe(
      "No operation is in the document: 4 production routes indexed (2 test-scope left out); listed below: 1 any-method entry (never expanded to methods), 2 conflicts, 1 path OpenAPI cannot express.",
    );
  });

  it("groups operations by router file with the document's counts", async () => {
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const groups = await screen.findAllByTestId("operation-group");
    expect(groups.length).toBeGreaterThan(3);
    expect(screen.getAllByTestId("operation").length).toBe(33);
    expect(screen.getByTestId("coverage").textContent).toContain("33operations");
    expect(screen.getByTestId("explorer-summary").textContent).toContain("33 operations generated");
  });

  it("keeps the groups to one column no wider than the section (layout checked in a browser)", async () => {
    // jsdom does no layout: this pins the track that stops a group header's
    // long file path from widening every group past the window (900 px).
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    await screen.findAllByTestId("operation-group");
    expect(screen.getByTestId("operation-groups").className).toContain("grid-cols-[minmax(0,1fr)]");
  });

  it("says not read for a route without a shape and never shows an empty table", async () => {
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const row = await expand("GET /users");
    const detail = within(row).getByTestId("operation-detail");
    for (const key of ["query", "header", "cookie", "body", "responses", "security"]) {
      const s = within(detail).getByTestId(`section-${key}`);
      expect(s.getAttribute("data-state"), key).toBe("not_read");
      expect(s.textContent, key).toContain("Not read by Graphvein");
      expect(s.textContent, key).not.toMatch(/None/);
      expect(within(s).queryAllByTestId("section-item"), key).toEqual([]);
    }
    expect(within(detail).getByTestId("section-path").textContent).toContain("The path has no parameters.");
    // Try it out is offered (part B); it opens the try-it panel.
    expect(within(detail).getByTestId("try-it-slot")).toHaveProperty("disabled", false);
    expect(within(detail).getByTestId("callers-link").getAttribute("href")).toBe(`#/api?sel=${encodeURIComponent("route:src/a.js#GET%20%2Fusers")}&tab=callers`);
    expect(within(detail).getByTestId("marker-shadowed")).toBeTruthy();
    expect(window.location.hash).toBe(`#/explorer?op=${encodeURIComponent("GET /users")}`);
  });

  it("shows proven none with evidence and items with their provenance chip", async () => {
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const row = await expand("POST /api/items/");
    const detail = within(row).getByTestId("operation-detail");
    const query = within(detail).getByTestId("section-query");
    const chips = within(query).getAllByTestId("provenance-chip");
    expect(chips.length).toBeGreaterThan(0);
    expect(chips[0]?.textContent).toContain("Fact");
    expect(chips[0]?.getAttribute("title")).toContain("SH-T1");
    const none = [...detail.querySelectorAll('[data-state="provably_none"]')];
    for (const s of none) expect(within(s as HTMLElement).getByTestId("section-none").textContent).toContain("None, proven");
    // Items kept in x-graphvein are marked as such.
    const kept = detail.querySelectorAll('[data-in-document="false"]');
    for (const k of kept) expect(within(k as HTMLElement).getByTestId("row-tag")).toBeTruthy();
    // The body's presence was not read: said on its heading, never "optional".
    expect(within(within(detail).getByTestId("section-body")).getByTestId("body-presence").textContent).toBe("Request bodyrequired? not read");
  });

  it("shows the header a security scheme reads in the Headers section", async () => {
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const row = await expand("GET /api/items/{id}");
    const headers = within(within(row).getByTestId("operation-detail")).getByTestId("section-header");
    const read = within(headers).getByTestId("read-by-scheme");
    expect(read.textContent).toBe("read by the security scheme HTTPBearer (see Security)");
    expect(read.closest("li")?.textContent).toContain("Authorization");
    expect(headers.textContent).not.toContain("None, proven");
  });

  it("lists unresolved routes with the unknown part and edits a value through preview and apply", async () => {
    const edits: Edit[] = [];
    render(<ExplorerSection status={ok()} client={fakeClient(edits)} />);
    const fold = await screen.findByTestId("unresolved");
    const rows = within(fold).getAllByTestId("unresolved-row");
    expect(rows.length).toBe(6);
    expect(within(rows[0] as HTMLElement).getByTestId("unknown-part").textContent).toBe("‹mount or prefix›");
    const enter = within(rows[0] as HTMLElement).getByTestId("enter-missing-part");
    await waitFor(() => {
      expect(enter).toHaveProperty("disabled", false);
    });
    fireEvent.click(enter);
    const input = screen.getByTestId("path-value-input");
    expect((input as HTMLInputElement).value).toBe("/lorn");
    fireEvent.change(input, { target: { value: "/api/lorn" } });
    fireEvent.click(screen.getByTestId("path-value-preview-button"));
    await screen.findByTestId("path-value-diff");
    expect(edits).toEqual([{ body: { base_hash: "sha256:base", operations: [{ op: "set", key: "route:src/orphan.js#GET%20%2Florn", value: "/api/lorn" }] }, token: "tok" }]);
    fireEvent.click(screen.getByTestId("path-value-apply"));
    await waitFor(() => {
      expect(edits.length).toBe(2);
    });
    expect(edits[1]?.body).toMatchObject({ apply: true, result_hash: "sha256:result", base_hash: "sha256:base" });
  });

  it("sets one prefix for every route under an unresolved mount hop with a preview", async () => {
    const edits: Edit[] = [];
    const client = fakeClient(edits);
    client.pathValues = () =>
      Promise.resolve({
        file: "graphvein.yaml",
        exists: true,
        hash: "sha256:base",
        editable: true,
        reason: null,
        token: "tok",
        values: [],
        unresolved: [],
        mounts: [
          {
            key: "mount:src/orphan.js#%28module%29%20app.use%28base%29",
            hop: "app.use(base,api)",
            file: "src/orphan.js",
            line: 3,
            reason: "route_prefix_unresolved",
            registration: "sha256:hop",
            shadowingNotChecked: false,
            routes: 2,
            examples: [
              { method: "GET", above: "/x", path: "/" },
              { method: "GET", above: "/x", path: "/lorn" },
            ],
            ambiguous: false,
            covers: "hop",
            above: ["/x"],
            framework: "express",
          },
        ],
      });
    render(<ExplorerSection status={ok()} client={client} />);
    const list = await screen.findByTestId("missing-prefixes");
    const set = within(list).getByTestId("set-missing-prefix");
    expect(set.textContent).toContain("Set the missing prefix for all 2 routes under app.use(base,api)");
    fireEvent.click(set);
    const input = screen.getByTestId("path-value-input");
    expect((input as HTMLInputElement).value).toBe("/");
    fireEvent.change(input, { target: { value: "/v1" } });
    const meaning = screen.getByTestId("mount-meaning").textContent;
    expect(meaning).toContain("only the part app.use(base,api)");
    expect(meaning).toContain("read the prefix above it, /x");
    const preview = screen.getByTestId("mount-preview").textContent;
    expect(preview).toContain("GET /x/v1/lorn");
    // Express ignores a trailing slash: "/" below the hop is the prefix.
    expect(preview).toContain("GET /x/v1");
    expect(preview).not.toContain("GET /x/v1/G");
    fireEvent.click(screen.getByTestId("path-value-preview-button"));
    await screen.findByTestId("path-value-diff");
    expect(edits[0]?.body.operations).toEqual([{ op: "set", key: "mount:src/orphan.js#%28module%29%20app.use%28base%29", value: "/v1" }]);
  });

  it("says what a mount value covers: nothing (only /) or the whole prefix from the root", async () => {
    const client = fakeClient([]);
    const hop = (key: string, covers: "nothing" | "root") => ({
      key,
      hop: covers === "nothing" ? "root.Handle(prefix+\"/\",api)" : "mux (router passed to guard)",
      file: "main.go",
      line: 9,
      reason: "route_mount_unresolved",
      registration: "sha256:hop",
      shadowingNotChecked: true,
      routes: 1,
      examples: [{ method: "GET", above: "", path: "/v2/status" }],
      ambiguous: false,
      covers,
      above: [],
      framework: "net/http",
    });
    client.pathValues = () =>
      Promise.resolve({
        file: "graphvein.yaml",
        exists: true,
        hash: "sha256:base",
        editable: true,
        reason: null,
        token: "tok",
        values: [],
        unresolved: [],
        mounts: [hop("mount:main.go#Serve%20a", "nothing"), hop("mount:main.go#ui%20mux", "root")],
      });
    render(<ExplorerSection status={ok()} client={client} />);
    const list = await screen.findByTestId("missing-prefixes");
    fireEvent.click(within(list).getAllByTestId("set-missing-prefix")[0] as HTMLElement);
    expect(screen.getByTestId("mount-meaning").textContent).toContain("The only value is /");
    expect(screen.getByTestId("mount-preview").textContent).toContain("GET /v2/status");
    fireEvent.change(screen.getByTestId("path-value-input"), { target: { value: "/svc" } });
    expect(screen.getByTestId("mount-preview").textContent).toContain("only / is accepted");
    fireEvent.click(screen.getByText("Cancel"));
    fireEvent.click(within(list).getAllByTestId("set-missing-prefix")[1] as HTMLElement);
    expect(screen.getByTestId("mount-meaning").textContent).toContain("the whole prefix from the root: the routers above");
  });

  it("opens the value editor of the missing prefix a link names (prefix= in the hash)", async () => {
    const client = fakeClient([]);
    const hop = (key: string) => ({
      key,
      hop: `app.use(${key.slice(-1)},api)`,
      file: "src/orphan.js",
      line: 3,
      reason: "route_prefix_unresolved",
      registration: "sha256:hop",
      shadowingNotChecked: false,
      routes: 1,
      examples: [{ method: "GET", above: "", path: "/lorn" }],
      ambiguous: false,
      covers: "hop" as const,
      above: [""],
      framework: "express",
    });
    client.pathValues = () => Promise.resolve({ file: "graphvein.yaml", exists: true, hash: "sha256:base", editable: true, reason: null, token: "tok", values: [], unresolved: [], mounts: [hop("mount:a"), hop("mount:b")] });
    window.history.replaceState(null, "", `#/explorer?prefix=${encodeURIComponent("mount:b")}`);
    render(<ExplorerSection status={ok()} client={client} />);
    const list = await screen.findByTestId("missing-prefixes");
    const rows = within(list).getAllByTestId("mount-row");
    // Only the linked hop's editor is open.
    expect(within(rows[1] as HTMLElement).getByTestId("path-value-editor")).toBeTruthy();
    expect(within(rows[0] as HTMLElement).queryByTestId("path-value-editor")).toBeNull();
    fireEvent.click(screen.getByText("Cancel"));
    expect(window.location.hash).not.toContain("prefix=");
  });

  it("says when the missing prefix a link names is not listed", async () => {
    window.history.replaceState(null, "", `#/explorer?prefix=${encodeURIComponent("mount:gone")}`);
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    expect((await screen.findByTestId("linked-prefix-absent")).textContent).toContain("not listed here now");
  });

  it("shows values set by you with their use and warnings", async () => {
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const values = await screen.findByTestId("user-values");
    expect(values.textContent).toContain("set by you, used");
    expect(values.textContent).toContain("set by you, not used");
    expect(values.textContent).toContain("Warning: no registration hash");
  });

  it("groups by OpenAPI tag after reading the documenting operations", async () => {
    const reads: string[][] = [];
    window.history.replaceState(null, "", "#/explorer?group=tag");
    render(<ExplorerSection status={ok()} client={fakeClient([], reads)} />);
    const groups = await screen.findAllByTestId("operation-group");
    expect(groups.map((g) => g.getAttribute("aria-label"))).toEqual(["items", "No tag"]);
    expect(reads.length).toBe(1);
  });

  it("shows the Spec tab with the document text", async () => {
    window.history.replaceState(null, "", "#/explorer?view=spec");
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const text = await screen.findByTestId("spec-text");
    expect(text.textContent).toContain("openapi: 3.1.1");
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    await waitFor(() => {
      expect(screen.getByTestId("spec-text").textContent).toContain('"openapi": "3.1.1"');
    });
  });

  it("filters operations and says how many match", async () => {
    window.history.replaceState(null, "", "#/explorer?q=orders");
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    await screen.findAllByTestId("operation");
    expect(screen.getAllByTestId("operation").length).toBe(3);
    expect(screen.getByTestId("filter-note").textContent).toBe("Showing 3 of 33 operations matching “orders”.");
  });

  it("project mode names each member's operation count from its own document", async () => {
    render(
      <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
        {() => <ExplorerSection status={ok()} client={fakeClient()} />}
      </ProjectProvider>,
    );
    const select = await screen.findByTestId("member-select");
    await waitFor(() => {
      expect([...select.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["react · 0 operations", "express · 33 operations", "axum-ddd · not read"]);
    });
    expect(screen.getByTestId("explorer-summary").textContent).toContain("every count is of synthetic's own document");
    setRequestRepository(null);
  });

  it("reads the document again for a newer index run", async () => {
    const documents: number[] = [];
    const client = fakeClient([], [], documents);
    const { rerender } = render(<ExplorerSection status={ok()} client={client} />);
    await screen.findAllByTestId("operation");
    const newer = goldenStatus("fresh");
    if (newer.index.last_run !== null) newer.index.last_run.finished_at = "2099-01-01T00:00:00Z";
    rerender(<ExplorerSection status={{ kind: "ok", status: newer }} client={client} />);
    await waitFor(() => {
      expect(documents.length).toBe(2);
    });
  });
  it("opens the operations of a route an API section link names, then writes op in the hash (OX-42)", async () => {
    // A route with two path keys (a basePath value): both open, the first
    // is written in the hash.
    const route = "route:docs/app/api/x/route.ts#GET%20%2Fapi%2Fx";
    window.history.replaceState(null, "", `#/explorer?route=${encodeURIComponent(route)}`);
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    await waitFor(() => {
      expect(screen.getAllByTestId("operation-detail")).toHaveLength(2);
    });
    const open = screen.getAllByTestId("operation").filter((r) => r.querySelector("[data-testid=operation-detail]") !== null);
    expect(open.map((r) => r.getAttribute("data-route"))).toEqual([route, route]);
    await waitFor(() => {
      expect(window.location.hash).toBe(`#/explorer?op=${encodeURIComponent("GET /docs")}`);
    });
    expect(screen.queryByTestId("route-absent")).toBeNull();
  });

  it("says why the document has no operation for a linked route (OX-42)", async () => {
    const d = parseOpenApi(structuredClone(synthetic));
    const unresolved = d.unresolved[0];
    if (unresolved === undefined) throw new Error("golden has no unresolved route");
    window.history.replaceState(null, "", `#/explorer?route=${encodeURIComponent(unresolved.route)}`);
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    expect((await screen.findByTestId("route-absent")).textContent).toContain("its path has an unknown part");
    expect(screen.queryByTestId("operation-detail")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByTestId("route-absent")).toBeNull();
    expect(window.location.hash).toBe("#/explorer");
  });
  it("opens the operation a collapsed equivalent registration is written in, and says so (OX-42)", async () => {
    const d = parseOpenApi(structuredClone(synthetic));
    const collapsed = d.operations.find((o) => o.equivalentRegistrations.length > 1);
    const second = collapsed?.equivalentRegistrations.find((r) => r !== collapsed.route);
    if (collapsed === undefined || second === undefined) throw new Error("golden has no equivalent registrations");
    window.history.replaceState(null, "", `#/explorer?route=${encodeURIComponent(second)}`);
    render(<ExplorerSection status={ok()} client={fakeClient()} />);
    const note = await screen.findByTestId("route-equivalent");
    expect(note.textContent).toContain(`Route ${second} is an equivalent registration of ${collapsed.method.toUpperCase()} ${collapsed.key}`);
    // On the operation the link lands on, so it is in view there.
    expect(note.closest("[data-testid=operation]")?.getAttribute("data-route")).toBe(collapsed.route);
    expect(screen.getAllByTestId("operation-detail")).toHaveLength(1);
    expect(window.location.hash).toBe(`#/explorer?op=${encodeURIComponent(`${collapsed.method.toUpperCase()} ${collapsed.key}`)}&route=${encodeURIComponent(second)}`);
  });

  it("requests the chip links of an opened operation's route once and ends each linked chip in its editor button (T029)", async () => {
    const asked: string[] = [];
    const status = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };
    const client: ExplorerClient = {
      ...fakeClient(),
      routeLinks: (route, editor) => {
        asked.push(`${route}|${String(editor)}`);
        // The handler chip's position is the operation's file:line.
        const op = parseOpenApi(structuredClone(synthetic)).operations.find((o) => o.route === route);
        const links = op === undefined ? {} : { [`${op.file}:${String(op.line)}`]: { link: `vscode://file/r/${op.file}:${String(op.line)}:1`, line: op.line, column: 1, exact: true, line_only: false, caveat: null } };
        return Promise.resolve({ status, links });
      },
    };
    render(<ExplorerSection status={ok()} client={client} />);
    expect(asked).toEqual([]);
    const row = await expand("POST /api/items/");
    const route = row.getAttribute("data-route") ?? "";
    const button = await within(row).findAllByTestId("chip-ide-button");
    const op = parseOpenApi(structuredClone(synthetic)).operations.find((o) => o.route === route);
    if (op === undefined) throw new Error("no operation");
    expect(button[0]?.getAttribute("href")).toBe(`vscode://file/r/${op.file}:${String(op.line)}:1`);
    expect(button[0]?.getAttribute("aria-label")).toMatch(new RegExp(`^Open ${op.file.replace(/[.]/g, "\\.")}:${String(op.line)} in VS Code`));
    // Closing and opening again reads the cached links.
    const toggle = row.querySelector("button");
    if (toggle === null) throw new Error("no button");
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    await within(row).findAllByTestId("chip-ide-button");
    expect(asked).toEqual([`${route}|null`]);
  });

  it("lists the linked calls whose request differs in the operation's links area, and nothing for agreeing calls (T029 W4)", async () => {
    const status = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };
    const side = (file: string, line: number, kind: string) => ({ file, line, rule: "SD-2", kind });
    const client: ExplorerClient = {
      ...fakeClient(),
      routeLinks: () =>
        Promise.resolve({
          status,
          links: {},
          callers: [
            {
              name: "web: addItem.POST '/api/items/'",
              file: "src/api.ts",
              line: 4,
              diffs: {
                crossRepository: true,
                compared: ["body"],
                differences: [{ row: "SD-8", section: "body", field: "color", direction: "sent_not_read" as const, text: "sent by the frontend, not read by the handler", call: side("src/api.ts", 7, "literal"), route: side("app/main.py", 7, "fact") }],
                callLinks: {},
                routeLinks: {},
              },
            },
          ],
        }),
    };
    render(<ExplorerSection status={ok()} client={client} />);
    const row = await expand("POST /api/items/");
    const area = await within(row).findByTestId("linked-call-differences");
    expect(area.textContent).toContain("web: addItem.POST '/api/items/'");
    expect(area.textContent).toContain("sent by the frontend, not read by the handler");
    expect(within(area).getByTestId("cross-repository").textContent).toBe("cross-repository link");
  });

  it("draws no links area when no linked call has a difference to show (T029 W4)", async () => {
    const status = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };
    let answered = false;
    const quiet: ExplorerClient = {
      ...fakeClient(),
      routeLinks: () => {
        answered = true;
        return Promise.resolve({ status, links: {}, callers: [] });
      },
    };
    render(<ExplorerSection status={ok()} client={quiet} />);
    const row = await expand("POST /api/items/");
    await waitFor(() => {
      expect(answered).toBe(true);
    });
    expect(within(row).queryByTestId("linked-call-differences")).toBeNull();
  });
});
