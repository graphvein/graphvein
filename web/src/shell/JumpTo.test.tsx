import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SEARCH_KINDS, type SearchAnswer, type SearchFetcher, type SearchItem, type SearchKind } from "@/api/search";
import { editorNav } from "@/components/EditorLink";
import { EditorProvider } from "@/hooks/useEditor";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { isJumpShortcut, JumpToDialog } from "@/shell/JumpTo";
import { ideFetcher, ideInfo } from "@/test/ideFixtures";
import { WorkspaceProvider, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { fakeWorkspaceClient, rawEntry, workspaceDoc } from "@/test/workspaceFixtures";

function item(kind: SearchKind, label: string, id: string): SearchItem {
  return { kind, repository: "app", id, label, tier: "prefix", match_key: "name", file: "src/a.ts", line: 1, ide: null, method: kind === "route" ? "GET" : null, path: null, prefix_unknown: null, test: false, node_kind: kind === "symbol" ? "function" : null, qualified_name: null, source: null, language: null };
}

function answer(q: string, groups: [SearchKind, SearchItem[]][]): SearchAnswer {
  const totals: Partial<Record<SearchKind, number>> = {};
  for (const k of SEARCH_KINDS) totals[k] = 0;
  for (const [k, items] of groups) totals[k] = items.length;
  return {
    project: null,
    query: q,
    kinds: [...SEARCH_KINDS],
    members: [{ repository: "app", checkout: true, ide: { available: true, error: null }, searched: { indexed: true, files: 12, files_not_read: 3, scanner_gaps: 0 }, totals }],
    totals,
    groups: groups.map(([kind, items]) => ({ kind, available: true, unavailable: [], total: items.length, best_tier: "prefix", truncated: false, items })),
  };
}

afterEach(() => {
  window.location.hash = "";
  window.localStorage.clear();
});

describe("JumpToDialog", () => {
  it("knows Ctrl+K and Cmd+K only", () => {
    const base = { ctrlKey: false, metaKey: false, altKey: false, shiftKey: false };
    expect(isJumpShortcut({ ...base, key: "k", ctrlKey: true })).toBe(true);
    expect(isJumpShortcut({ ...base, key: "K", metaKey: true })).toBe(true);
    expect(isJumpShortcut({ ...base, key: "k" })).toBe(false);
    expect(isJumpShortcut({ ...base, key: "k", ctrlKey: true, shiftKey: true })).toBe(false);
  });

  it("groups results by kind and moves through them with the keyboard", async () => {
    const fetcher = vi.fn<SearchFetcher>((p) => Promise.resolve(answer(p.q, [["route", [item("route", "/items", "route:a#GET%20%2Fitems"), item("route", "/items/{id}", "route:a#GET%20%2Fitems%2F%7Bid%7D")]], ["symbol", [item("symbol", "listItems", "function:a#listItems")]]])));
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={fetcher} />);
    const input = screen.getByTestId("jump-input");
    expect(screen.getByTestId("jump-empty")).toBeTruthy();
    fireEvent.change(input, { target: { value: "items" } });
    await waitFor(() => {
      expect(screen.getAllByTestId("jump-row")).toHaveLength(3);
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    const groups = screen.getByTestId("jump-list").querySelectorAll('[role="group"]');
    expect([...groups].map((g) => g.getAttribute("aria-label"))).toEqual(["Routes", "Symbols"]);
    // The chips keep the fixed kind order with their totals.
    expect(screen.getAllByTestId("jump-chip").map((c) => c.textContent)).toEqual(["All3", "Routes2", "Explorer operations0", "Tables0", "Symbols1", "Files0"]);
    const rows = () => screen.getAllByTestId("jump-row");
    expect(rows()[0]?.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(rows()[1]?.getAttribute("aria-selected")).toBe("true");
    expect(input.getAttribute("aria-activedescendant")).toBe(rows()[1]?.id);
    fireEvent.keyDown(input, { key: "Tab" });
    expect(rows()[2]?.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(rows()[2]?.getAttribute("aria-selected")).toBe("true");
    // A symbol without an editor link says so instead of opening.
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByTestId("jump-status").textContent).toMatch(/^No editor link/);
    fireEvent.keyDown(input, { key: "Tab", shiftKey: true });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(window.location.hash).toBe("#/api?sel=route%3Aa%23GET%2520%252Fitems");
  });

  it("says a search without a match is not proof of absence", async () => {
    const fetcher: SearchFetcher = (p) => Promise.resolve(answer(p.q, []));
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={fetcher} />);
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "billing" } });
    const none = await screen.findByTestId("jump-no-result");
    expect(none.textContent).toContain("No stored name or path matches “billing”.");
    expect(none.textContent).toContain("Not proof that it does not exist");
    expect(none.textContent).toContain("3 files Graphvein could not read");
  });

  it("lists the other workspace entries as Switch to <name> and switches without a reload", async () => {
    const ws = workspaceDoc([rawEntry({ id: "api~1", name: "api", state: "open" }), rawEntry({ id: "shop~2", name: "shop", kind: "project" })], "api~1");
    const switchTo = vi.fn();
    const state: WorkspaceState = {
      workspace: ws,
      entry: ws.entries[0] ?? null,
      unknownLink: null,
      dismissUnknown: vi.fn(),
      switchTo,
      refresh: vi.fn(),
      watch: vi.fn(),
      client: fakeWorkspaceClient(ws),
      addRequest: null,
      openAdd: vi.fn(),
      closeAdd: vi.fn(),
    };
    const before = window.location.href;
    render(
      <WorkspaceProvider value={state}>
        <JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={(p) => Promise.resolve(answer(p.q, []))} />
      </WorkspaceProvider>,
    );
    const row = await screen.findByTestId("jump-row");
    expect(row.textContent).toContain("Switch to shop");
    expect(within(screen.getByTestId("jump-list")).queryByText(/Switch to api/)).toBeNull();
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter" });
    expect(switchTo).toHaveBeenCalledWith("shop~2");
    // No page load: the address did not change.
    expect(window.location.href).toBe(before);
  });

  it("lists the jumps opened in this browser when the query is empty", async () => {
    const fetcher: SearchFetcher = (p) => Promise.resolve(answer(p.q, [["route", [item("route", "/items", "route:a#GET%20%2Fitems")]]]));
    let open = true;
    const onOpenChange = (o: boolean) => {
      open = o;
    };
    const { rerender } = render(<JumpToDialog served="app" open={open} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    // Nothing opened yet: no Recent group, the hint only.
    expect(within(screen.getByTestId("jump-list")).queryByRole("group", { name: "Recent" })).toBeNull();
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "items" } });
    await screen.findByTestId("jump-row");
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter" });
    expect(open).toBe(false);
    rerender(<JumpToDialog served="app" open={open} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    rerender(<JumpToDialog served="app" open onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    // Opening again starts empty, with the jump just made.
    expect(screen.getByTestId<HTMLInputElement>("jump-input").value).toBe("");
    const recent = await screen.findByRole("group", { name: "Recent" });
    expect(recent.textContent).toContain("in this browser");
    expect(within(recent).getAllByTestId("jump-row").map((r) => r.textContent)).toEqual([expect.stringContaining("/items")]);
    expect(screen.getByTestId("jump-empty")).toBeTruthy();
  });

  it("works without storage", () => {
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={(p) => Promise.resolve(answer(p.q, []))} />);
    expect(screen.getByTestId("jump-empty")).toBeTruthy();
    get.mockRestore();
  });

  it("debounces typing into one request", async () => {
    const fetcher = vi.fn<SearchFetcher>((p) => Promise.resolve(answer(p.q, [])));
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={fetcher} />);
    const input = screen.getByTestId("jump-input");
    act(() => {
      fireEvent.change(input, { target: { value: "b" } });
      fireEvent.change(input, { target: { value: "bi" } });
      fireEvent.change(input, { target: { value: "bil" } });
    });
    await screen.findByTestId("jump-no-result");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0].q).toBe("bil");
  });

  // B3: one server address serves repoA, then repoB. repoA's jumps are not
  // listed for repoB, and an item of another repository stored under
  // repoB's key is dropped.
  it("keeps recent jumps per served index on the same origin", async () => {
    const a: SearchItem = { ...item("route", "/a-only", "route:a#GET%20%2Fa"), repository: "repoA" };
    const fetcher: SearchFetcher = (p) => Promise.resolve(answer(p.q, [["route", [a]]]));
    let open = true;
    const onOpenChange = (o: boolean) => {
      open = o;
    };
    const { rerender, unmount } = render(<JumpToDialog served="repoA" open={open} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "a-only" } });
    await screen.findByTestId("jump-row");
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter" });
    expect(open).toBe(false);
    rerender(<JumpToDialog served="repoA" open={false} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    rerender(<JumpToDialog served="repoA" open onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    expect(await screen.findByRole("group", { name: "Recent" })).toBeTruthy();
    unmount();
    // The same origin now serves repoB.
    render(<JumpToDialog served="repoB" open onOpenChange={() => undefined} searchFetcher={fetcher} />);
    expect(within(screen.getByTestId("jump-list")).queryByRole("group", { name: "Recent" })).toBeNull();
    expect(within(screen.getByTestId("jump-list")).queryByText(/a-only/)).toBeNull();
  });

  it("drops a stored recent item of another repository", () => {
    const stray: SearchItem = { ...item("route", "/stray", "route:x#GET%20%2Fstray"), repository: "repoA" };
    window.localStorage.setItem("graphvein.jump.recent.v1:repository%3ArepoB", JSON.stringify([stray]));
    window.localStorage.setItem("graphvein.jump.recent.v1", JSON.stringify([stray]));
    render(<JumpToDialog served="repoB" open onOpenChange={() => undefined} searchFetcher={(p) => Promise.resolve(answer(p.q, []))} />);
    expect(within(screen.getByTestId("jump-list")).queryByText(/stray/)).toBeNull();
  });

  // JX-F2: Ctrl+Enter on a route without an editor link says so.
  it("says why Ctrl+Enter does not open a route without an editor link", async () => {
    const r = item("route", "/items", "route:a#GET%20%2Fitems");
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={(p) => Promise.resolve(answer(p.q, [["route", [r]]]))} />);
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "items" } });
    await screen.findByTestId("jump-row");
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter", ctrlKey: true });
    expect(screen.getByTestId("jump-status").textContent).toBe("No editor link for this route: Enter opens it in the API section");
    expect(window.location.hash).toBe("");
  });

  // JX-F2: a route without a full path (dozzle's "/rules" below
  // r.Route(base, ...)) stays "…/api/notifications/rules" in recent jumps.
  it("keeps a route's unknown-prefix path in recent jumps", async () => {
    const r: SearchItem = { ...item("route", "…/api/notifications/rules", "route:internal/web/routes.go#GET%20%2Frules"), prefix_unknown: "…/api/notifications/rules", match_key: "prefix_unknown", tier: "word" };
    const fetcher: SearchFetcher = (p) => Promise.resolve(answer(p.q, [["route", [r]]]));
    let open = true;
    const onOpenChange = (o: boolean) => {
      open = o;
    };
    const { rerender } = render(<JumpToDialog served="app" open={open} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "notifications/rules" } });
    await screen.findByTestId("jump-row");
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter" });
    rerender(<JumpToDialog served="app" open={open} onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    rerender(<JumpToDialog served="app" open onOpenChange={onOpenChange} searchFetcher={fetcher} />);
    const recent = await screen.findByRole("group", { name: "Recent" });
    const text = within(recent).getAllByTestId("jump-row")[0]?.textContent ?? "";
    expect(text).toContain("…/api/notifications/rules");
    expect(text).not.toMatch(/GET\s*\/rules/);
  });

  // B4: Ctrl+Enter on an operation says it has no source line.
  it("says why Ctrl+Enter does not open an operation in the editor", async () => {
    const op = item("operation", "/items", "route:a#GET%20%2Fitems");
    render(<JumpToDialog served="app" open onOpenChange={() => undefined} searchFetcher={(p) => Promise.resolve(answer(p.q, [["operation", [op]]]))} />);
    fireEvent.change(screen.getByTestId("jump-input"), { target: { value: "items" } });
    await screen.findByTestId("jump-row");
    fireEvent.keyDown(screen.getByTestId("jump-input"), { key: "Enter", ctrlKey: true });
    expect(screen.getByTestId("jump-status").textContent).toBe("No source line for an Explorer operation: Enter opens it in the Explorer");
    expect(window.location.hash).toBe("");
  });
});

