import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";

import { LabelProvider } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { ElkClient, type WorkerLike } from "@/sections/api/flow/elk";
import { ArchitectureSection } from "@/sections/architecture/ArchitectureSection";
import { parseArchParams } from "@/sections/architecture/params";
import { fakeArchClient } from "@/test/architectureFixtures";

const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };
const elk = new ElkClient(() => {
  const w = new workerModule.default.Worker() as WorkerLike;
  w.terminate = () => undefined;
  return w;
});

beforeAll(() => {
  // jsdom does no layout. React Flow measures nodes (offsetWidth/Height,
  // through a ResizeObserver) before it draws lines, and reads the
  // viewport transform through DOMMatrixReadOnly; the same stand-ins as
  // the Flow tab's test, for this file only.
  class Observer {
    private readonly cb: ResizeObserverCallback;
    constructor(cb: ResizeObserverCallback) {
      this.cb = cb;
    }
    observe(target: Element): void {
      const entry = { target, contentRect: { width: 1200, height: 700 } } as unknown as ResizeObserverEntry;
      this.cb([entry], this);
    }
    unobserve(): void {
      // Nothing is held.
    }
    disconnect(): void {
      // Nothing is held.
    }
  }
  Object.defineProperty(globalThis, "ResizeObserver", { value: Observer, configurable: true });
  Object.defineProperties(HTMLElement.prototype, {
    offsetHeight: {
      configurable: true,
      get(this: HTMLElement) {
        return Number.parseFloat(this.style.height) || 1;
      },
    },
    offsetWidth: {
      configurable: true,
      get(this: HTMLElement) {
        return Number.parseFloat(this.style.width) || 1;
      },
    },
  });
  if (!("DOMMatrixReadOnly" in window)) {
    class Matrix {
      m22 = 1;
    }
    Object.defineProperty(window, "DOMMatrixReadOnly", { value: Matrix, configurable: true });
  }
});

const SLOW = { timeout: 8000 };

const LOADING: StatusView = { kind: "loading" };

function setHash(h: string) {
  window.history.replaceState(null, "", h);
}

function renderSection(name: "components" | "synthetic", style?: "technical") {
  const requests: string[] = [];
  const ui = <ArchitectureSection status={LOADING} client={fakeArchClient(name, requests, name === "components" ? "components-all" : name)} elk={elk} />;
  render(style === undefined ? ui : <LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: style, error: null })}>{ui}</LabelProvider>);
  return requests;
}

