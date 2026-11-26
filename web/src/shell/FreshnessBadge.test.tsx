import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TooltipProvider } from "@/components/ui/tooltip";
import { ApiError } from "@/api/client";
import { parseStatus, type Status } from "@/api/status";
import type { StatusView } from "@/hooks/useStatus";
import { formatShortTime, formatTime } from "@/lib/time";
import { FreshnessBadge, FreshnessDetails } from "@/shell/FreshnessBadge";
import { GOLDENS, goldenStatus, rawGolden } from "@/test/fixtures";

function ok(status: Status): StatusView {
  return { kind: "ok", status };
}

function badge(view: StatusView) {
  render(
    <TooltipProvider>
      <FreshnessBadge view={view} />
    </TooltipProvider>,
  );
  return screen.getByRole("status");
}

function details(view: StatusView): string {
  const { container } = render(<FreshnessDetails view={view} />);
  return container.textContent;
}

function staleWith(paths: string[], count = paths.length, truncated = false): Status {
  const status = goldenStatus("stale");
  status.freshness.changed_files = paths;
  status.freshness.changed_count = count;
  status.freshness.changed_files_truncated = truncated;
  return status;
}

const paths = (n: number) => Array.from({ length: n }, (_, i) => `src/file${String(i).padStart(2, "0")}.ts`);
const finished = "2026-11-03T14:32:05.123Z";
const checked = "2026-11-03T14:40:00Z";