// Jump-to opens editor links through the app's one first-click chooser
// (developer decision 2026-11-25): no bypass while nothing chose an editor.
describe("Jump-to and the editor chooser", () => {
  const VS = "vscode://file/r/src/a.ts:1:1";
  const ZED = "zed://file/r/src/a.ts:1:1";
  function sym(link: string): SearchItem {
    return { ...item("symbol", "listItems", "function:a#listItems"), ide: { link, line: 1, column: 1, exact: true, line_only: false, caveat: null } };
  }
  const fetcher = () => vi.fn<SearchFetcher>((p) => Promise.resolve(answer(p.q, [["symbol", [sym(p.editor === "zed" ? ZED : VS)]]])));
  let opened: string[] = [];
  beforeEach(() => {
    opened = [];
    vi.spyOn(editorNav, "open").mockImplementation((href: string) => {
      opened.push(href);
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function openSymbol(f: ReturnType<typeof fetcher>, onOpenChange: (o: boolean) => void = () => undefined) {
    render(
      <EditorProvider fetcher={ideFetcher(ideInfo())}>
        <JumpToDialog served="app" open onOpenChange={onOpenChange} searchFetcher={f} />
      </EditorProvider>,
    );
    const input = screen.getByTestId("jump-input");
    fireEvent.change(input, { target: { value: "listItems" } });
    await waitFor(() => {
      expect(screen.getAllByTestId("jump-row")).toHaveLength(1);
    });
    fireEvent.keyDown(input, { key: "Enter" });
    return input;
  }

  it("asks with the shared chooser instead of opening, then opens the link built for the pick", async () => {
    const f = fetcher();
    const onOpenChange = vi.fn();
    await openSymbol(f, onOpenChange);
    const chooser = await screen.findByTestId("editor-chooser");
    expect(opened).toEqual([]);
    expect(within(chooser).getByTestId("editor-chooser-list")).toBeTruthy();
    fireEvent.click(within(chooser).getByTestId("editor-chooser-zed"));
    await waitFor(() => {
      expect(opened).toEqual([ZED]);
    });
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("zed");
    expect(f.mock.calls.some(([p]) => p.editor === "zed" && p.q === "listItems")).toBe(true);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("opens the row's link at once when the pick is the editor the links already use", async () => {
    await openSymbol(fetcher());
    fireEvent.click(within(await screen.findByTestId("editor-chooser")).getByTestId("editor-chooser-vscode"));
    expect(opened).toEqual([VS]);
  });

  it("closes the chooser on Esc without opening anything or closing the palette", async () => {
    const onOpenChange = vi.fn();
    const input = await openSymbol(fetcher(), onOpenChange);
    const chooser = await screen.findByTestId("editor-chooser");
    fireEvent.keyDown(chooser, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("editor-chooser")).toBeNull();
    });
    expect(opened).toEqual([]);
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    expect(screen.getByTestId("jump-to")).toBeTruthy();
    await waitFor(() => {
      expect(document.activeElement).toBe(input);
    });
  });

  it("opens directly once this browser chose an editor", async () => {
    window.localStorage.setItem(EDITOR_KEY, "vscode");
    await openSymbol(fetcher());
    await waitFor(() => {
      expect(opened).toEqual([VS]);
    });
    expect(screen.queryByTestId("editor-chooser")).toBeNull();
  });
});
