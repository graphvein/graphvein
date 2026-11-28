// The code peek in the API detail pane (T016e2): mouse, button and
// keyboard paths, the collapsed coverage, and the run guard.

import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { ApiClient } from "@/api/endpoints";
import type { SourceParams } from "@/api/source";
import { MIN_TOP } from "@/components/VerticalSplit";
import type { StatusView } from "@/hooks/useStatus";
import { ApiSection, PEEK_MIN_BOTTOM, PEEK_ROW_ROOM, peekSplitLimits } from "@/sections/api/ApiSection";
import { COVERAGE_OPEN_KEY } from "@/sections/api/SummaryStrip";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { sourceBody, stubSource } from "@/test/sourceFixtures";

const SEL = "route:server/app.ts#GET%20%2Ft016d-items";
const FIRST = "client_call:root-call.ts#atRoot.GET%20%27%2Ft016d-items%27";
const HANDLER = "function:server/app.ts#listItems";
const PEEK = /^Peek source of /;

function ok(): StatusView {
  return { kind: "ok", status: goldenStatus("fresh") };
}

function rowOf(id: string): HTMLElement {
  const row = [...document.querySelectorAll<HTMLElement>("tr[data-peek-row]")].find((r) => r.getAttribute("data-peek-row") === id);
  if (row === undefined) throw new Error(`no row ${id}`);
  return row;
}

async function setup(finishedAt?: string, tab?: "overview") {
  const requests: SourceParams[] = [];
  const client = stubSource((p) => sourceBody({ ...(finishedAt !== undefined ? { finished_at: finishedAt } : {}), node: { ...(sourceBody() as { node: Record<string, unknown> }).node, identity: p.id } }), requests);
  window.history.replaceState(null, "", `#/api?sel=${encodeURIComponent(SEL)}${tab === undefined ? "" : `&tab=${tab}`}`);
  render(<ApiSection status={ok()} client={fakeClient()} sourceClient={client} />);
  await screen.findByTestId("endpoint-detail");
  return requests;
}

/** classes lists the unconditional classes (no variant prefix). */
function classes(testId: string): string[] {
  return screen.getByTestId(testId).className.split(/\s+/).filter((c) => !c.includes(":"));
}

function peekPanel(): HTMLElement | null {
  return screen.queryByTestId("code-peek");
}

beforeEach(() => {
  window.localStorage.clear();
  // The coverage cards are behind the "Coverage and roots" disclosure
  // (T065): open it, so the tests see them collapse under the peek.
  window.localStorage.setItem(COVERAGE_OPEN_KEY, "open");
});

describe("peekSplitLimits", () => {
  it("keeps the measured route header, the handle and a row's first line above the peek", () => {
    // Not measured yet (or jsdom): the split's own limits.
    expect(peekSplitLimits(0)).toEqual({});
    // A short header keeps the split's usual top minimum.
    expect(peekSplitLimits(40)).toEqual({ minTop: MIN_TOP, minBottom: PEEK_MIN_BOTTOM });
    // The owncast route at 900 px wide: 278 px of header and tabs.
    expect(peekSplitLimits(277.5)).toEqual({ minTop: 278 + 12 + PEEK_ROW_ROOM, minBottom: PEEK_MIN_BOTTOM });
  });
});

