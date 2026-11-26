import { describe, expect, it } from "vitest";

import { callsPath, endpointPath, parseIdeStatus } from "@/api/endpoints";
import { flowPath } from "@/api/flow";
import { parseIdeInfo } from "@/api/ide";
import { sourcePath } from "@/api/source";
import builtin from "../../../internal/server/testdata/ide-builtin.golden.json";

describe("editor parameter", () => {
  it("is added to the four URLs only when a choice is active", () => {
    expect(endpointPath({ id: "r", scope: "production", root: null })).toBe("/api/v0/api/endpoint?id=r&scope=production");
    expect(endpointPath({ id: "r", scope: "production", root: null, editor: "zed" })).toBe("/api/v0/api/endpoint?id=r&scope=production&editor=zed");
    const calls = { transport: "http" as const, statuses: ["linked"], scope: "production" as const, root: null };
    expect(callsPath(calls)).not.toContain("editor");
    expect(callsPath({ ...calls, editor: "jetbrains" })).toBe("/api/v0/api/calls?transport=http&scope=production&editor=jetbrains&status=linked");
    expect(flowPath({ id: "r", scope: "all", root: null })).not.toContain("editor");
    expect(flowPath({ id: "r", scope: "all", root: null, editor: "zed" })).toBe("/api/v0/api/flow?id=r&scope=all&editor=zed");
    expect(sourcePath({ id: "n", context: 8 })).toBe("/api/v0/source?id=n&context=8");
    expect(sourcePath({ id: "n", context: 8, editor: "vscode-insiders" })).toBe("/api/v0/source?id=n&context=8&editor=vscode-insiders");
  });
});

describe("parseIdeStatus", () => {
  it("accepts editor and source as strings, null or absent", () => {
    expect(parseIdeStatus({ available: true, error: null, label: "Open in Zed", editor: "zed", source: "request" }, "ide")).toEqual({
      available: true,
      error: null,
      label: "Open in Zed",
      editor: "zed",
      source: "request",
    });
    expect(parseIdeStatus({ available: true, error: null, label: null, editor: null, source: "repository" }, "ide").editor).toBeNull();
    expect(parseIdeStatus({ available: true, error: null }, "ide")).toEqual({ available: true, error: null, label: null, editor: null, source: null });
  });

  it("rejects other types", () => {
    expect(() => parseIdeStatus({ available: true, error: null, editor: 1 }, "ide")).toThrow(/ide.editor is not a string or null/);
    expect(() => parseIdeStatus({ available: true, error: null, source: false }, "ide")).toThrow(/ide.source is not a string or null/);
  });
});

describe("parseIdeInfo", () => {
  it("reads the server's golden body", () => {
    const info = parseIdeInfo(builtin);
    expect(info.presets.map((p) => p.editor)).toEqual(["vscode", "vscode-insiders", "cursor", "vscodium", "jetbrains", "zed"]);
    expect(info.presets.find((p) => p.editor === "cursor")?.status).toBe("partly_verified");
    expect(info.repository).toEqual({ available: true, error: null, editor: "vscode", label: "Open in VS Code", custom: false, source: "builtin" });
  });

  it("rejects malformed bodies", () => {
    const bad = structuredClone(builtin) as unknown as { presets: unknown[]; repository: Record<string, unknown> };
    bad.presets.push(bad.presets[0]);
    expect(() => parseIdeInfo(bad)).toThrow(/repeats vscode/);
    const noCustom = structuredClone(builtin) as unknown as { repository: Record<string, unknown> };
    delete noCustom.repository.custom;
    expect(() => parseIdeInfo(noCustom)).toThrow(/custom/);
  });
});
