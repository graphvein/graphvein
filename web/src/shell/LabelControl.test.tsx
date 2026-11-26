import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { UiDefaults, UiFetcher } from "@/api/ui";
import { parseUiDefaults } from "@/api/ui";
import { LabelProvider, useLabel, useLabels } from "@/hooks/useLabels";
import { BANNER_KEY, LABELS_KEY } from "@/lib/labelChoice";
import { LabelBanner, LabelSetting } from "@/shell/LabelControl";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

const answer =
  (labels: string | null): UiFetcher =>
  () =>
    Promise.resolve<UiDefaults>({ api_version: 0, labels, error: null });

function Probe() {
  const l = useLabel("edge", { kind: "calls_route", provenance: "inferred", confidence: 0.9, match: "exact" });
  const { source } = useLabels();
  return (
    <span data-testid="probe" title={l.title} data-source={source}>
      {l.text}
    </span>
  );
}

function shell(fetcher: UiFetcher) {
  return render(
    <LabelProvider fetcher={fetcher}>
      <LabelBanner />
      <LabelSetting />
      <Probe />
    </LabelProvider>,
  );
}

describe("label style (T025b)", () => {
  it("first launch: plain labels and the one-time banner with two equal choices and examples", async () => {
    shell(answer(null));
    const banner = await screen.findByTestId("labels-banner");
    expect(screen.getByRole("heading", { name: "How should Graphvein label facts?" })).toBeTruthy();
    expect(screen.getByTestId("labels-banner-plain").textContent).toBe("Plaine.g. Exact path match · 0.9");
    expect(screen.getByTestId("labels-banner-technical").textContent).toBe("Technicale.g. inferred 0.9 exact, I4");
    // Not a dialog: nothing is modal and the page stays usable.
    expect(banner.closest("[role=dialog]")).toBeNull();
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    expect(screen.getByTestId("probe").getAttribute("title")).toBe("Stored calls_route edge: inferred 0.9 exact. The number is a fixed rank per match rule, not a probability.");
    // Nothing is chosen in this browser yet.
    expect(screen.getByTestId("labels-unset").getAttribute("aria-checked")).toBe("true");
  });

  it("choosing Technical in the banner applies at once, is stored, and the banner does not return", async () => {
    const first = shell(answer(null));
    fireEvent.click(await screen.findByTestId("labels-banner-technical"));
    expect(screen.queryByTestId("labels-banner")).toBeNull();
    expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    expect(screen.getByTestId("probe").getAttribute("title")).toBe("Stored calls_route edge: Exact path match");
    expect(screen.getByTestId("labels-technical").getAttribute("aria-checked")).toBe("true");
    expect(window.localStorage.getItem(LABELS_KEY)).toBe("technical");
    first.unmount();
    shell(answer(null));
    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    });
    expect(screen.queryByTestId("labels-banner")).toBeNull();
  });

  it("the close button keeps plain labels and the banner does not return", async () => {
    const first = shell(answer(null));
    fireEvent.click(await screen.findByRole("button", { name: "Close: keep plain labels" }));
    expect(screen.queryByTestId("labels-banner")).toBeNull();
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    expect(window.localStorage.getItem(BANNER_KEY)).toBe("dismissed");
    expect(window.localStorage.getItem(LABELS_KEY)).toBeNull();
    first.unmount();
    shell(answer(null));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByTestId("labels-banner")).toBeNull();
  });

  it("the sidebar setting switches both ways", async () => {
    shell(answer(null));
    fireEvent.click(screen.getByTestId("labels-technical"));
    expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    fireEvent.click(screen.getByTestId("labels-plain"));
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    expect(window.localStorage.getItem(LABELS_KEY)).toBe("plain");
    await waitFor(() => {
      expect(screen.queryByTestId("labels-banner")).toBeNull();
    });
  });

  it("graphvein.yaml's ui.labels is the default without a banner; a browser choice wins", async () => {
    const first = shell(answer("technical"));
    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    });
    expect(screen.getByTestId("probe").getAttribute("data-source")).toBe("repository");
    expect(screen.queryByTestId("labels-banner")).toBeNull();
    expect(screen.getByTestId("labels-control").getAttribute("title")).toBe("Labels: Technical, the repository default from graphvein.yaml");
    first.unmount();
    window.localStorage.setItem(LABELS_KEY, "plain");
    shell(answer("technical"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    expect(screen.getByTestId("probe").getAttribute("data-source")).toBe("browser");
  });

  it("an unknown repository value or a failed request means plain, with the banner", async () => {
    const first = shell(answer("jargon"));
    await screen.findByTestId("labels-banner");
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    first.unmount();
    shell(() => Promise.reject(new Error("HTTP 500")));
    await screen.findByTestId("labels-banner");
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
  });

  it("blocked storage: plain by default, a choice lasts for the page and says so", async () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    shell(answer(null));
    await screen.findByTestId("labels-banner");
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    fireEvent.click(screen.getByTestId("labels-banner-technical"));
    expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    expect(screen.queryByTestId("labels-banner")).toBeNull();
    expect(screen.getByTestId("labels-setting").textContent).toContain("Not saved");
  });

  it("parses the /api/v0/ui body", () => {
    // A server before T042 sends no depth fields: they read as null.
    expect(parseUiDefaults({ api_version: 0, labels: "technical", error: null })).toEqual({ api_version: 0, labels: "technical", impact_depth: null, chain_depth: null, error: null, report_url: null });
    expect(parseUiDefaults({ api_version: 0, labels: null, impact_depth: 4, chain_depth: null, error: null })).toEqual({ api_version: 0, labels: null, impact_depth: 4, chain_depth: null, error: null, report_url: null });
    // A depth field in another form is ignored as absent; the labels stay
    // (T042 review), and nothing is written to the console.
    const warn = vi.spyOn(console, "warn");
    const err = vi.spyOn(console, "error");
    expect(parseUiDefaults({ api_version: 0, labels: "technical", impact_depth: "4", chain_depth: 2.5, error: null })).toEqual({ api_version: 0, labels: "technical", impact_depth: null, chain_depth: null, error: null, report_url: null });
    expect(parseUiDefaults({ api_version: 0, labels: "plain", impact_depth: { n: 4 }, chain_depth: 7, error: null })).toEqual({ api_version: 0, labels: "plain", impact_depth: null, chain_depth: 7, error: null, report_url: null });
    expect(warn).not.toHaveBeenCalled();
    expect(err).not.toHaveBeenCalled();
    expect(parseUiDefaults({ api_version: 0, labels: null, error: "bad yaml" }).error).toBe("bad yaml");
    expect(() => parseUiDefaults({ api_version: 0, labels: 3, error: null })).toThrow();
  });

  it("Not set clears the browser's choice and keeps the storage key: the repository default applies again", async () => {
    window.localStorage.setItem(LABELS_KEY, "plain");
    shell(answer("technical"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTestId("labels-plain").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByTestId("probe").textContent).toBe("Exact path match · 0.9");
    fireEvent.click(screen.getByTestId("labels-unset"));
    expect(window.localStorage.getItem(LABELS_KEY)).toBeNull();
    expect(screen.getByTestId("labels-unset").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByTestId("probe").textContent).toBe("inferred 0.9 exact");
    expect(screen.getByTestId("probe").getAttribute("data-source")).toBe("repository");
  });
});