describe("code peek in the API detail pane", () => {
  it("opens from a row click, collapses the coverage cards and marks the row", async () => {
    const requests = await setup();
    expect(peekPanel()).toBeNull();
    expect(classes("coverage-cards")).not.toContain("hidden");
    const row = rowOf(FIRST);
    expect(row.className).toContain("cursor-pointer");
    fireEvent.click(row.querySelector("td") as HTMLElement);
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(FIRST);
    await waitFor(() => {
      expect(panel.getAttribute("data-state")).toBe("exact");
    });
    expect(requests.at(-1)).toEqual({ id: FIRST, context: 8 });
    expect(row.getAttribute("data-peeking")).toBe("true");
    expect(within(row).getByText("peeking")).toBeTruthy();
    expect(within(row).getByRole("button", { name: PEEK }).getAttribute("aria-pressed")).toBe("true");
    expect(classes("coverage-cards")).toContain("hidden");
    expect(classes("summary-strip")).not.toContain("hidden");
    expect(within(panel).getByText(/^caller 1 of \d+$/)).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize the code peek" })).toBeTruthy();
  });

  it("the peek button toggles; the IDE link in a row does not open the peek", async () => {
    await setup();
    const row = rowOf(FIRST);
    fireEvent.click(within(row).getByRole("link"));
    expect(peekPanel()).toBeNull();
    const btn = within(row).getByRole("button", { name: PEEK });
    expect(btn.getAttribute("title")).toBe("Peek source (Enter)");
    expect(btn.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(btn);
    expect(await screen.findByTestId("code-peek")).toBeTruthy();
    fireEvent.click(within(rowOf(FIRST)).getByRole("button", { name: PEEK }));
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    expect(classes("coverage-cards")).not.toContain("hidden");
  });

  it("Enter opens, Down moves the peek with the focus, Esc closes and returns focus", async () => {
    const requests = await setup();
    const rows = [...document.querySelectorAll<HTMLElement>("tbody tr[data-peek-row]")];
    expect(rows.length).toBeGreaterThan(1);
    // One tab stop: the first row.
    expect(rows.map((r) => r.tabIndex)).toEqual(rows.map((_, i) => (i === 0 ? 0 : -1)));
    const first = rows[0] as HTMLElement;
    const second = rows[1] as HTMLElement;
    first.focus();
    fireEvent.keyDown(first, { key: "Enter" });
    await waitFor(() => {
      expect(peekPanel()?.getAttribute("data-node")).toBe(first.getAttribute("data-peek-row"));
    });
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(second);
    await waitFor(() => {
      expect(peekPanel()?.getAttribute("data-node")).toBe(second.getAttribute("data-peek-row"));
    });
    expect(requests.at(-1)?.id).toBe(second.getAttribute("data-peek-row"));
    expect(within(peekPanel() as HTMLElement).getByText(/^caller 2 of \d+$/)).toBeTruthy();
    // Esc inside the peek closes it; focus goes back to the row.
    const panel = peekPanel() as HTMLElement;
    within(panel).getByTestId("peek-body").focus();
    fireEvent.keyDown(within(panel).getByTestId("peek-body"), { key: "Escape" });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement?.getAttribute("data-peek-row")).toBe(second.getAttribute("data-peek-row"));
    });
    // Esc on the row closes too; Up without a peek only moves focus.
    fireEvent.keyDown(second, { key: "Enter" });
    await screen.findByTestId("code-peek");
    fireEvent.keyDown(rowOf(second.getAttribute("data-peek-row") ?? ""), { key: "Escape" });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    fireEvent.keyDown(rowOf(second.getAttribute("data-peek-row") ?? ""), { key: "ArrowUp" });
    expect(document.activeElement).toBe(rowOf(first.getAttribute("data-peek-row") ?? ""));
    expect(peekPanel()).toBeNull();
  });

  it("peeks the handler from its button", async () => {
    await setup(undefined, "overview");
    fireEvent.click(screen.getByRole("button", { name: "Peek handler source" }));
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(HANDLER);
    expect(within(panel).getByText("handler")).toBeTruthy();
  });

  it("does not show a source answer of another index run", async () => {
    await setup("2026-12-01T00:00:00Z");
    fireEvent.click(within(rowOf(FIRST)).getByRole("button", { name: PEEK }));
    await waitFor(() => {
      expect(peekPanel()?.getAttribute("data-state")).toBe("mismatch");
    });
    expect(peekPanel()?.querySelector("[data-line]")).toBeNull();
  });

  it("closes when another endpoint is selected", async () => {
    await setup();
    fireEvent.click(within(rowOf(FIRST)).getByRole("button", { name: PEEK }));
    await screen.findByTestId("code-peek");
    act(() => {
      window.history.replaceState(null, "", "#/api?sel=route%3Aserver%2Fapp.ts%23GET%20%2Fnone");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    expect(window.location.hash).not.toContain("peek=");
  });

  it("names each row's peek button after its caller and marks the open row", async () => {
    await setup();
    const rows = [...document.querySelectorAll<HTMLElement>("tbody tr[data-peek-row]")];
    const names = rows.map((r) => within(r).getByRole("button", { name: PEEK }).getAttribute("aria-label"));
    expect(new Set(names).size).toBe(rows.length);
    expect(within(rowOf(FIRST)).getByRole("button", { name: PEEK }).getAttribute("aria-label")).toMatch(/^Peek source of \S.* \(root-call\.ts:\d+\)$/);
    expect(rows.map((r) => r.getAttribute("aria-expanded"))).toEqual(rows.map(() => "false"));
    expect(rows.some((r) => r.hasAttribute("aria-controls"))).toBe(false);
    fireEvent.click(rowOf(FIRST).querySelector("td") as HTMLElement);
    const panel = await screen.findByTestId("code-peek");
    const row = rowOf(FIRST);
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(row.getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.id).not.toBe("");
    expect(within(row).getByRole("button", { name: PEEK }).getAttribute("aria-controls")).toBe(panel.id);
    expect(rows.filter((r) => r.getAttribute("aria-expanded") === "true")).toHaveLength(1);
  });

  it("row keys work while focus is on the row's peek button or IDE link", async () => {
    await setup();
    const rows = [...document.querySelectorAll<HTMLElement>("tbody tr[data-peek-row]")];
    const first = rows[0] as HTMLElement;
    const secondId = rows[1]?.getAttribute("data-peek-row") ?? "";
    const btn = within(first).getByRole("button", { name: PEEK });
    // Enter on the button itself is the button's own (no row toggle).
    btn.focus();
    fireEvent.keyDown(btn, { key: "Enter" });
    expect(peekPanel()).toBeNull();
    fireEvent.click(btn);
    await screen.findByTestId("code-peek");
    // Down from the button moves to the next row and the peek follows.
    fireEvent.keyDown(btn, { key: "ArrowDown" });
    expect(document.activeElement).toBe(rowOf(secondId));
    await waitFor(() => {
      expect(peekPanel()?.getAttribute("data-node")).toBe(secondId);
    });
    // Enter on the IDE link is the link's own; Esc from it closes the peek.
    const link = within(rowOf(secondId)).getByRole("link");
    link.focus();
    fireEvent.keyDown(link, { key: "Enter" });
    expect(peekPanel()?.getAttribute("data-node")).toBe(secondId);
    fireEvent.keyDown(link, { key: "Escape" });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    // Focus stays on the link it was on.
    await waitFor(() => {
      expect(document.activeElement).toBe(link);
    });
    fireEvent.keyDown(link, { key: "ArrowUp" });
    expect(document.activeElement).toBe(rowOf(first.getAttribute("data-peek-row") ?? ""));
  });

  it("returns focus to the handler's peek button when the peek closes", async () => {
    await setup(undefined, "overview");
    const btn = screen.getByRole("button", { name: "Peek handler source" });
    btn.focus();
    fireEvent.click(btn);
    const panel = await screen.findByTestId("code-peek");
    await waitFor(() => {
      expect(panel.getAttribute("data-state")).toBe("exact");
    });
    within(panel).getByTestId("peek-body").focus();
    fireEvent.keyDown(within(panel).getByTestId("peek-body"), { key: "Escape" });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Peek handler source" }));
    });
    // The close button returns focus too.
    fireEvent.click(screen.getByRole("button", { name: "Peek handler source" }));
    const close = within(await screen.findByTestId("code-peek")).getByRole("button", { name: "Close code peek (Esc)" });
    close.focus();
    fireEvent.click(close);
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Peek handler source" }));
    });
  });

  it("keeps the strips and the root filter while the peek is open", async () => {
    await setup();
    expect(screen.getByTestId("strip-http")).toBeTruthy();
    fireEvent.click(rowOf(FIRST).querySelector("td") as HTMLElement);
    await screen.findByTestId("code-peek");
    expect(screen.getByTestId("strip-http")).toBeTruthy();
    expect(screen.queryAllByTestId(/^strip-roots-/)).toHaveLength(0);
    expect(screen.getByRole("combobox", { name: /^Source root/ })).toBeTruthy();
  });

});

