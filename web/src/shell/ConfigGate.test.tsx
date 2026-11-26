import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";
import { editResult, fakeConfigClient, sampleConfig } from "@/test/settingsFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const MESSAGE = "graphvein.yaml: line 5: index.shape_depth must be an integer from 1 to 10";

function setHash(hash: string) {
  window.history.replaceState(null, "", hash);
}

beforeEach(() => {
  setHash("#/api");
});

describe("configuration gate (T039 ST-F)", () => {
  it("every view but Settings names the file and the decoder's message while graphvein.yaml does not load; a fix lifts it", async () => {
    const broken = sampleConfig({ config_error: MESSAGE });
    const config = fakeConfigClient([broken, broken, broken, sampleConfig()], () => editResult());
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} configClient={config} />);
    const screenEl = await screen.findByTestId("config-error-screen");
    expect(screenEl.textContent).toContain("graphvein.yaml has an error");
    expect(screenEl.textContent).toContain(MESSAGE);
    expect(screenEl.textContent).toContain("API is not shown while the file does not load");
    expect(screen.getByRole("link", { name: "Fix it in Settings" }).getAttribute("href")).toBe("#/settings");
    expect(screen.queryByTestId("card-http")).toBeNull();

    // Another section reads the file again: still broken.
    act(() => {
      window.location.hash = "#/schema";
    });
    await waitFor(() => {
      expect(config.fetches).toBe(2);
    });
    expect((await screen.findByTestId("config-error-screen")).textContent).toContain("Schema is not shown");

    // Settings is where it is fixed: never gated.
    act(() => {
      window.location.hash = "#/settings";
    });
    await waitFor(() => {
      expect(screen.queryByTestId("config-error-screen")).toBeNull();
    });

    // Fixed (in Settings or an editor): the next read lifts the gate.
    act(() => {
      window.location.hash = "#/api";
    });
    await waitFor(() => {
      expect(screen.queryByTestId("config-error-screen")).toBeNull();
    });
    expect(await screen.findByTestId("card-http")).toBeTruthy();
  });

  it("a config read that fails gates nothing", async () => {
    const config = fakeConfigClient([], () => editResult());
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} configClient={config} />);
    expect(await screen.findByTestId("card-http")).toBeTruthy();
    expect(screen.queryByTestId("config-error-screen")).toBeNull();
  });
});
