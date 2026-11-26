// T018g0b: a single-dialect model group renders exactly as before
// the per-dialect view (ADR 0065). The file snapshots were written by the
// view before K2 (commit ff8bc06) from the server's SQLModel goldens; the
// test fails on any change to that markup.
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const TEAM = "table:app/models.py#Team";

describe("single-dialect model groups (ADR 0065)", () => {
  it("the table detail and the set strip render as before", async () => {
    // The snapshot predates T029's "Routes that accept or return this
    // model" section (model_routes), which modelRoutes.test.tsx checks; it
    // is left out here so the snapshot keeps checking only K2's markup.
    const client = fakeSchemaClient({ summary: summaryGolden("models"), sets: setsGolden("models"), tables: { [TEAM]: { ...tableGolden("schema-table-model"), model_routes: null } } });
    window.history.replaceState(null, "", `#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(TEAM)}`);
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.getByTestId("model-history").textContent).not.toBe("");
      expect(screen.getAllByTestId("column-row").length).toBeGreaterThan(0);
      expect(screen.queryByTestId("table-ide")).not.toBeNull();
    });
    expect(screen.queryByTestId("dialect-type")).toBeNull();
    expect(screen.queryByTestId("tool-type")).toBeNull();
    expect(screen.queryByTestId("group-dialects")).toBeNull();
    await expect(screen.getByTestId("table-detail").outerHTML).toMatchFileSnapshot("./__snapshots__/single-dialect-table-detail.html");
    await expect(screen.getByTestId("set-strip").outerHTML).toMatchFileSnapshot("./__snapshots__/single-dialect-set-strip.html");
  });
});
