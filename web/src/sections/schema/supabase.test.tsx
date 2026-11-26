// T024c1: a Supabase set from the server's goldens (fixture
// supabase-migrations): the set strip names the platform assumption
// (SB-V2), the table detail shows row level security and its policies
// (SB-M3), and a reference to auth.users reads as a platform table, never
// as unresolved (SB-M2).
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
const PROFILES = "table:app/supabase/migrations/20240101000000_profiles.sql#profiles";

describe("Supabase sets (T024c1, T024c1b)", () => {
  it("shows the platform assumption, row level security and platform references", async () => {
    const client = fakeSchemaClient({ summary: summaryGolden("supabase"), sets: setsGolden("supabase"), tables: { [PROFILES]: tableGolden("schema-table-supabase") } });
    window.history.replaceState(null, "", `#/schema?set=${enc("app/supabase/migrations")}&sel=${enc(PROFILES)}`);
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.queryByTestId("row-security-section")).not.toBeNull();
    });
    expect(screen.getByTestId("group-platform").textContent).toContain("Supabase platform objects (SB-V2)");
    expect(screen.getByTestId("rls-flags").textContent).toBe("enabled · not forced");
    expect(screen.getAllByTestId("policy").map((r) => r.textContent)).toEqual(["Profiles are publicSELECTprofiles", "Own profileUPDATEprofiles", "Insert ownINSERTprofiles"]);
    expect(screen.getAllByTestId("external-ref")[0]?.textContent).toContain("auth.users");
    expect(screen.queryByTestId("unresolved-ref")).toBeNull();
    // T024c1b R1: the set's insert into storage.buckets (SB-V5).
    expect(screen.getAllByTestId("group-platform-assumption").map((e) => e.textContent)).toEqual([
      "Assumes (SB-V5): inserts into Supabase platform tables (storage.buckets) succeed on a fresh project, and the platform's triggers change no object of the set: insert into storage.buckets (id, name, public) values ('avatars', 'avatars', true)",
    ]);
  });

  // T024c1b: a bare supabase/migrations directory (SB-V3) that creates two
  // platform extensions (SB-V4) shows each assumption in the set strip.
  it("shows the bare directory and platform extension assumptions", async () => {
    const client = fakeSchemaClient({ summary: summaryGolden("supabase"), sets: setsGolden("supabase"), tables: {} });
    window.history.replaceState(null, "", `#/schema?set=${enc("no-config/supabase/migrations")}`);
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
    await waitFor(() => {
      expect(screen.getAllByTestId("group-platform-assumption").length).toBe(3);
    });
    expect(screen.getAllByTestId("group-platform-assumption").map((e) => e.textContent)).toEqual([
      "Assumes (SB-V3): read as Supabase CLI migrations; no supabase/config.toml found",
      "Assumes (SB-V4): Supabase platform: extension http is available to the migration role",
      "Assumes (SB-V4): Supabase platform: extension vector is available to the migration role",
    ]);
  });
});