describe("ArchitectureSection", { timeout: 20_000 }, () => {
  beforeEach(() => {
    try {
      localStorage.clear();
    } catch {
      // storage blocked: the label default applies
    }
    setHash("#/architecture");
  });

  it("draws the map with the Hidden note and the level tag", async () => {
    const requests = renderSection("components");
    await screen.findByTestId("arch-map", {}, SLOW);
    await waitFor(() => {
      expect(document.querySelector('[data-layout="ok"]')).not.toBeNull();
    }, SLOW);
    // The layout can finish before the map draws its cards; under load
    // the cards come a render later, so wait for one instead of assuming
    // it is there.
    await waitFor(() => {
      expect(document.querySelector('[data-arch-card="c:web"]')).not.toBeNull();
    }, SLOW);
    expect(requests).toContain("overview production");
    expect(screen.getByTestId("hidden-note").textContent).toBe("Hidden: tests 1 component · docs 1 component · libraries 4 · configured 1");
    expect(screen.getByTestId("arch-crumbs").textContent).toContain("L1Architecture");
    // The minimap shows above 12 cards (C1); AG-RF's unassigned root card
    // (CK7, no longer folded) is one of them.
    await waitFor(() => {
      const shown = Number(screen.getByTestId("arch-map").getAttribute("data-cards"));
      expect(document.querySelector(".react-flow__minimap") !== null).toBe(shown > 12);
    }, SLOW);
    // Libraries are folded and hidden by default.
    expect(document.querySelector('[data-arch-card="lib"]')).toBeNull();
  });

  it("shows tests with the all scope and the libraries card from the toggles", async () => {
    const requests = renderSection("components");
    await screen.findByTestId("arch-map", {}, SLOW);
    fireEvent.click(screen.getByRole("button", { name: /^Tests/ }));
    fireEvent.click(screen.getByRole("button", { name: /^Libraries/ }));
    expect(parseArchParams(window.location.hash).show).toMatchObject({ tests: true, libraries: true });
    await waitFor(() => {
      expect(requests).toContain("overview all");
    }, SLOW);
    await waitFor(() => {
      expect(document.querySelector('[data-arch-card="lib"]')).not.toBeNull();
    }, SLOW);
    expect(document.querySelector('[data-arch-card="c:e2e"]')).not.toBeNull();
    // Above 12 cards the minimap shows (C1); the default map has fewer.
    const cards = Number(screen.getByTestId("arch-map").getAttribute("data-cards"));
    expect(cards).toBeGreaterThan(12);
    expect(document.querySelector(".react-flow__minimap")).not.toBeNull();
  });

  it("selects a line: strip, each cell's source and the evidence", async () => {
    renderSection("synthetic");
    await waitFor(() => {
      expect(document.querySelector("[data-arch-chip]")).not.toBeNull();
    }, SLOW);
    const chip = document.querySelector<HTMLButtonElement>('[data-arch-chip="e:c:app|c:ui"]');
    expect(chip).not.toBeNull();
    act(() => {
      chip?.click();
    });
    const insp = await screen.findByTestId("inspector", {}, SLOW);
    expect(insp.getAttribute("data-kind")).toBe("link");
    expect(within(insp).getByTestId("strip").textContent).toBe("Tauri IPCauth ?n/a, in-app");
    expect(within(insp).getByTestId("cell-Auth").textContent).toContain("unknown:");
    // Focus dimming: cards off the selected line are dimmed.
    expect(document.querySelector('[data-arch-card="c:svc"]')?.getAttribute("data-dim")).toBe("true");
    expect(document.querySelector('[data-arch-card="c:ui"]')?.getAttribute("data-dim")).toBeNull();
  });

  it("shows deploy markers as text on a card and never as a line", async () => {
    renderSection("synthetic");
    await waitFor(() => {
      expect(document.querySelector('[data-arch-card="c:svc"]')).not.toBeNull();
    }, SLOW);
    expect(document.querySelector('[data-arch-card="c:svc"] [data-testid="card-markers"]')?.textContent).toContain("Dockerfile svc/Dockerfile");
    const lines = [...document.querySelectorAll("[data-arch-line]")].map((l) => l.getAttribute("data-type"));
    expect(lines.filter((t) => t === "link")).toHaveLength(5);
  });

  it("expands a component in place with its rows and selects a row", async () => {
    setHash("#/architecture?show=libraries");
    renderSection("components");
    await waitFor(() => {
      expect(document.querySelector('[data-arch-card="c:crates/app"]')).not.toBeNull();
    }, SLOW);
    fireEvent.click(screen.getByRole("button", { name: /^Expand t026-app/ }));
    expect(parseArchParams(window.location.hash).expanded).toBe("crates/app");
    const row = await waitFor(() => {
      const r = document.querySelector<HTMLButtonElement>('[data-row="node"]');
      if (r === null) throw new Error("no row yet");
      return r;
    }, SLOW);
    act(() => {
      row.click();
    });
    expect(parseArchParams(window.location.hash).item).toBe(row.getAttribute("data-identity"));
    expect((await screen.findByTestId("inspector", {}, SLOW)).getAttribute("data-kind")).toBe("item");
    expect(screen.getByTestId("arch-crumbs").textContent).toMatch(/L2.*L3.*L4/);
  });

  it("lists every link in the Links tab with the Not proven list", async () => {
    setHash("#/architecture?tab=links");
    renderSection("synthetic");
    const tab = await screen.findByTestId("links-tab", {}, SLOW);
    // Six links, and the server action link of TOP-2 of 2026-11-18 (CE7).
    expect(within(tab).getAllByTestId("link-row")).toHaveLength(7);
    // 12 entries: the 7 compose services and the unmapped ingress fold into
    // one collapsed group; db and cache, whose images AE2 ties to database
    // cards, are noted, not listed (AG-C2 fix (a)).
    const list = within(tab).getByTestId("not-proven");
    expect(list.children.length).toBe(7);
    const group = within(list).getByTestId("deploy-group");
    expect(group.textContent).toContain("Deploy services not tied to a component (7)");
    expect(within(group).queryByTestId("deploy-group-entries")).toBeNull();
    fireEvent.click(within(group).getByRole("button"));
    expect(within(group).getByTestId("deploy-group-entries").children).toHaveLength(7);
    expect(within(group).getByTestId("deploy-tied").textContent).toMatch(/cache \(deploy\/compose\.yml\).*db \(deploy\/compose\.yml\)/);
    // fix (c): the database card's name, not its raw identity.
    expect(list.textContent).not.toContain("database:");
    expect(list.textContent).toContain("svc may also use sql schema (db): 1 code site not bound to a table");
    fireEvent.click(within(tab).getByRole("button", { name: "Has unknown" }));
    expect(within(tab).getAllByTestId("link-row").length).toBeGreaterThan(0);
  });

  it("draws a row's chain in place, numbered, with the evidence chain on top of the inspector", async () => {
    setHash(`#/architecture?chain=${encodeURIComponent("web")}&node=${encodeURIComponent("c:web")}`);
    const requests = renderSection("components");
    const bar = await screen.findByTestId("chain-bar", {}, SLOW);
    await waitFor(() => {
      expect(bar.textContent).toContain("Chain of web · 2 steps");
    }, SLOW);
    expect(requests).toContain("chain web both");
    const panel = screen.getByTestId("chain-panel");
    const steps = within(panel).getByTestId("chain-steps");
    expect([...steps.querySelectorAll("[data-step]")].map((e) => e.getAttribute("data-step"))).toEqual(["1", "2"]);
    // Selecting a member here selects it on the map too (the hash).
    act(() => {
      within(steps).getAllByRole("button", { pressed: false })[1]?.click();
    });
    expect(parseArchParams(window.location.hash).at).not.toBeNull();
    // Esc clears the chain.
    fireEvent.keyDown(window, { key: "Escape" });
    expect(parseArchParams(window.location.hash).chain).toBeNull();
  });

  it("the chain's depth switch (T042): repository default, a remembered choice, the link's depth", async () => {
    setHash(`#/architecture?chain=${encodeURIComponent("web")}&node=${encodeURIComponent("c:web")}`);
    const requests: string[] = [];
    const ui = <ArchitectureSection status={LOADING} client={fakeArchClient("components", requests, "components-all")} elk={elk} />;
    const first = render(<LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: null, impact_depth: null, chain_depth: 8, error: null })}>{ui}</LabelProvider>);
    const sw = await screen.findByTestId("chain-depth", {}, SLOW);
    const group = within(sw).getByRole("radiogroup", { name: "Depth" });
    expect(within(group).getAllByRole("radio").length).toBe(10);
    await waitFor(() => {
      expect(within(group).getByRole("radio", { name: "8" }).getAttribute("aria-checked")).toBe("true");
    }, SLOW);
    expect(requests.filter((r) => r.startsWith("chain-depth"))).toEqual(["chain-depth web 8"]);
    expect(within(sw).getByText("Depth").getAttribute("title")).toContain("ui.chain_depth");
    act(() => {
      within(group).getByRole("radio", { name: "10" }).click();
    });
    await waitFor(() => {
      expect(requests).toContain("chain-depth web 10");
    }, SLOW);
    expect(localStorage.getItem("graphvein.chain-depth.v1")).toBe("10");
    expect(parseArchParams(window.location.hash).depth).toBe(10);
    first.unmount();
    // The remembered choice wins over the repository default.
    setHash(`#/architecture?chain=${encodeURIComponent("web")}&node=${encodeURIComponent("c:web")}`);
    requests.length = 0;
    const second = render(<LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: null, impact_depth: null, chain_depth: 8, error: null })}>{ui}</LabelProvider>);
    await waitFor(() => {
      expect(requests).toContain("chain-depth web 10");
    }, SLOW);
    second.unmount();
    // A link's depth wins for the page and is not stored.
    setHash(`#/architecture?chain=${encodeURIComponent("web")}&node=${encodeURIComponent("c:web")}&depth=2`);
    requests.length = 0;
    render(ui);
    await waitFor(() => {
      expect(requests.filter((r) => r.startsWith("chain-depth"))).toEqual(["chain-depth web 2"]);
    }, SLOW);
    expect(localStorage.getItem("graphvein.chain-depth.v1")).toBe("10");
  });

  it("without a repository default or a choice the chain walks 6 steps", async () => {
    setHash(`#/architecture?chain=${encodeURIComponent("web")}&node=${encodeURIComponent("c:web")}`);
    const requests = renderSection("components");
    await waitFor(() => {
      expect(requests).toContain("chain-depth web 6");
    }, SLOW);
  });

  it("selecting a card requests its chain; Show in Links tab filters the ledger to it", async () => {
    setHash("#/architecture");
    const requests = renderSection("components");
    const card = await screen.findByRole("button", { name: "Component web" }, SLOW);
    act(() => {
      card.click();
    });
    expect(parseArchParams(window.location.hash).chain).toBe("web");
    await waitFor(() => {
      expect(requests).toContain("chain web both");
    }, SLOW);
    const links = await screen.findByTestId("show-in-links", {}, SLOW);
    act(() => {
      links.click();
    });
    const tab = await screen.findByTestId("links-tab", {}, SLOW);
    expect(within(tab).getByTestId("links-chain-filter").textContent).toContain("web");
    const rows = within(tab).getAllByTestId("link-row");
    expect(rows.length).toBeGreaterThan(0);
    for (const r of rows) expect(r.getAttribute("data-link")).toMatch(/web/);
    expect(within(tab).getByTestId("links-csv").getAttribute("href")).toMatch(/^data:text\/csv/);
  });

  it("adds the contract rows in the technical style", async () => {
    setHash("#/architecture?tab=links");
    renderSection("synthetic", "technical");
    const tab = await screen.findByTestId("links-tab", {}, SLOW);
    await waitFor(() => {
      expect(tab.textContent).toContain("[AP1]");
    }, SLOW);
    expect(tab.textContent).toContain("http CE1");
  });
});
