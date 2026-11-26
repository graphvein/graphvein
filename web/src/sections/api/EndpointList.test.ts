import { describe, expect, it } from "vitest";

import { endpointLabel, matchesFilter, noPathText, rowAriaLabel } from "@/sections/api/EndpointList";
import { endpointsGolden } from "@/test/apiFixtures";

const http = endpointsGolden("api-endpoints-production").transports.find((t) => t.transport === "http");

describe("rowAriaLabel", () => {
  it("never says no linked caller for a server action an unread use may reach (T024b R1)", () => {
    const actions = endpointsGolden("api-endpoints-production").transports.find((t) => t.transport === "server_action");
    const row = actions?.endpoints[1];
    if (row === undefined) throw new Error("fixture changed");
    expect(rowAriaLabel({ ...row, caller_count: 0, flags: ["unread_use"] }, "production")).toMatch(/, may be called \(unread use\)$/);
    expect(rowAriaLabel({ ...row, caller_count: 0, flags: [] }, "production")).toMatch(/, no linked caller in production$/);
  });

  it("says no linked caller, 1 caller or n callers", () => {
    const row = http?.endpoints[0];
    if (row === undefined) throw new Error("fixture changed");
    expect(rowAriaLabel({ ...row, caller_count: 0 }, "all")).toMatch(/, no linked caller$/);
    expect(rowAriaLabel({ ...row, caller_count: 1 }, "all")).toMatch(/, 1 caller$/);
    expect(rowAriaLabel({ ...row, caller_count: 3 }, "all")).toMatch(/, 3 callers$/);
  });
  it("says no linked caller in production in production scope", () => {
    const row = http?.endpoints[0];
    if (row === undefined) throw new Error("fixture changed");
    expect(rowAriaLabel({ ...row, caller_count: 0 }, "production")).toMatch(/, no linked caller in production$/);
  });
});

describe("unknown-prefix routes (T033 UP-29)", () => {
  const up = endpointsGolden("unknown-prefix-endpoints").transports.find((t) => t.transport === "http");
  it("labels a route without a full path by its known part with the prefix marked", () => {
    const row = up?.endpoints.find((r) => r.transport === "http" && r.prefix_unknown?.path === "…/upb/orders/{id}");
    if (row === undefined || row.transport !== "http") throw new Error("fixture changed");
    expect(row.path).toBeNull();
    expect(row.path_count).toBe(0);
    expect(row.flags).toContain("prefix_unresolved");
    expect(row.flags).not.toContain("unmounted");
    expect(row.prefix_unknown?.reason_text).toMatch(/^prefix unknown: /);
    expect(endpointLabel(row)).toBe("GET …/upb/orders/{id}");
    expect(matchesFilter(row, "upb/orders")).toBe(true);
  });
  it("keeps a known prefix above the unknown one", () => {
    expect(up?.endpoints.some((r) => r.transport === "http" && r.prefix_unknown?.path === "/outer/…/upb/inner/{name}")).toBe(true);
  });
});

// JX-F4: a row with no path of any kind says what the server knows, never
// "not mounted": shadowed, path withheld (flag path_withheld, row CR-44:
// owncast's ANY /* on the root router), else path not known.
describe("routes without a path", () => {
  it("words the row from the server's flags", () => {
    const row = http?.endpoints[0];
    if (row === undefined) throw new Error("fixture changed");
    const bare = { ...row, path: null, prefix_unknown: null };
    expect(noPathText({ ...bare, flags: ["unmounted"] })).toMatchObject({ text: "path not known" });
    expect(noPathText({ ...bare, flags: ["unmounted"] }).title).toContain("not proof that it is unreachable");
    expect(noPathText({ ...bare, flags: ["path_withheld"] })).toMatchObject({ text: "path withheld" });
    expect(noPathText({ ...bare, flags: ["path_withheld"] }).title).toContain("another pattern of the ServeMux");
    expect(noPathText({ ...bare, flags: ["shadowed"] })).toMatchObject({ text: "shadowed" });
    for (const flags of [["unmounted"], ["path_withheld"], ["shadowed"]]) expect(noPathText({ ...bare, flags }).text).not.toContain("mounted");
  });
});
