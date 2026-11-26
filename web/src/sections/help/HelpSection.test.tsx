// The Help page (T045): the guide rendered with an anchor per heading, a
// heading opened from its link, the Start here examples and their
// fallback (HP-4, HP-5), the "?" shortcut and the sidebar entry.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ApiClient } from "@/api/endpoints";
import type { Status } from "@/api/status";
import type { StatusFetcher, StatusView } from "@/hooks/useStatus";
import { helpHash, parseRoute } from "@/router";
import { GUIDE } from "@/sections/help/guide";
import { HelpSection, headingId } from "@/sections/help/HelpSection";
import { isHelpShortcut, isTypingTarget } from "@/sections/help/shortcut";
import { AppShell } from "@/shell/AppShell";
import { Sidebar } from "@/shell/Sidebar";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

function setHash(hash: string) {
  window.history.replaceState(null, "", hash === "" ? window.location.pathname : hash);
}

function ok(finishedAt?: string): StatusView {
  const status = goldenStatus("fresh");
  if (finishedAt !== undefined && status.index.last_run !== null) status.index.last_run.finished_at = finishedAt;
  return { kind: "ok", status };
}

beforeEach(() => {
  setHash("#/help");
});

describe("Help page", () => {
  it("renders every guide page with its headings, links and shortcut keys", () => {
    render(<HelpSection status={{ kind: "loading" }} client={fakeClient()} />);
    expect(screen.getByRole("heading", { level: 1, name: "Help" })).toBeTruthy();
    expect(screen.getAllByTestId("help-page")).toHaveLength(GUIDE.length);
    for (const name of ["Start here", "The sections", "Beyond the UI", "Questions", "Keyboard shortcuts"]) {
      expect(screen.getByRole("heading", { level: 2, name: new RegExp(`^${name}`) }), name).toBeTruthy();
    }
    const contents = screen.getByRole("navigation", { name: "On this page" });
    expect(within(contents).getByRole("link", { name: "Questions" }).getAttribute("href")).toBe("#/help?at=questions");
    // App links are links; a public document is named by its path.
    expect(screen.getAllByRole("link", { name: "API" })[0]?.getAttribute("href")).toBe("#/api");
    expect(document.body.textContent).toContain("docs/cli/mcp.md");
    expect(screen.queryByRole("link", { name: "mcp" })).toBeNull();
    // The shortcut table shows its keys as key caps.
    const keys = [...document.querySelectorAll("td kbd")].map((k) => k.textContent);
    expect(keys).toContain("?");
    expect(keys).toContain("Ctrl+K");
    // Nothing of the Markdown shows through.
    expect(document.body.textContent).not.toMatch(/\*\*|\]\(|<!--|graphvein:example/);
  });

  it("gives every heading its own link and anchor", () => {
    render(<HelpSection status={{ kind: "loading" }} client={fakeClient()} />);
    const headings = GUIDE.flatMap((p) => p.blocks.flatMap((b) => (b.t === "heading" ? [b.slug] : [])));
    const links = screen.getAllByTestId("heading-link");
    expect(links).toHaveLength(headings.length);
    for (const [i, slug] of headings.entries()) {
      const el = document.getElementById(headingId(slug));
      expect(el, slug).not.toBeNull();
      expect(links[i]?.getAttribute("href")).toBe(helpHash(slug));
    }
    expect(screen.getByRole("link", { name: "Link to “Is the number beside \"Inferred\" a probability?”" }).getAttribute("href")).toBe(
      "#/help?at=is-the-number-beside-inferred-a-probability",
    );
  });

  it("opens the heading a link names: scrolled to, focused and marked", async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    setHash(helpHash("how-do-i-open-the-code-in-my-editor"));
    render(<HelpSection status={{ kind: "loading" }} client={fakeClient()} />);
    const el = document.getElementById(headingId("how-do-i-open-the-code-in-my-editor"));
    expect(el?.getAttribute("data-current")).toBe("true");
    expect(document.activeElement).toBe(el);
    expect(scroll).toHaveBeenCalledWith({ block: "start" });
    // Another heading's link moves the mark.
    act(() => {
      window.location.hash = helpHash("is-what-i-see-up-to-date");
    });
    await waitFor(() => {
      expect(document.getElementById(headingId("is-what-i-see-up-to-date"))?.getAttribute("data-current")).toBe("true");
    });
    expect(el?.hasAttribute("data-current")).toBe(false);
  });

  it("shows each label's fixed example, marked as one, in the label's chip", () => {
    render(<HelpSection status={{ kind: "loading" }} client={fakeClient()} />);
    const want: [string, string, string, string][] = [
      ["fact", "GET /api/users", "Fact · 1.0", "bg-linked-soft"],
      ["inferred", 'fetch("/api/users")', "Inferred · 0.9", "border-dashed"],
      ["unknown", "fetch(baseUrl + path)", "Unknown", "bg-muted"],
    ];
    for (const [kind, subject, label, tone] of want) {
      const fixed = within(screen.getByTestId(`help-example-${kind}`)).getByTestId("help-example-fixed");
      expect(fixed.textContent, kind).toMatch(new RegExp(`^Example${subject.replace(/[()+]/g, "\\$&")} ${label}`));
      expect(fixed.querySelector("code")?.textContent).toBe(subject);
      const chip = within(fixed).getByTestId("help-example-chip");
      expect(chip.textContent).toBe(label);
      expect(chip.className, kind).toContain(tone);
      expect(fixed.querySelector("a")).toBeNull();
    }
  });

  it("follows each fixed example with the case of the index being viewed, with a link into it", async () => {
    render(<HelpSection status={ok()} client={fakeClient()} />);
    const fact = await screen.findByRole("link", { name: "GET /t016d-api/t016d-users" });
    expect(fact.getAttribute("href")).toContain("tab=overview");
    const index = (kind: string) => within(screen.getByTestId(`help-example-${kind}`)).getByTestId("help-example-index");
    expect(index("fact").textContent).toBe("In your index: GET /t016d-api/t016d-users Fact · 1.0 from its registration");
    expect(await screen.findByRole("link", { name: "GET /t016d-items" })).toBeTruthy();
    expect(index("inferred").textContent).toContain("Inferred · 0.9 called from root-call.ts:2");
    expect(within(index("inferred")).getByTestId("help-example-chip").className).toContain("border-dashed");
    expect(screen.getByRole("link", { name: "3 calls whose URL is built at runtime" }).getAttribute("href")).toBe("#/api?gap=http%3Adynamic");
    // The fixed example stays first, unchanged.
    for (const kind of ["fact", "inferred", "unknown"]) {
      const box = screen.getByTestId(`help-example-${kind}`);
      expect(box.firstElementChild?.getAttribute("data-testid")).toBe("help-example-fixed");
    }
    expect(screen.queryByTestId("help-example-pending")).toBeNull();
  });

  it("holds the index line's place while the examples load", () => {
    render(<HelpSection status={{ kind: "loading" }} client={fakeClient()} />);
    for (const kind of ["fact", "inferred", "unknown"]) {
      const box = screen.getByTestId(`help-example-${kind}`);
      expect(within(box).getByTestId("help-example-fixed")).toBeTruthy();
      expect(within(box).getByTestId("help-example-pending").className).toContain("min-h-6");
      expect(within(box).queryByTestId("help-example-index")).toBeNull();
    }
  });

  it("shows only the fixed examples without an index, with an empty index or after a failed request", async () => {
    // reserved: with an index, the index line's place is kept, so the box
    // keeps the height it had while loading. proven: the answer proves the
    // index has no case, and the line says so; else it stays empty.
    const onlyFixed = (reserved: boolean, proven = false) => {
      for (const kind of ["fact", "inferred", "unknown"]) {
        const box = screen.getByTestId(`help-example-${kind}`);
        expect(box.children, kind).toHaveLength(reserved ? 2 : 1);
        expect(within(box).getByTestId("help-example-fixed").textContent).toMatch(/^Example/);
        const empty = within(box).queryByTestId("help-example-empty");
        expect(empty !== null, kind).toBe(reserved);
        if (empty !== null) {
          expect(empty.textContent).toBe(proven ? "In your index: none of this kind yet" : "");
          expect(empty.className).toContain("min-h-6");
        }
        expect(box.querySelector("a")).toBeNull();
      }
      expect(document.body.textContent).not.toContain("No example");
      if (!proven) expect(document.body.textContent).not.toContain("In your index");
    };
    const status = goldenStatus("not_indexed");
    expect(status.index.last_run).toBeNull();
    const notIndexed: StatusView = { kind: "ok", status };
    const first = render(<HelpSection status={notIndexed} client={fakeClient()} />);
    onlyFixed(false);
    first.unmount();

    const requests: { kind: string }[] = [];
    const base = fakeClient(requests as never);
    const empty: ApiClient = {
      ...base,
      endpoints: async (p, o) => ({ ...(await base.endpoints(p, o)), transports: [] }),
    };
    const second = render(<HelpSection status={ok()} client={empty} />);
    await waitFor(() => {
      expect(screen.queryByTestId("help-example-pending")).toBeNull();
    });
    expect(requests.filter((r) => r.kind === "endpoints")).toHaveLength(1);
    onlyFixed(true, true);
    second.unmount();

    const failing: ApiClient = { ...fakeClient(), endpoints: () => Promise.reject(new Error("down")) };
    render(<HelpSection status={ok()} client={failing} />);
    await waitFor(() => {
      expect(screen.queryByTestId("help-example-pending")).toBeNull();
    });
    onlyFixed(true);
  });

  it("requests the examples again for a newer index run", async () => {
    const requests: { kind: string }[] = [];
    const client = fakeClient(requests as never);
    const { rerender } = render(<HelpSection status={ok("2026-11-01T00:00:00Z")} client={client} />);
    await screen.findByRole("link", { name: "GET /t016d-items" });
    const first = requests.filter((r) => r.kind === "endpoints").length;
    rerender(<HelpSection status={ok("2026-11-02T00:00:00Z")} client={client} />);
    await screen.findByRole("link", { name: "GET /t016d-items" });
    expect(requests.filter((r) => r.kind === "endpoints").length).toBe(first + 1);
  });
});