// T016i D4: the open peek is in the hash (`peek=<identity>`), so a reload
// or a shared link opens it again.
describe("code peek in the hash", () => {
  const base = `#/api?sel=${encodeURIComponent(SEL)}`;
  const withPeek = (id: string, extra = "") => `${base}${extra}&peek=${encodeURIComponent(id)}`;

  function mount(hash: string, client: ApiClient = fakeClient()) {
    const source = stubSource((p) => sourceBody({ node: { ...(sourceBody() as { node: Record<string, unknown> }).node, identity: p.id } }));
    window.history.replaceState(null, "", hash);
    return render(<ApiSection status={ok()} client={client} sourceClient={source} />);
  }

  it("writes the peek on open, removes it on close, and a reload opens the same peek", async () => {
    const view = mount(base);
    await screen.findByTestId("endpoint-detail");
    fireEvent.click(within(rowOf(FIRST)).getByRole("button", { name: PEEK }));
    await screen.findByTestId("code-peek");
    expect(window.location.hash).toBe(withPeek(FIRST));
    // A reload: the page renders again from the hash alone.
    view.unmount();
    mount(window.location.hash);
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(FIRST);
    expect(rowOf(FIRST).getAttribute("data-peeking")).toBe("true");
    fireEvent.click(within(rowOf(FIRST)).getByRole("button", { name: PEEK }));
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    expect(window.location.hash).toBe(base);
  });

  it("opens a pasted link, also for the handler on the Overview tab", async () => {
    mount(withPeek(HANDLER, "&tab=overview"));
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(HANDLER);
  });

  it("ignores an identity that is not a target, without rewriting it", async () => {
    mount(withPeek("function:nowhere.ts#bogus"));
    await screen.findByTestId("endpoint-detail");
    expect(peekPanel()).toBeNull();
    expect(window.location.hash).toBe(withPeek("function:nowhere.ts#bogus"));
    expect(screen.getByTestId("coverage-cards").className).not.toMatch(/(^| )hidden( |$)/);
  });

  it("keeps the value while the endpoint loads, then opens it", async () => {
    const real = fakeClient();
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    mount(withPeek(FIRST), { ...real, endpoint: (p, o) => gate.then(() => real.endpoint(p, o)) });
    await screen.findByText("Loading the endpoint");
    expect(peekPanel()).toBeNull();
    expect(window.location.hash).toBe(withPeek(FIRST));
    act(() => {
      release();
    });
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(FIRST);
  });

  it("Overview <-> Callers keeps the peek", async () => {
    mount(withPeek(HANDLER));
    await screen.findByTestId("code-peek");
    fireEvent.click(screen.getByTestId("tab-overview"));
    await waitFor(() => {
      expect(window.location.hash).toContain(`peek=${encodeURIComponent(HANDLER)}`);
    });
    expect(peekPanel()?.getAttribute("data-node")).toBe(HANDLER);
    fireEvent.click(screen.getByTestId("tab-callers"));
    expect(window.location.hash).toBe(withPeek(HANDLER));
    expect(peekPanel()?.getAttribute("data-node")).toBe(HANDLER);
  });

  it("a hashchange to a hash with or without peek opens or closes it (back and forward)", async () => {
    mount(base);
    await screen.findByTestId("endpoint-detail");
    expect(peekPanel()).toBeNull();
    act(() => {
      window.history.pushState(null, "", withPeek(FIRST));
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect((await screen.findByTestId("code-peek")).getAttribute("data-node")).toBe(FIRST);
    const active = document.activeElement;
    act(() => {
      window.history.pushState(null, "", base);
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await waitFor(() => {
      expect(peekPanel()).toBeNull();
    });
    // A hash change never moves the focus.
    expect(document.activeElement).toBe(active);
  });
});
