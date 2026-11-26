// Manifest editing from the UI (T022 B10, ADR 0075 "Manifest editing from
// the UI"; docs/cli/serve.md "Project manifest endpoints"). The YAML stays
// the source of truth: the UI sends typed operations, shows the server's
// diff of graphvein-project.yaml and writes only after a confirm, with the
// hash of the manifest it was based on and of the previewed result. No
// operation names a file; adding a member by folder stays in the CLI.

import { ApiError, fetchJSON } from "@/api/client";

export const MANIFEST_PATH = "/api/v0/project/manifest";
export const SUGGESTIONS_PATH = "/api/v0/project/suggestions";

export interface Selector {
  bases: string[];
  origins: string[];
  path_prefixes: string[];
}

export interface ConsumeEntry {
  repository: string;
  scope: string;
  default_scope: boolean;
  select: Selector;
}

export interface ManifestMember {
  repository: string;
  repository_source: string;
  checkout: boolean;
  consumes: ConsumeEntry[];
  scopes: { name: string; path: string; consumes: ConsumeEntry[] }[];
}

export interface Manifest {
  project: string;
  file: string;
  hash: string;
  content: string;
  token: string;
  members: ManifestMember[];
}

/** Op is one typed manifest operation, as the server takes it. */
export type Op =
  | {
      op: "add_consume";
      member: string;
      scope?: string;
      consume: {
        repository: string;
        scope?: string;
        default_scope?: boolean;
        select?: Partial<Selector>;
      };
    }
  | { op: "remove_consume"; member: string; scope?: string; target: Target }
  | {
      op: "set_select";
      member: string;
      scope?: string;
      target: Target;
      select: Partial<Selector>;
    }
  | {
      op: "set_default_scope";
      member: string;
      scope?: string;
      repository: string;
      value: boolean;
    }
  | { op: "add_scope"; member: string; name: string; path: string }
  | { op: "remove_scope"; member: string; name: string }
  | { op: "rename_repository"; member: string; to: string }
  | { op: "accept_suggestion"; number: number };

export interface Target {
  repository: string;
  scope?: string;
  default_scope?: boolean;
}

export interface EditResult {
  applied: boolean;
  base_hash: string;
  result_hash: string;
  diff: string;
  valid: boolean;
  problems: string[];
  changes: string[];
  reindex: { kind: "link_pass" | "project"; reason: string };
  /** T022 review: per old name of a rename, the stored index the write
   * deletes. Each needs its explicit confirm (confirm_delete). */
  retiring?: Retiring[];
}

/** Retiring is one stored index a rename deletes from the store shared by
 * all projects. standalone: no stored link pass lists it (it was indexed
 * on its own), so it may be the store's only copy. */
export interface Retiring {
  repository: string;
  stored: boolean;
  nodes: number;
  projects: string[];
  standalone: boolean;
  confirm: string;
}

export interface Evidence {
  row: string;
  member: string;
  file: string;
  line: number;
  key: string;
  value: string;
}

export interface Proposal {
  number: number;
  member: string;
  scope?: string;
  provider: string;
  provider_scope?: string;
  select: Partial<Selector>;
  evidence: Evidence[];
  declared: boolean;
}

export interface Suggestions {
  hash: string;
  proposals: Proposal[] | null;
}

/** shaped checks that a body is an object with key; the fields are the
 * server's documented ones (docs/cli/serve.md). */
function shaped(body: unknown, key: string, path: string): object {
  if (typeof body !== "object" || body === null || !(key in body))
    throw new ApiError("format", `${path}: unexpected response`);
  return body;
}

export async function fetchManifest(): Promise<Manifest> {
  return shaped(
    await fetchJSON(MANIFEST_PATH),
    "hash",
    MANIFEST_PATH,
  ) as Manifest;
}

export async function fetchSuggestions(): Promise<Suggestions> {
  return shaped(
    await fetchJSON(SUGGESTIONS_PATH, { timeoutMs: 60_000 }),
    "proposals",
    SUGGESTIONS_PATH,
  ) as Suggestions;
}

/** postEdit previews ops on the manifest m (resultHash undefined) or
 * applies the previewed result. */
export async function postEdit(
  m: Manifest,
  operations: Op[],
  resultHash?: string,
  confirmDelete: string[] = [],
): Promise<EditResult> {
  const body =
    resultHash === undefined
      ? { base_hash: m.hash, operations }
      : confirmDelete.length === 0
        ? {
            base_hash: m.hash,
            operations,
            apply: true,
            result_hash: resultHash,
          }
        : {
            base_hash: m.hash,
            operations,
            apply: true,
            result_hash: resultHash,
            confirm_delete: confirmDelete,
          };
  return shaped(
    await fetchJSON(MANIFEST_PATH, {
      method: "POST",
      body,
      headers: { "X-Graphvein-Token": m.token },
      timeoutMs: 60_000,
    }),
    "diff",
    MANIFEST_PATH,
  ) as EditResult;
}

/** selectText words a selector for a list ("" when it covers every call
 * without a literal origin). */
export function selectText(s: Partial<Selector>): string {
  return [
    ["bases", s.bases],
    ["origins", s.origins],
    ["path_prefixes", s.path_prefixes],
  ]
    .filter(
      (p): p is [string, string[]] => p[1] !== undefined && p[1].length > 0,
    )
    .map(([k, v]) => `${k} ${v.join(", ")}`)
    .join("; ");
}