describe("the ? shortcut", () => {
  const key = (target: EventTarget | null, over: Partial<KeyboardEvent> = {}) => ({ key: "?", ctrlKey: false, metaKey: false, altKey: false, defaultPrevented: false, isComposing: false, target, ...over });

  it("is ? alone, never while typing", () => {
    const div = document.createElement("div");
    expect(isHelpShortcut(key(div))).toBe(true);
    expect(isHelpShortcut(key(div, { key: "/" }))).toBe(false);
    expect(isHelpShortcut(key(div, { ctrlKey: true }))).toBe(false);
    expect(isHelpShortcut(key(div, { metaKey: true }))).toBe(false);
    expect(isHelpShortcut(key(div, { defaultPrevented: true }))).toBe(false);
    for (const html of ["<input>", "<textarea></textarea>", "<select></select>", '<div contenteditable="true"><span>x</span></div>', '<div role="textbox"></div>', '<div role="combobox"></div>']) {
      const host = document.createElement("div");
      host.innerHTML = html;
      const el = host.firstElementChild?.firstElementChild ?? host.firstElementChild;
      expect(isTypingTarget(el ?? null), html).toBe(true);
      expect(isHelpShortcut(key(el ?? null)), html).toBe(false);
    }
  });

  it("opens Help from a section, and not from a field", async () => {
    setHash("#/api");
    const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} />);
    await screen.findByRole("heading", { name: "API" });
    const field = document.createElement("input");
    document.body.append(field);
    fireEvent.keyDown(field, { key: "?" });
    expect(window.location.hash).toBe("#/api");
    field.remove();
    act(() => {
      fireEvent.keyDown(document.body, { key: "?" });
    });
    expect(window.location.hash).toBe("#/help");
    expect(await screen.findByRole("heading", { level: 1, name: "Help" })).toBeTruthy();
    // The sidebar marks Help as the current page.
    const nav = screen.getByRole("navigation", { name: "Sections" });
    expect(within(nav).getByRole("link", { name: /^Help/ }).getAttribute("aria-current")).toBe("page");
  });
});

describe("the Help entry", () => {
  it("is at the bottom of the sidebar and of the icon rail", () => {
    const { unmount } = render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={{ kind: "loading" }} />);
    const entry = screen.getByTestId("sidebar-help");
    expect(entry.tagName).toBe("A");
    expect(entry.textContent).toMatch(/^Help/);
    // Settings, then Help, at the bottom (T039 x T045).
    expect(screen.getByTestId("sidebar-settings").contains(entry)).toBe(true);
    expect(entry.getAttribute("href")).toBe("#/help");
    expect(entry.getAttribute("aria-keyshortcuts")).toBe("?");
    unmount();
    render(<Sidebar route={parseRoute("#/help")} repository="fixture" status={{ kind: "loading" }} collapsed />);
    const rail = screen.getByTestId("rail-help");
    expect(rail.getAttribute("href")).toBe("#/help");
    expect(rail.getAttribute("aria-current")).toBe("page");
  });

  it("is a route of its own", () => {
    expect(parseRoute("#/help")).toMatchObject({ kind: "section", section: { id: "help" } });
    expect(parseRoute("#/help?at=questions")).toMatchObject({ kind: "section", section: { id: "help" } });
  });
});
