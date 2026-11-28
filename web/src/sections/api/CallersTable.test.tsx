import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { EndpointDetail } from "@/api/endpoints";
import { LabelProvider } from "@/hooks/useLabels";
import { LABELS_KEY } from "@/lib/labelChoice";
import { edgeLabel } from "@/lib/labels";
import { CallersTable, STACK_BELOW_PX } from "@/sections/api/CallersTable";
import { ideLabel } from "@/sections/api/parts";
import { detailGolden } from "@/test/apiFixtures";

const edgeWords = (e: Parameters<typeof edgeLabel>[1]) => edgeLabel("plain", e).text;
const edgeTitle = (e: Parameters<typeof edgeLabel>[1]) => edgeLabel("plain", e).title;

function table(d: EndpointDetail) {
  return render(
    <CallersTable
      transport={d.transport}
      callers={d.callers}
      nearMisses={d.near_misses}
      nearMissesIncomplete={d.near_misses_incomplete}
      ide={d.ide}
    />,
  );
}

function rowOrder(container: HTMLElement): string[] {
  return [...container.querySelectorAll("tbody tr")].map((tr) =>
    tr.hasAttribute("data-caller") ? `caller:${tr.querySelector("[data-category]")?.getAttribute("data-category") ?? ""}` : tr.hasAttribute("data-near-miss") ? "near" : "sep",
  );
}

