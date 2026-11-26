import { afterEach, describe, expect, it, vi } from "vitest";

import { impactPath } from "@/api/changes";
import { fetchJSON, setRequestRepository, withRepository } from "@/api/client";
import { fetchProject, parseCallProject, parseProject } from "@/api/project";
import { projectBody } from "@/test/projectFixtures";

afterEach(() => {
  setRequestRepository(null);
  vi.unstubAllGlobals();
});

describe("project API (T022 B9)", () => {
  it("parses /api/v0/project with the B9 parts", () => {
    const p = parseProject(projectBody());
    expect(p.members.map((m) => m.repository)).toEqual(["react", "express", "axum-ddd"]);
    expect(p.members[2]?.checkout).toBe(false);
    expect(p.members[2]?.freshness.error).toContain("no checkout here");
    expect(p.members[0]?.finished_at).toBe("2026-11-03T14:32:05.123Z");
    expect(p.ambiguous.groups[0]?.providers).toEqual(["axum-ddd", "express"]);
    expect(p.link_pass.outcomes).toEqual({ linked: 27, ambiguous: 2, dynamic: 4 });
  });

  it("reads an older body without the B9 parts as empty", () => {
    const body = projectBody();
    delete body.declarations;
    delete body.ambiguous;
    delete body.conflicts;
    const p = parseProject(body);
    expect(p.declarations).toEqual({ consumes: [], scopes: [] });
    expect(p.ambiguous).toEqual({ total: 0, groups: [] });
    expect(p.conflicts).toEqual([]);
  });

  it("names the selected member on every per-member request, never on the project ones", () => {
    expect(withRepository("/api/v0/status")).toBe("/api/v0/status");
    setRequestRepository("express");
    expect(withRepository("/api/v0/status")).toBe("/api/v0/status?repository=express");
    expect(withRepository("/api/v0/api/endpoint?id=x")).toBe("/api/v0/api/endpoint?id=x&repository=express");
    expect(withRepository("/api/v0/project")).toBe("/api/v0/project");
    expect(withRepository("/api/v0/index/run")).toBe("/api/v0/index/run");
    expect(withRepository("/third-party-licenses.txt")).toBe("/third-party-licenses.txt");
    setRequestRepository("a b");
    expect(withRepository("/api/v0/ui")).toBe("/api/v0/ui?repository=a%20b");
    // A request naming its member keeps it (the Explorer's member counts).
    expect(withRepository("/api/v0/openapi?repository=react")).toBe("/api/v0/openapi?repository=react");
  });

  it("sends the repository through fetchJSON", async () => {
    const fetch = vi.fn<(url: string) => Promise<Response>>(() => Promise.resolve(new Response("{}", { status: 200 })));
    vi.stubGlobal("fetch", fetch);
    setRequestRepository("react");
    await fetchJSON("/api/v0/schema/summary");
    expect(fetch.mock.calls[0]?.[0]).toBe("/api/v0/schema/summary?repository=react");
  });

  it("asks the project change impact in project mode, except for a ref base", () => {
    expect(impactPath({ base: "previous", id: "x" })).toBe("/api/v0/changes/impact?base=previous&id=x");
    setRequestRepository("express");
    expect(impactPath({ base: "previous", id: "x" })).toBe("/api/v0/project/changes/impact?base=previous&id=x");
    expect(impactPath({ base: "ref:main", id: "x" })).toBe("/api/v0/changes/impact?base=ref%3Amain&id=x");
  });

  it("tells a single-repository server (404) from a project", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: { code: "not_found", message: "no" } }), { status: 404 }))));
    await expect(fetchProject({})).resolves.toEqual({ kind: "single" });
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify(projectBody()), { status: 200 }))));
    const probe = await fetchProject({});
    expect(probe.kind).toBe("project");
  });

  it("parses a call's project links and outcomes", () => {
    const p = parseCallProject(
      {
        link_pass: { state: "stale", pass: 3, stale_reasons: ["react was indexed again"] },
        links: [{ target: { repository: "express", identity: "route:x", kind: "route", name: "GET /x", file: "a.ts", line: 3 }, kind: "calls_route", provenance: "inferred", confidence: 0.7, evidence: [], assumption: "a", stale: true, stale_reasons: ["r"] }],
        outcomes: [{ template: "", row: "PL-L1", status: "linked", reason: "", candidate_count: 0, candidates: [] }],
      },
      "c",
    );
    expect(p?.links[0]?.target.repository).toBe("express");
    expect(parseCallProject(undefined, "c")).toBeNull();
  });
});
