import { describe, expect, it } from "vitest";

import { isFullPathEntry, pathAssumptions, pathAssumptionTitle, prefixDisplay, prefixReasonText, prefixUnknownPaths, servedPath, shadowedPaths } from "@/lib/routePath";

describe("servedPath (T025 revision)", () => {
  it("restores a FastAPI trailing slash and keeps the root", () => {
    expect(servedPath({ template: "/api/v1/items", trailing_slash: true })).toBe("/api/v1/items/");
    expect(servedPath({ template: "/api/v1/items" })).toBe("/api/v1/items");
    expect(servedPath({ template: "/", trailing_slash: true })).toBe("/");
    expect(servedPath({ template: null })).toBeNull();
    expect(servedPath("x")).toBeNull();
  });
});

describe("unknown-prefix paths (T033 UP-29, NX-R4 under U6)", () => {
  it("marks the unknown prefix with an ellipsis, above it when known", () => {
    expect(prefixDisplay("", "/api/hosts/{host}/logs/stream", false)).toBe("…/api/hosts/{host}/logs/stream");
    expect(prefixDisplay("/outer", "/upb/inner/{name}", false)).toBe("/outer/…/upb/inner/{name}");
    expect(prefixDisplay("", "/upb/files", true)).toBe("…/upb/files/");
  });
  it("reads entries and Next.js unresolved-base paths, never full paths", () => {
    const attrs = {
      paths: [
        { template: "/api/search", resolution: "unresolved" },
        { template: "/api/known", resolution: "literal" },
      ],
      unresolved_prefix_paths: [{ template: "/api/version", prefix: "unresolved", prefix_reason: "route_prefix_unresolved", hop: "mount:a.go#f r.Route(base)" }],
    };
    expect(prefixUnknownPaths(attrs)).toEqual([
      { path: "…/api/version", reason: "route_prefix_unresolved", hop: "mount:a.go#f r.Route(base)", scope: "production" },
      { path: "…/api/search", reason: "nextjs_base_path_unresolved", hop: null, scope: "production" },
    ]);
    expect(isFullPathEntry({ template: "/api/search", resolution: "unresolved" })).toBe(false);
    expect(isFullPathEntry({ template: "/api/known", resolution: "literal" })).toBe(true);
  });
  it("words the reason", () => {
    expect(prefixReasonText("route_mount_unresolved")).toBe("prefix unknown: the router is reached through a mount Graphvein does not prove");
    expect(prefixReasonText("other")).toBe("prefix unknown");
  });
});

describe("path assumptions and withheld paths", () => {
  it("reads a path's assumptions with the mounts they name", () => {
    const a = pathAssumptions({ template: "/health", assumptions: ["RO-V1"], may_answer_first: ['app.mount(os.environ["SUB"],sub) (api/app/main.py:58)'] });
    expect(a).toEqual({ ids: ["RO-V1"], names: ['app.mount(os.environ["SUB"],sub) (api/app/main.py:58)'] });
    expect(a === null ? "" : pathAssumptionTitle(a)).toContain("a mount at an unknown prefix may answer first");
    expect(pathAssumptions({ template: "/x" })).toBeNull();
  });
  it("reads the withheld paths of a shadowed route", () => {
    expect(shadowedPaths({ shadowed_paths: [{ path: "/sh/a", by: "m" }, { path: 1 }] })).toEqual([{ path: "/sh/a", by: "m" }]);
    expect(shadowedPaths({})).toEqual([]);
  });
});
