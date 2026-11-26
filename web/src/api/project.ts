// Project mode (graphvein serve --project; T022 B8 and B9, ADR 0075 "Server
// and UI"): GET /api/v0/project and the `project` parts of the endpoint,
// flow and calls answers. A single-repository server answers 404 on
// /api/v0/project, which is how the UI tells the two apart.

import { ApiError, fetchJSON } from "@/api/client";
import { ApiFormatError, array, bool, object, parseIdeStatus, parseNode, str, strOrNull, version, type IdeStatus, type JSONObject, type PresentedNode } from "@/api/endpoints";
import { readIndexSettings, type IndexSettings } from "@/api/indexSettings";
import { parseShapeDifferences, type ShapeDifferences } from "@/api/shapeDifferences";

export const PROJECT_PATH = "/api/v0/project";

/** MemberFreshness is a member checkout's freshness as /api/v0/status
 * words it ("unknown" with an error for a member without a checkout). */
export interface MemberFreshness {
  state: string;
  error: string | null;
  changed_count: number | null;
  /** checked_at is when the server computed this state (RFC 3339); null
   * when the server did not say. The UI never stamps its own time on it. */
  checked_at: string | null;
  /** The member's settings comparison (ADR 0088); null when not sent. */
  settings: IndexSettings | null;
}

/** MemberLinks is a member's place in the last link pass. */
export interface MemberLinks {
  in_pass: boolean;
  index_error: string | null;
  links: number;
  stale: number;
  stale_reasons: string[];
}

export interface ProjectMember {
  repository: string;
  repository_source: string;
  path: string | null;
  checkout: boolean;
  remote_url: string | null;
  indexed: boolean;
  finished_at: string | null;
  freshness: MemberFreshness;
  links: MemberLinks;
}

/** LinkPass is the last stored link pass: state "none" (no pass),
 * "fresh" or "stale". */
export interface LinkPass {
  state: string;
  pass: number;
  links: number;
  stale_links: number;
  outcomes: Record<string, number>;
  stale_reasons: string[];
}

export interface ProjectCheck {
  check: string;
  members: string[];
  message: string;
}

export interface ConsumeDeclaration {
  consumer: string;
  provider: string;
  scope: string;
  default_scope: boolean;
  text: string;
}

export interface ScopeDeclaration {
  repository: string;
  name: string;
  path: string;
}

/** NodeBrief names a node by (repository, identity); name, file and line
 * are empty when the member no longer stores it. */
export interface NodeBrief {
  repository: string;
  identity: string;
  kind: string;
  name: string;
  file: string;
  line: number;
}

export interface AmbiguousSource {
  source: NodeBrief;
  template: string;
  candidate_count: number;
  candidates: NodeBrief[];
}

export interface AmbiguityGroup {
  row: string;
  providers: string[];
  count: number;
  sources: AmbiguousSource[];
}

export interface DeclarationConflict {
  call: NodeBrief;
  consumer: string;
  declared: string[];
  target: NodeBrief;
  target_scope: string;
}

/** Project is GET /api/v0/project. */
export interface Project {
  api_version: number;
  project: string;
  server_version: string;
  members: ProjectMember[];
  link_pass: LinkPass;
  checks: ProjectCheck[];
  declarations: { consumes: ConsumeDeclaration[]; scopes: ScopeDeclaration[] };
  ambiguous: { total: number; groups: AmbiguityGroup[] };
  conflicts: DeclarationConflict[];
}

