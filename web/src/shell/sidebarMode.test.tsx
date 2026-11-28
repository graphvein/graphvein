// The sidebar's width (T064, T061 D2 variant A): one width on every
// section, the user's choice remembered per browser, the rail by default
// below 1100 px with the full sidebar opened over the content there.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { SIDEBAR_KEY, isSidebarShortcut, readChoice, sidebarLayout } from "@/shell/sidebarMode";
import { fakeClient } from "@/test/apiFixtures";
import { fakeArchClient } from "@/test/architectureFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));

function setHash(hash: string) {
  window.history.replaceState(null, "", hash === "" ? window.location.pathname : hash);
}

/** narrow mocks matchMedia: the window is below 1100 px. */
function narrow() {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }));
}

function shell() {
  return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} architectureClient={fakeArchClient("components")} unreadFetcher={() => new Promise(() => undefined)} />);
}

function key(k: string, target: Element = document.body) {
  fireEvent.keyDown(target, { key: k });
}

beforeEach(() => {
  setHash("#/overview");
});

afterEach(() => {
  window.localStorage.clear();
  vi.unstubAllGlobals();
});

describe("sidebarLayout", () => {
  it("follows the stored choice from 1100 px, and is the rail below with the overlay on demand", () => {
    expect(sidebarLayout(null, true, false)).toEqual({ rail: false, overlay: false });
    expect(sidebarLayout("rail", true, false)).toEqual({ rail: true, overlay: false });
    expect(sidebarLayout("expanded", true, true)).toEqual({ rail: false, overlay: false });
    expect(sidebarLayout(null, false, false)).toEqual({ rail: true, overlay: false });
    expect(sidebarLayout("expanded", false, false)).toEqual({ rail: true, overlay: false });
    expect(sidebarLayout("expanded", false, true)).toEqual({ rail: true, overlay: true });
  });

  it("reads only the two stored values and survives blocked storage", () => {
    window.localStorage.setItem(SIDEBAR_KEY, "wide");
    expect(readChoice()).toBeNull();
    window.localStorage.setItem(SIDEBAR_KEY, "rail");
    expect(readChoice()).toBe("rail");
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(readChoice()).toBeNull();
    vi.restoreAllMocks();
  });

  it("'[' is the shortcut, never while typing or with a modifier", () => {
    const input = document.createElement("input");
    const base = { ctrlKey: false, metaKey: false, altKey: false, defaultPrevented: false, isComposing: false, target: document.body };
    expect(isSidebarShortcut({ ...base, key: "[" })).toBe(true);
    expect(isSidebarShortcut({ ...base, key: "[", target: input })).toBe(false);
    expect(isSidebarShortcut({ ...base, key: "[", ctrlKey: true })).toBe(false);
    expect(isSidebarShortcut({ ...base, key: "]" })).toBe(false);
  });
});

describe("the shell's sidebar", () => {
  it("keeps the full sidebar on graph views: no automatic rail", async () => {
    setHash("#/architecture");
    shell();
    await screen.findByTestId("logo-link");
    expect(screen.queryByTestId("sidebar-rail")).toBeNull();
    expect(screen.getByTestId("sidebar-collapse").getAttribute("title")).toBe("Collapse the sidebar ([)");
  });

  it("collapses with the button and with '[', remembers it, and expands from the rail", async () => {
    shell();
    fireEvent.click(await screen.findByTestId("sidebar-collapse"));
    expect(screen.getByTestId("sidebar-rail")).toBeTruthy();
    expect(window.localStorage.getItem(SIDEBAR_KEY)).toBe("rail");
    // The rail's logo is the home link too.
    expect(within(screen.getByTestId("sidebar-rail")).getByTestId("logo-link").getAttribute("href")).toBe("#/overview");
    fireEvent.click(screen.getByTestId("sidebar-expand"));
    expect(screen.queryByTestId("sidebar-rail")).toBeNull();
    expect(window.localStorage.getItem(SIDEBAR_KEY)).toBe("expanded");
    act(() => {
      key("[");
    });
    expect(screen.getByTestId("sidebar-rail")).toBeTruthy();
  });

  it("starts as the stored rail", async () => {
    window.localStorage.setItem(SIDEBAR_KEY, "rail");
    shell();
    expect(await screen.findByTestId("sidebar-rail")).toBeTruthy();
  });

  it("below 1100 px: the rail by default; expanding opens the sidebar over the content, Esc and navigation close it", async () => {
    narrow();
    window.localStorage.setItem(SIDEBAR_KEY, "expanded");
    shell();
    await screen.findByTestId("sidebar-rail");
    expect(screen.queryByTestId("sidebar-overlay")).toBeNull();
    fireEvent.click(screen.getByTestId("sidebar-expand"));
    const overlay = screen.getByTestId("sidebar-overlay");
    // Over the content: the grid column stays the rail.
    expect(screen.getByTestId("sidebar-rail")).toBeTruthy();
    expect(within(overlay).getByTestId("sidebar-collapse").getAttribute("title")).toBe("Close the sidebar (Esc)");
    act(() => {
      key("Escape");
    });
    expect(screen.queryByTestId("sidebar-overlay")).toBeNull();
    act(() => {
      key("[");
    });
    expect(screen.getByTestId("sidebar-overlay")).toBeTruthy();
    act(() => {
      window.location.hash = "#/api";
    });
    await waitFor(() => {
      expect(screen.queryByTestId("sidebar-overlay")).toBeNull();
    });
    // The overlay never changes the stored choice.
    expect(window.localStorage.getItem(SIDEBAR_KEY)).toBe("expanded");
  });
});
