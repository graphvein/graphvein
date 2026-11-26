// T025 revision: a near miss names its route by the full path when the
// seed item or a loaded path gives it, else the path as written, marked.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ChangeItem, ChangesImpact } from "@/api/changes";
import { ImpactBody } from "@/sections/changes/ImpactTab";
import { impactOf, itemsOf } from "@/test/changesFixtures";

const LIST = "route:server/app.ts#GET%20%2Fapi%2Fitems";
const ROUTER = "route:server/router.ts#GET%20%2Fitems%2F%3Aid";
const OTHER = "route:server/other.ts#GET%20%2Fx";

function withNearMisses(data: ChangesImpact, routes: string[]): ChangesImpact {
  return { ...data, unknowns: { ...data.unknowns, near_misses: routes.map((route) => ({ route, call: "client_call:web/a.ts#c", status: "ambiguous" })) } };
}

describe("near misses (T025 revision)", () => {
  it("show a reached route's full path, the seed's, or the path as written with a marker", () => {
    const data = impactOf("previous-rev1-rev2", "function:server/handlers.ts#listItems");
    if (data === null) throw new Error("golden impact missing");
    const seed = itemsOf("previous-rev1-rev2", { base: "previous", group: "routes" }).items.find((i) => i.identity === ROUTER) as ChangeItem;
    render(<ImpactBody data={withNearMisses(data, [LIST, ROUTER, OTHER])} paths={data.paths} more={null} item={{ ...seed }} />);
    const shown = screen.getAllByTestId("near-miss-route").map((e) => e.textContent);
    expect(shown).toEqual(["GET /api/items", "GET /api/v2/items/{id}", "GET /xpath as written"]);
  });
});