describe("FreshnessBadge", () => {
  it("shows Fresh with the index time", () => {
    const el = badge(ok(goldenStatus("fresh")));
    expect(el.textContent).toBe(`Freshindexed ${formatShortTime(finished)}`);
    expect(details(ok(goldenStatus("fresh")))).toBe(
      // The golden's settings comparison says unchanged: said only here.
      `The index matches the working tree.Index settings unchanged since the last index run.Checked ${formatTime(checked)}`,
    );
  });

  it("shows one changed file in the singular", () => {
    const el = badge(ok(staleWith(["main.go"])));
    expect(el.textContent).toBe(`Stale: 1 file changedsince ${formatShortTime(finished)}`);
    const text = details(ok(staleWith(["main.go"])));
    expect(text).toContain("Changed since the last index:main.go");
    expect(text).toContain("Run graphvein index to update.");
    expect(text).not.toContain("more");
  });

  it("shows the golden's stale count and paths", () => {
    const el = badge(ok(goldenStatus("stale")));
    expect(el.textContent).toContain("Stale: 3 files changed");
    const text = details(ok(goldenStatus("stale")));
    for (const p of GOLDENS.stale.freshness.changed_files) expect(text).toContain(p);
    expect(text).toContain(`Checked ${formatTime(checked)}`);
  });

  it("lists at most 20 paths and counts the rest", () => {
    const { container } = render(<FreshnessDetails view={ok(staleWith(paths(25)))} />);
    expect(container.querySelectorAll("li")).toHaveLength(20);
    expect(container.textContent).toContain("and 5 more");
    expect(container.textContent).not.toContain("truncated");
  });

  it("counts paths the server left out and says the list was truncated", () => {
    const { container } = render(<FreshnessDetails view={ok(staleWith(paths(20), 1500, true))} />);
    expect(container.querySelectorAll("li")).toHaveLength(20);
    expect(container.textContent).toContain("and 1480 more");
    expect(container.textContent).toContain("(list truncated by the server)");
    cleanup();
    expect(badge(ok(staleWith(paths(20), 1500, true))).textContent).toContain(`Stale: ${new Intl.NumberFormat().format(1500)} files changed`);
  });

  it("shows Not indexed", () => {
    const el = badge(ok(goldenStatus("not_indexed")));
    expect(el.textContent).toBe("Not indexed");
    expect(details(ok(goldenStatus("not_indexed")))).toContain("This repository has no index yet. Run graphvein index.");
  });

  it("shows Freshness unknown with the server's error", () => {
    const el = badge(ok(goldenStatus("unknown")));
    expect(el.textContent).toBe("Freshness unknown");
    expect(details(ok(goldenStatus("unknown")))).toContain("Graphvein could not check freshness: injected failure");
  });

  it("shows an unrecognised state as unknown and names it", () => {
    const raw = rawGolden("fresh");
    (raw.freshness as Record<string, unknown>).state = "weird";
    const view = ok(parseStatus(raw));
    expect(badge(view).textContent).toBe("Freshness unknown");
    expect(details(view)).toContain("does not know: weird");
  });

  it("shows a skeleton while loading", () => {
    const el = badge({ kind: "loading" });
    expect(el.textContent).toBe("Checking freshness");
    expect(el.querySelector('[data-slot="skeleton"]')).not.toBeNull();
    expect(details({ kind: "loading" })).toBe("");
  });

  it("shows Status unavailable after a failed request", () => {
    const view: StatusView = { kind: "failed", error: new ApiError("network", "Failed to fetch"), at: new Date(checked) };
    expect(badge(view).textContent).toBe("Status unavailable");
    const text = details(view);
    expect(text).toContain("The last status request failed: Failed to fetch");
    expect(text).toContain(`Attempted ${formatTime(checked)}`);
  });

  it("renders Fresh only for a successful answer whose state is fresh", () => {
    const views: [string, StatusView][] = [
      ["loading", { kind: "loading" }],
      ["checking", { kind: "checking" }],
      ["failed", { kind: "failed", error: new Error("x"), at: new Date() }],
      ["stale", ok(goldenStatus("stale"))],
      ["not_indexed", ok(goldenStatus("not_indexed"))],
      ["unknown", ok(goldenStatus("unknown"))],
    ];
    for (const state of ["weird", "FRESH", "fresh ", ""]) {
      const raw = rawGolden("fresh");
      (raw.freshness as Record<string, unknown>).state = state;
      views.push([`reported ${JSON.stringify(state)}`, ok(parseStatus(raw))]);
    }
    for (const [label, view] of views) {
      const { unmount } = render(
        <TooltipProvider>
          <FreshnessBadge view={view} />
        </TooltipProvider>,
      );
      const el = screen.getByRole("status");
      expect(el.textContent, label).not.toMatch(/\bFresh\b/);
      expect(el.querySelector('[data-state-kind="fresh"]'), label).toBeNull();
      unmount();
    }
    expect(badge(ok(goldenStatus("fresh"))).querySelector('[data-state-kind="fresh"]')).not.toBeNull();
  });

  it("shows Checking... while an old answer is withdrawn", () => {
    const el = badge({ kind: "checking" });
    expect(el.textContent).toBe("Checking...");
    expect(el.querySelector('[data-state-kind="checking"]')).not.toBeNull();
    expect(details({ kind: "checking" })).toBe(
      "The last answer is too old to show; a new check is running.Freshness is unknown until it answers.",
    );
  });

  it("opens the details on click and keeps them open", async () => {
    const el = badge(ok(goldenStatus("stale")));
    const button = within(el).getByRole("button");
    fireEvent.click(button);
    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toContain("helper/added.go");
    fireEvent.click(button);
    expect(screen.getByRole("tooltip").textContent).toContain("helper/added.go");
  });

  it("opens the tooltip on keyboard focus", async () => {
    const el = badge(ok(goldenStatus("stale")));
    fireEvent.focus(within(el).getByRole("button"));
    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toContain("helper/added.go");
  });

  it("settings changed", () => {
    // Row ST-48: changed shows on the badge with the keys in the tooltip.
    const st = goldenStatus("fresh");
    st.freshness.settings = { state: "changed", reported_state: "changed", changed_keys: ["index.shape_depth", "openapi.base_paths"], unknown_keys: [], reason: null };
    const el = badge(ok(st));
    expect(el.textContent).toBe(`Fresh · settings changedindexed ${formatShortTime(finished)}`);
    expect(within(el).getByRole("button").getAttribute("data-state-kind")).toBe("fresh-settings");
    const text = details(ok(st));
    expect(text).toContain("index settings changed since the last index run: index.shape_depth, openapi.base_paths. Re-index to apply.");
    cleanup();
    // A stale badge adds it to its time.
    const stale = goldenStatus("stale");
    stale.freshness.settings = st.freshness.settings;
    expect(badge(ok(stale)).textContent).toContain(`since ${formatShortTime(finished)} · settings changed`);
  });

  it("unknown or absent settings never read as unchanged", () => {
    const st = goldenStatus("fresh");
    st.freshness.settings = { state: "unknown", reported_state: "unknown", changed_keys: [], unknown_keys: [], reason: "the last run stored no settings" };
    const el = badge(ok(st));
    expect(el.textContent).toBe(`Freshindexed ${formatShortTime(finished)}`);
    expect(details(ok(st))).toContain("is unknown: the last run stored no settings");
    expect(details(ok(st))).not.toContain("unchanged");
    cleanup();
    // A state this UI does not know reads as unknown.
    const raw = rawGolden("fresh");
    (raw.freshness as Record<string, unknown>).settings = { state: "drifted", changed_keys: [] };
    const parsed = parseStatus(raw);
    expect(parsed.freshness.settings?.state).toBe("unknown");
    expect(details(ok(parsed))).toContain("the server reported drifted");
    cleanup();
    const absent = goldenStatus("fresh");
    delete absent.freshness.settings;
    expect(details(ok(absent))).not.toMatch(/settings/i);
  });
});
