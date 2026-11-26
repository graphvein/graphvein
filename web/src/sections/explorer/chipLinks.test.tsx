// The editor buttons of provenance chips (T029): the server's link, the
// same as the IDE button's for the same position, hidden without a link
// or with editor links off, named with the full action, reachable by the
// keyboard.
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import golden from "../../../../internal/server/testdata/route-shape.golden.json";

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import { parseRouteShape, type ShapeProvenance } from "@/api/routeShape";
import { EditorProvider } from "@/hooks/useEditor";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { RouteShapeBlock } from "@/sections/api/RouteShapeBlock";
import { IdeButton } from "@/sections/api/parts";
import { chipAction, ChipLinksProvider, routeLinksCache, type ChipLinks } from "@/sections/explorer/chipLinks";
import { ProvenanceChip } from "@/sections/explorer/chips";
import { ideFetcher } from "@/test/ideFixtures";

afterEach(() => {
  window.localStorage.clear();
});

const FILE = "backend/app/api/routes/items.py";
const p: ShapeProvenance = { kind: "fact", rule: "SH-F1", file: FILE, line: 75, chain: [], confidence: 1 };
const link: IdeLink = { link: `vscode://file/r/${FILE}:75:1`, line: 75, column: 1, exact: true, line_only: false, caveat: null };
const status: IdeStatus = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };
const links: ChipLinks = { status, links: { [`${FILE}:75`]: link } };

describe("ChipIdeButton", () => {
  it("opens the same link as the IDE button for the same file:line, with the editor's mark", () => {
    render(
      <>
        <ChipLinksProvider value={links}>
          <ProvenanceChip p={p} />
        </ChipLinksProvider>
        <IdeButton ide={link} status={status} label place="test" />
      </>,
    );
    const button = screen.getByTestId("chip-ide-button");
    expect(button.getAttribute("href")).toBe(screen.getByTestId("ide-button").getAttribute("href"));
    expect(button.getAttribute("href")).toBe(link.link);
    expect(within(button).getByTestId("editor-mark").getAttribute("data-mark")).toBe("vscode");
    // The existing labelled button is unchanged: it still names the editor.
    expect(screen.getByTestId("ide-button").textContent).toBe("Open in VS Code");
  });

  it("names the full action in its accessible name and tooltip, and is reachable by the keyboard", () => {
    render(
      <ChipLinksProvider value={links}>
        <ProvenanceChip p={p} />
      </ChipLinksProvider>,
    );
    const button = screen.getByRole("link", { name: /^Open backend\/app\/api\/routes\/items\.py:75 in VS Code/ });
    expect(button.getAttribute("aria-label")).toBe("Open backend/app/api/routes/items.py:75 in VS Code, Graphvein's default (graphvein.yaml sets no editor)");
    // The shared first line (editorTip), then the full action.
    expect(button.getAttribute("title")).toBe(`Open in VS Code · change in Settings\n${button.getAttribute("aria-label") ?? ""}`);
    expect(button.tabIndex).toBe(0);
    button.focus();
    expect(document.activeElement).toBe(button);
    // Muted until hover or focus.
    expect(button.className).toContain("opacity-55");
    expect(button.className).toContain("focus-visible:opacity-100");
    expect(button.className).toContain("hover:opacity-100");
  });

  it("is hidden without links, for a position without a link, and when links are off", async () => {
    const { unmount } = render(<ProvenanceChip p={p} />);
    expect(screen.queryByTestId("chip-ide-button")).toBeNull();
    unmount();
    const other = render(
      <ChipLinksProvider value={{ status, links: { [`${FILE}:76`]: link } }}>
        <ProvenanceChip p={p} />
      </ChipLinksProvider>,
    );
    expect(screen.queryByTestId("chip-ide-button")).toBeNull();
    other.unmount();
    window.localStorage.setItem(EDITOR_KEY, "off");
    render(
      <EditorProvider fetcher={ideFetcher()}>
        <ChipLinksProvider value={links}>
          <ProvenanceChip p={p} />
        </ChipLinksProvider>
        <IdeButton ide={link} status={status} label place="test" />
      </EditorProvider>,
    );
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Editor links are off/ })).toBeTruthy();
    });
    expect(screen.queryByTestId("chip-ide-button")).toBeNull();
  });

  it("words the generic editor", () => {
    expect(chipAction("a.py", 3, "Open in editor")).toBe("Open a.py:3 in the editor");
    expect(chipAction("a.py", 3, "Open in Zed")).toBe("Open a.py:3 in Zed");
  });

  it("gives the route detail block's chips their link from shape_links", () => {
    const shape = parseRouteShape(structuredClone(golden), "shape");
    const app: IdeLink = { ...link, link: "vscode://file/r/app.py:3:1", line: 3 };
    render(<RouteShapeBlock shape={shape} shapeError={null} facts={{ file: "app.py", line: 2, framework: "fastapi", attributes: {} }} links={{ status, links: { "app.py:3": app } }} />);
    const path = screen.getByTestId("section-path");
    const buttons = within(path).getAllByTestId("chip-ide-button");
    expect(buttons.length).toBeGreaterThan(0);
    for (const b of buttons) expect(b.getAttribute("href")).toBe(app.link);
    // deps.py:7 has no link in the map: its chip shows none.
    const header = screen.getByTestId("section-header");
    const chips = within(header).getAllByTestId("provenance-chip");
    for (const c of chips) if (c.textContent.includes("deps.py:7")) expect(within(c).queryByTestId("chip-ide-button")).toBeNull();
  });
});

describe("routeLinksCache", () => {
  it("asks once per route and editor, and again after a failure", async () => {
    const calls: string[] = [];
    let fail = true;
    const cache = routeLinksCache((route, editor) => {
      calls.push(`${route}|${String(editor)}`);
      if (fail) {
        fail = false;
        return Promise.reject(new Error("HTTP 500"));
      }
      return Promise.resolve(links);
    }, "run-1");
    const signal = new AbortController().signal;
    await expect(cache("r1", null, signal)).rejects.toThrow("HTTP 500");
    await expect(cache("r1", null, signal)).resolves.toBe(links);
    await expect(cache("r1", null, signal)).resolves.toBe(links);
    await cache("r1", "zed", signal);
    expect(calls).toEqual(["r1|null", "r1|null", "r1|zed"]);
  });
});