describe("CallersTable", () => {
  it("lists linked callers first, then the F5 group with its reasons", () => {
    const d = detailGolden("api-endpoint-route");
    const { container } = table(d);
    expect(rowOrder(container)).toEqual(["caller:linked", "caller:linked", "caller:linked", "caller:linked", "sep", "caller:dynamic"]);
    expect(screen.getByText(/Linked alternative of a dynamic call/)).toBeTruthy();
    const f5 = container.querySelector('tr[data-caller*="partlyDynamic"]');
    expect(f5?.querySelector('[data-reason="alternative_linked"]')).not.toBeNull();
    expect(screen.getByRole("heading", { name: /4 linked · 1 not fully linked/ })).toBeTruthy();
  });

  it("never renders a dynamic or unmatched call without a stored edge", () => {
    const d = detailGolden("api-endpoint-route");
    // A dynamic call smuggled into near misses is dropped.
    const smuggled = structuredClone(d.callers[4]);
    if (smuggled === undefined) throw new Error("fixture changed");
    smuggled.node.identity = "client_call:x.ts#smuggled";
    d.near_misses = [{ node: smuggled.node, owner: smuggled.owner, relation: "ambiguous_candidate", candidate_count: 2, candidate_count_exact: true, reasons: [] }];
    const { container } = table(d);
    expect(container.querySelector('[data-near-miss="client_call:x.ts#smuggled"]')).toBeNull();
    // Every rendered caller comes from the callers list (stored edges).
    const ids = new Set(d.callers.map((c) => c.node.identity));
    for (const tr of container.querySelectorAll("tr[data-caller]")) expect(ids.has(tr.getAttribute("data-caller") ?? "")).toBe(true);
  });

  it("separates near misses; without callers the heading says so", () => {
    const d = detailGolden("api-endpoint-route-ambiguous");
    d.near_misses.forEach((n) => (n.candidate_count_exact = true));
    const { container } = table(d);
    expect(rowOrder(container)).toEqual(["sep", "near"]);
    expect(screen.getByText(/Near misses · not linked, not counted above/)).toBeTruthy();
    expect(screen.getByText("1 of 2 tied candidates")).toBeTruthy();
    expect(screen.getByRole("heading", { name: /^Near misses/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Jump to/ })).toBeNull();
  });

  it("jumps to near misses inside the scroll container", () => {
    const d = detailGolden("api-endpoint-route");
    d.callers = d.callers.filter((c) => c.node.attributes.status === "linked");
    const amb = detailGolden("api-endpoint-route-ambiguous");
    d.near_misses = amb.near_misses;
    const scrollTo = vi.fn();
    const { container } = render(
      <div data-scroll-container>
        <CallersTable transport="http" callers={d.callers} nearMisses={d.near_misses} nearMissesIncomplete={0} ide={d.ide} />
      </div>,
    );
    const box = container.firstElementChild as HTMLElement;
    box.scrollTo = scrollTo;
    const page = vi.fn();
    Element.prototype.scrollIntoView = page;
    fireEvent.click(screen.getByRole("button", { name: /Jump to near misses/ }));
    expect(scrollTo).toHaveBeenCalledTimes(1);
    expect(page).not.toHaveBeenCalled();
  });

  it("says at least N when the candidate count is a lower bound", () => {
    const d = detailGolden("api-endpoint-route-ambiguous");
    const n = d.near_misses[0];
    if (n === undefined) throw new Error("fixture changed");
    n.candidate_count = 10;
    n.candidate_count_exact = false;
    table(d);
    expect(screen.getByText("1 of at least 10 tied candidates")).toBeTruthy();
  });

  it("jumps to the F5 group first, renders unknown statuses as other, and notes capped candidates", () => {
    const d = detailGolden("api-statuses-endpoint-route");
    const scroll = vi.fn();
    // Outside a scroll container the jump falls back to scrollIntoView.
    Element.prototype.scrollIntoView = scroll;
    const { container } = table(d);
    // LINKED is not linked: it is its own F5 group under "other".
    const linkedRows = [...container.querySelectorAll('tr[data-caller] [data-category="linked"]')];
    expect(linkedRows).toHaveLength(2);
    const other = container.querySelector('[data-category="other"]');
    expect(other?.textContent).toBe("Other: LINKED");
    expect(screen.getByText(/Linked alternative of a call with status LINKED/)).toBeTruthy();
    expect(screen.getByTestId("near-misses-incomplete").textContent).toContain("1 ambiguous or test-only call lists only their first 10 candidates");
    fireEvent.click(screen.getByRole("button", { name: /Jump to not fully linked/ }));
    expect((scroll.mock.contexts[0] as HTMLElement).textContent).toContain("Linked alternative");
    // Missing files are marked per caller.
    expect(within(container).getAllByText("missing").length).toBe(d.callers.length + d.near_misses.length);
  });

  it("labels IDE links by scheme and disables them without a link", () => {
    expect(ideLabel("vscode://file/a:1:1")).toBe("Open in VS Code");
    expect(ideLabel("vscode-insiders://file/a")).toBe("Open in VS Code Insiders");
    expect(ideLabel("cursor://file/a")).toBe("Open in Cursor");
    // Every scheme of T016e D6; anything else is generic.
    expect(ideLabel("vscodium://file/a")).toBe("Open in VSCodium");
    expect(ideLabel("vscodium-insiders://file/a")).toBe("Open in VSCodium Insiders");
    expect(ideLabel("idea://open?file=a")).toBe("Open in IntelliJ IDEA");
    expect(ideLabel("zed://file/a")).toBe("Open in Zed");
    expect(ideLabel("subl://open?url=a")).toBe("Open in editor");
    expect(ideLabel("no-scheme")).toBe("Open in editor");
    expect(ideLabel("constructor://x")).toBe("Open in editor");
    const d = detailGolden("api-endpoint-command");
    const first = d.callers[0];
    if (first === undefined) throw new Error("fixture changed");
    first.node.ide = null;
    d.ide = { available: false, error: "graphvein.yaml: bad" };
    table(d);
    const links = screen.getAllByRole("link", { name: "Open in VS Code" });
    expect(links).toHaveLength(d.callers.length - 1);
    expect(links[0]?.getAttribute("href")).toMatch(/^vscode:\/\/file\//);
    const disabled = screen.getByRole("button", { name: /IDE links are unavailable: graphvein.yaml: bad/ });
    expect((disabled as HTMLButtonElement).disabled).toBe(true);
  });

  it("prefers the server's ide.label over the scheme", () => {
    const d = detailGolden("api-endpoint-command");
    d.ide = { available: true, error: null, label: "Open in My Editor" };
    table(d);
    expect(screen.getAllByRole("link", { name: "Open in My Editor" })).toHaveLength(d.callers.length);
  });

  it("shows the wrapper and conditional badges and the changed chip", () => {
    const d = detailGolden("api-endpoint-command");
    const c = d.callers.find((x) => x.node.attributes.conditional === true);
    if (c === undefined) throw new Error("fixture changed");
    c.node.file_state = "changed";
    const w = d.callers[1];
    if (w === undefined) throw new Error("fixture changed");
    w.node.attributes.wrapper_identity = "function:web/src/ipcwrap.ts#call";
    const { container } = table(d);
    const row = container.querySelector(`tr[data-caller="${c.node.identity}"]`);
    expect(row?.textContent).toContain("conditional");
    expect(row?.textContent).toContain("changed");
    expect(container.querySelector(`tr[data-caller="${w.node.identity}"]`)?.textContent).toContain("via wrapper");
    expect(container.querySelectorAll("tr[data-caller]")[0]?.textContent).not.toContain("via wrapper");
    expect(container.textContent).toContain("test");
  });
});

describe("edge words (T025)", () => {
  it("says the known edge tiers in plain words and keeps the stored fields in the tooltip", () => {
    const e = (provenance: string, confidence: number, match: string | null) => ({ kind: "calls_route", provenance, confidence, match });
    expect(edgeWords(e("inferred", 0.9, "exact"))).toBe("Exact path match · 0.9");
    expect(edgeWords(e("inferred", 0.7, "exact"))).toBe("Exact path match · 0.7");
    expect(edgeWords(e("inferred", 0.6, "suffix"))).toBe("Path suffix match · 0.6");
    expect(edgeWords({ kind: "exposes_route", provenance: "fact", confidence: 1, match: null })).toBe("Fact · 1.0");
    expect(edgeTitle(e("inferred", 0.9, "exact"))).toBe("Stored calls_route edge: inferred 0.9 exact. The number is a fixed rank per match rule, not a probability.");
    // An inferred edge with no match field: the provenance word and its
    // number (T041, row 4).
    expect(edgeWords(e("inferred", 0.9, null))).toBe("Inferred · 0.9");
    // Any other form keeps its stored fields: no word it was not checked for.
    expect(edgeWords(e("inferred", 0.9, "fuzzy"))).toBe("inferred 0.9 fuzzy");
  });
});

describe("technical labels (T025b)", () => {
  it("the Confidence column shows the stored fields, the plain words in the tooltip", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    try {
      const d = detailGolden("api-endpoint-route");
      const { container } = render(
        <LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: null, error: null })}>
          <CallersTable transport={d.transport} callers={d.callers} nearMisses={d.near_misses} nearMissesIncomplete={d.near_misses_incomplete} ide={d.ide} />
        </LabelProvider>,
      );
      const edge = d.callers.flatMap((c) => c.edges)[0];
      if (edge === undefined) throw new Error("no caller edge in the golden");
      const cell = [...container.querySelectorAll("[title^='Stored ']")][0];
      expect(cell?.textContent).toBe(edgeLabel("technical", edge).text);
      expect(cell?.textContent).toMatch(/^inferred 0\.\d+ (exact|suffix)$/);
      expect(cell?.getAttribute("title")).toMatch(/: (Exact path match|Path suffix match)$/);
      await act(async () => {
        await Promise.resolve();
      });
    } finally {
      window.localStorage.removeItem(LABELS_KEY);
    }
  });
});

