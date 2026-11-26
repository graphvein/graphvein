// A GET /api/v0/config answer for the Settings tests (T039), in
// the server's JSON shape (internal/server/settings.go, docs/cli/serve.md),
// parsed by the real guard so a field the parser does not accept fails here.

import { parseConfig, type Config, type ConfigClient, type EditRequest, type EditResult } from "@/api/config";
import { ApiError } from "@/api/client";

export const SAMPLE_YAML = [
  "# Graphvein settings",
  "version: 1",
  "ui:",
  "  labels: plain",
  "index:",
  "  shape_depth: 6",
  "openapi:",
  "  base_paths:",
  "    apps/app/controller: /api",
  "explorer:",
  "  path_values:",
  "    - op: getUser",
  "changes:",
  "  journal_runs: 30",
  "",
].join("\n");

/** rawConfig is the JSON body; overrides replace top-level fields. */
export function rawConfig(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  const key = (k: Record<string, unknown>) => ({ choices: undefined, reason: null, editable: true, line: null, ...k });
  return {
    api_version: 0,
    repository: "express",
    file: "graphvein.yaml",
    exists: true,
    hash: "sha256:3f9c0000aaaa",
    text: SAMPLE_YAML,
    editable: true,
    reason: null,
    token: "tok",
    config_error: null,
    keys: [
      key({ key: "ui.labels", kind: "enum", choices: ["plain", "technical"], value: "plain", default: "plain", origin: "file", line: 4, applies: "at_once" }),
      key({ key: "ide.editor", kind: "enum", choices: ["vscode", "zed", "jetbrains"], value: "vscode", default: "vscode", origin: "builtin", applies: "at_once" }),
      key({ key: "index.shape_depth", kind: "integer", min: 1, max: 10, value: 6, default: 6, origin: "file", line: 6, applies: "next_index_run" }),
      key({ key: "clients.link_generated_output", kind: "boolean", value: false, default: false, origin: "builtin", applies: "next_index_run" }),
      key({ key: "tests.include", kind: "list", value: null, default: null, origin: "not_set", applies: "next_index_run" }),
      key({ key: "openapi.base_paths", kind: "map", value: [{ key: "apps/app/controller", value: "/api" }], default: null, origin: "file", line: 9, applies: "next_index_run" }),
      key({ key: "architecture.hide", kind: "list", value: null, default: null, origin: "not_set", applies: "at_once" }),
    ],
    other_keys: [
      { key: "explorer.path_values", line: 11, edited_by: "/api/v0/explorer/path-values" },
      { key: "changes.journal_runs", line: 14, edited_by: null },
    ],
    reindex: { path: "/api/v0/index/run", scope: "repository" },
    index_settings: { state: "unchanged", changed_keys: [], unknown_keys: [], reason: null, index_hash: "sha256:1", current_hash: "sha256:1" },
    ignore: { file: ".graphveinignore", exists: false, text: null, editable: false },
    project: null,
    ...overrides,
  };
}

export function sampleConfig(overrides: Record<string, unknown> = {}): Config {
  return parseConfig(JSON.parse(JSON.stringify(rawConfig(overrides))));
}

export function editResult(o: Partial<EditResult> = {}): EditResult {
  return {
    applied: false,
    base_hash: "sha256:3f9c0000aaaa",
    result_hash: "sha256:bbbb",
    diff: "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n@@ -6 +6 @@\n-  shape_depth: 6\n+  shape_depth: 8",
    valid: true,
    problems: [],
    changes: ["sets index.shape_depth to 8 (was 6)"],
    reformatted: false,
    lost_comments: [],
    changed_keys: [{ key: "index.shape_depth", applies: "next_index_run" }],
    other_keys_changed: false,
    reindex_needed: true,
    ...o,
  };
}

/** EditCall records one POST. */
export interface EditCall {
  request: EditRequest;
  resultHash: string | undefined;
  base: string | null;
}

/** fakeConfigClient answers fetch with configs in turn (the last one
 * repeats) and edit with the answer given; calls collects the POSTs. */
export function fakeConfigClient(configs: Config[], edit: (call: EditCall) => EditResult | ApiError, calls: EditCall[] = []): ConfigClient & { fetches: number } {
  const client = {
    fetches: 0,
    fetch: () => {
      const c = configs[Math.min(client.fetches, configs.length - 1)];
      client.fetches++;
      return c === undefined ? Promise.reject(new Error("no config")) : Promise.resolve(c);
    },
    edit: (config: Config, request: EditRequest, resultHash?: string) => {
      const call = { request, resultHash, base: config.hash };
      calls.push(call);
      const answer = edit(call);
      return answer instanceof ApiError ? Promise.reject(answer) : Promise.resolve(answer);
    },
  };
  return client;
}