function num(obj: JSONObject, key: string, path: string): number {
  const v = obj[key];
  if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${key} is not a number`);
  return v;
}

function numOrNull(obj: JSONObject, key: string, path: string): number | null {
  return obj[key] === null || obj[key] === undefined ? null : num(obj, key, path);
}

function strings(value: unknown, path: string): string[] {
  const list = array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function counts(value: unknown, path: string): Record<string, number> {
  const obj = object(value, path);
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(obj)) {
    if (typeof v !== "number") throw new ApiFormatError(`${path}.${k} is not a number`);
    out[k] = v;
  }
  return out;
}

function parseBrief(value: unknown, path: string): NodeBrief {
  const o = object(value, path);
  return { repository: str(o, "repository", path), identity: str(o, "identity", path), kind: str(o, "kind", path), name: str(o, "name", path), file: str(o, "file", path), line: num(o, "line", path) };
}

function parseMember(value: unknown, path: string): ProjectMember {
  const o = object(value, path);
  const remote = o.remote === null || o.remote === undefined ? null : object(o.remote, `${path}.remote`);
  const index = o.index === null || o.index === undefined ? null : object(o.index, `${path}.index`);
  const f = object(o.freshness, `${path}.freshness`);
  const l = object(o.links, `${path}.links`);
  return {
    repository: str(o, "repository", path),
    repository_source: str(o, "repository_source", path),
    path: strOrNull(o, "path", path),
    checkout: bool(o, "checkout", path),
    remote_url: remote === null ? null : strOrNull(remote, "url", `${path}.remote`),
    indexed: index !== null,
    finished_at: index === null ? null : str(index, "finished_at", `${path}.index`),
    freshness: {
      state: str(f, "state", `${path}.freshness`),
      error: f.error === undefined || f.error === "" ? null : strOrNull(f, "error", `${path}.freshness`),
      changed_count: numOrNull(f, "changed_count", `${path}.freshness`),
      checked_at: strOrNull(f, "checked_at", `${path}.freshness`),
      settings: f.settings === undefined || f.settings === null ? null : readIndexSettings(f.settings, `${path}.freshness.settings`, (m) => new ApiFormatError(m)),
    },
    links: {
      in_pass: bool(l, "in_pass", `${path}.links`),
      index_error: strOrNull(l, "index_error", `${path}.links`),
      links: num(l, "links", `${path}.links`),
      stale: num(l, "stale", `${path}.links`),
      stale_reasons: strings(l.stale_reasons, `${path}.links.stale_reasons`),
    },
  };
}

/** parseProject checks a GET /api/v0/project body. The B9 parts
 * (declarations, ambiguous, conflicts) read as empty from an older server. */
export function parseProject(value: unknown): Project {
  const p = "project";
  const o = object(value, p);
  const lp = object(o.link_pass, `${p}.link_pass`);
  const decl = o.declarations === undefined ? null : object(o.declarations, `${p}.declarations`);
  const amb = o.ambiguous === undefined ? null : object(o.ambiguous, `${p}.ambiguous`);
  return {
    api_version: version(o, p),
    project: str(o, "project", p),
    server_version: str(o, "server_version", p),
    members: array(o.members, `${p}.members`).map((m, i) => parseMember(m, `${p}.members[${String(i)}]`)),
    link_pass: {
      state: str(lp, "state", `${p}.link_pass`),
      pass: num(lp, "pass", `${p}.link_pass`),
      links: num(lp, "links", `${p}.link_pass`),
      stale_links: num(lp, "stale_links", `${p}.link_pass`),
      outcomes: counts(lp.outcomes, `${p}.link_pass.outcomes`),
      stale_reasons: strings(lp.stale_reasons, `${p}.link_pass.stale_reasons`),
    },
    checks: array(o.checks, `${p}.checks`).map((c, i) => {
      const q = `${p}.checks[${String(i)}]`;
      const co = object(c, q);
      return { check: str(co, "check", q), members: strings(co.members, `${q}.members`), message: str(co, "message", q) };
    }),
    declarations: {
      consumes:
        decl === null
          ? []
          : array(decl.consumes, `${p}.declarations.consumes`).map((c, i) => {
              const q = `${p}.declarations.consumes[${String(i)}]`;
              const co = object(c, q);
              return { consumer: str(co, "consumer", q), provider: str(co, "provider", q), scope: str(co, "scope", q), default_scope: bool(co, "default_scope", q), text: str(co, "text", q) };
            }),
      scopes:
        decl === null
          ? []
          : array(decl.scopes, `${p}.declarations.scopes`).map((c, i) => {
              const q = `${p}.declarations.scopes[${String(i)}]`;
              const co = object(c, q);
              return { repository: str(co, "repository", q), name: str(co, "name", q), path: str(co, "path", q) };
            }),
    },
    ambiguous:
      amb === null
        ? { total: 0, groups: [] }
        : {
            total: num(amb, "total", `${p}.ambiguous`),
            groups: array(amb.groups, `${p}.ambiguous.groups`).map((g, i) => {
              const q = `${p}.ambiguous.groups[${String(i)}]`;
              const go = object(g, q);
              return {
                row: str(go, "row", q),
                providers: strings(go.providers, `${q}.providers`),
                count: num(go, "count", q),
                sources: array(go.sources, `${q}.sources`).map((s, j) => {
                  const r = `${q}.sources[${String(j)}]`;
                  const so = object(s, r);
                  return {
                    source: parseBrief(so.source, `${r}.source`),
                    template: str(so, "template", r),
                    candidate_count: num(so, "candidate_count", r),
                    candidates: array(so.candidates, `${r}.candidates`).map((c, k) => parseBrief(c, `${r}.candidates[${String(k)}]`)),
                  };
                }),
              };
            }),
          },
    conflicts:
      o.conflicts === undefined
        ? []
        : array(o.conflicts, `${p}.conflicts`).map((c, i) => {
            const q = `${p}.conflicts[${String(i)}]`;
            const co = object(c, q);
            return {
              call: parseBrief(co.call, `${q}.call`),
              consumer: str(co, "consumer", q),
              declared: strings(co.declared, `${q}.declared`),
              target: parseBrief(co.target, `${q}.target`),
              target_scope: str(co, "target_scope", q),
            };
          }),
  };
}

/** ProjectProbe is what the shell learns at start: a project, or a
 * single-repository server (404 on /api/v0/project). */
export type ProjectProbe = { kind: "single" } | { kind: "project"; project: Project };

/** ProjectFetcher requests /api/v0/project; tests pass their own. */
export type ProjectFetcher = (options: { signal?: AbortSignal }) => Promise<ProjectProbe>;

/** fetchProject asks the server whether it serves a project. 404 means a
 * single-repository server; any other failure is an error for the caller. */
export const fetchProject: ProjectFetcher = async (options) => {
  let body: unknown;
  try {
    body = await fetchJSON(PROJECT_PATH, options);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return { kind: "single" };
    throw err;
  }
  try {
    return { kind: "project", project: parseProject(body) };
  } catch (err) {
    if (err instanceof ApiFormatError) throw new ApiError("format", err.message);
    throw err;
  }
};

// The `project` part of /api/v0/api/endpoint and /api/v0/api/flow.

/** PassBrief is the link pass a set of links comes from. */
export interface PassBrief {
  state: string;
  pass: number;
  stale_reasons: string[];
}

/** ProjectLink is one cross-repository link into a route: a call (or an
 * OpenAPI operation) of another member. node is null when that member no
 * longer stores the source (a stale link). */
export interface ProjectLink {
  repository: string;
  identity: string;
  node: PresentedNode | null;
  owner: { id: string; kind: string; qualified_name: string } | null;
  kind: string;
  provenance: string;
  confidence: number;
  assumption: string;
  stale: boolean;
  stale_reasons: string[];
  /** A calls_route link's comparison of what the call sends with what
   * the route reads (T029 W4); null when nothing was compared. */
  shape_differences: ShapeDifferences | null;
  /** How the server built node.ide: the member's editor precedence (the
   * browser's editor=, the member's graphvein.yaml, the built-in
   * default). null from a server that does not send it, or when node is
   * not presented through a member's checkout. */
  ide_status: IdeStatus | null;
}

export interface ProjectEndpoint {
  project: string;
  link_pass: PassBrief;
  callers: ProjectLink[];
  operations: ProjectLink[];
  /** T022 review: calls of other members the pass left ambiguous with
   * this route among the candidates, or incomplete with this repository
   * among the candidate providers. "May be called by", never callers. */
  possible_callers: PossibleCaller[];
  /** T022 review: in-repository callers whose link a scope declaration
   * contradicts. */
  declaration_conflicts: CallConflict[];
}

export interface PossibleCaller {
  repository: string;
  identity: string;
  kind: string;
  row: string;
  status: string;
  wording: string;
  reason: string;
  candidate_count: number;
  candidate_repositories: string[];
}

export interface CallConflict {
  call: string;
  consumer: string;
  declared: string[];
  target: string;
  target_scope: string;
}

function parseConflict(value: unknown, path: string): CallConflict {
  const o = object(value, path);
  return {
    call: str(o, "call", path),
    consumer: str(o, "consumer", path),
    declared: strings(o.declared, `${path}.declared`),
    target: str(o, "target", path),
    target_scope: str(o, "target_scope", path),
  };
}

function parsePass(value: unknown, path: string): PassBrief {
  const o = object(value, path);
  return { state: str(o, "state", path), pass: num(o, "pass", path), stale_reasons: strings(o.stale_reasons, `${path}.stale_reasons`) };
}

function parseLink(value: unknown, path: string): ProjectLink {
  const o = object(value, path);
  const owner = o.owner === null || o.owner === undefined ? null : object(o.owner, `${path}.owner`);
  return {
    repository: str(o, "repository", path),
    identity: str(o, "identity", path),
    node: o.node === null ? null : parseNode(o.node, `${path}.node`),
    owner: owner === null ? null : { id: str(owner, "id", `${path}.owner`), kind: str(owner, "kind", `${path}.owner`), qualified_name: str(owner, "qualified_name", `${path}.owner`) },
    kind: str(o, "kind", path),
    provenance: str(o, "provenance", path),
    confidence: num(o, "confidence", path),
    assumption: str(o, "assumption", path),
    stale: bool(o, "stale", path),
    stale_reasons: strings(o.stale_reasons, `${path}.stale_reasons`),
    shape_differences: parseShapeDifferences(o.shape_differences, `${path}.shape_differences`),
    ide_status: o.ide_status === undefined || o.ide_status === null ? null : parseIdeStatus(o.ide_status, `${path}.ide_status`),
  };
}

/** parseProjectEndpoint reads the optional `project` member of an endpoint
 * or flow answer: null on a single-repository server (absent). */
export function parseProjectEndpoint(value: unknown, path: string): ProjectEndpoint | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    project: str(o, "project", path),
    link_pass: parsePass(o.link_pass, `${path}.link_pass`),
    callers: array(o.callers, `${path}.callers`).map((c, i) => parseLink(c, `${path}.callers[${String(i)}]`)),
    operations: array(o.operations, `${path}.operations`).map((c, i) => parseLink(c, `${path}.operations[${String(i)}]`)),
    possible_callers: array(o.possible_callers, `${path}.possible_callers`).map((c, i) => {
      const q = `${path}.possible_callers[${String(i)}]`;
      const co = object(c, q);
      return {
        repository: str(co, "repository", q),
        identity: str(co, "identity", q),
        kind: str(co, "kind", q),
        row: str(co, "row", q),
        status: str(co, "status", q),
        wording: str(co, "wording", q),
        reason: str(co, "reason", q),
        candidate_count: num(co, "candidate_count", q),
        candidate_repositories: strings(co.candidate_repositories, `${q}.candidate_repositories`),
      };
    }),
    declaration_conflicts: array(o.declaration_conflicts, `${path}.declaration_conflicts`).map((c, i) => parseConflict(c, `${path}.declaration_conflicts[${String(i)}]`)),
  };
}

// The `project` part of a call in /api/v0/api/calls.

export interface CallLink {
  target: NodeBrief;
  kind: string;
  provenance: string;
  confidence: number;
  assumption: string;
  stale: boolean;
  stale_reasons: string[];
}

export interface CallOutcome {
  template: string;
  row: string;
  status: string;
  reason: string;
  candidate_count: number;
  candidates: { repository: string; identity: string }[];
}

export interface CallProject {
  link_pass: PassBrief;
  links: CallLink[];
  outcomes: CallOutcome[];
  /** T022 review: set when the call's in-repository link contradicts a
   * scope declaration. */
  declaration_conflict: CallConflict | null;
}

/** parseCallProject reads a call's optional `project` member. */
export function parseCallProject(value: unknown, path: string): CallProject | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    link_pass: parsePass(o.link_pass, `${path}.link_pass`),
    links: array(o.links, `${path}.links`).map((l, i) => {
      const q = `${path}.links[${String(i)}]`;
      const lo = object(l, q);
      return {
        target: parseBrief(lo.target, `${q}.target`),
        kind: str(lo, "kind", q),
        provenance: str(lo, "provenance", q),
        confidence: num(lo, "confidence", q),
        assumption: str(lo, "assumption", q),
        stale: bool(lo, "stale", q),
        stale_reasons: strings(lo.stale_reasons, `${q}.stale_reasons`),
      };
    }),
    outcomes: array(o.outcomes, `${path}.outcomes`).map((c, i) => {
      const q = `${path}.outcomes[${String(i)}]`;
      const co = object(c, q);
      return {
        template: str(co, "template", q),
        row: str(co, "row", q),
        status: str(co, "status", q),
        reason: str(co, "reason", q),
        candidate_count: num(co, "candidate_count", q),
        candidates: array(co.candidates, `${q}.candidates`).map((x, k) => {
          const r = `${q}.candidates[${String(k)}]`;
          const xo = object(x, r);
          return { repository: str(xo, "repository", r), identity: str(xo, "identity", r) };
        }),
      };
    }),
    declaration_conflict:
      o.declaration_conflict === undefined || o.declaration_conflict === null ? null : parseConflict(o.declaration_conflict, `${path}.declaration_conflict`),
  };
}