describe("narrow table (T078)", () => {
  // jsdom applies no container query; the classes say what a browser
  // does below STACK_BELOW_PX of the table's width: the caller cell spans
  // the row, Status and Confidence stack on a second line, the open
  // buttons stay at the right, and the header row is for screen readers.
  it("stacks Status and Confidence under the caller below the container width", () => {
    for (const golden of ["api-endpoint-route", "api-endpoint-route-ambiguous"] as const) {
      const { container, unmount } = table(detailGolden(golden));
      const q = `@max-[${String(STACK_BELOW_PX)}px]/callers:`;
      expect(container.querySelector('[data-testid="callers-table"]')?.className).toContain("@container/callers");
      expect(container.querySelector("table")?.className).toContain(`${q}block`);
      expect(container.querySelector("thead")?.className).toContain(`${q}sr-only`);
      const rows = [...container.querySelectorAll("tr[data-caller], tr[data-near-miss]")];
      expect(rows.length).toBeGreaterThan(0);
      for (const tr of rows) {
        expect(tr.className).toContain(`${q}grid`);
        const [caller, status, confidence, open] = [...tr.children].map((td) => td.className);
        expect(caller).toContain(`${q}col-span-2`);
        expect(status).toContain(`${q}row-start-2`);
        expect(confidence).toContain(`${q}col-start-2`);
        expect(open).toContain(`${q}row-span-2`);
        // One copy of each cell: nothing is rendered twice for the narrow form.
        expect(tr.children.length).toBe(4);
      }
      unmount();
    }
  });
});
