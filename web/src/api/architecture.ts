// Types, hand-written guards and requests of the architecture endpoints
// (docs/cli/serve.md "Architecture endpoints", ADR 0079, T026):
//   GET /api/v0/architecture?scope=
//   GET /api/v0/architecture/component?id=&editor=
// Components, links and annotation cells are computed by the server from
// stored facts; the UI draws exactly what it receives. Kinds, families,
// states and rows stay raw strings: a value this UI does not know is kept
// and shown raw, never mapped to a known one. Fields that later steps add
// (deploy markers, auth and TLS rows) are optional here: an absent value
// reads as unknown, never as "none".

import {
  ApiFormatError,
  array,
  getJSON,
  object,
  parseIndex,
  parseNode,
  str,
  strOrNull,
  version,
  type IndexRun,
  type JSONObject,
  type PresentedNode,
  type Signal,
} from "@/api/endpoints";

/** ArchScope is the request scope: production leaves out test-scope edges
 * (counted in hidden.test_edges); all includes them. */
export type ArchScope = "production" | "all";

/** Cell is one annotation cell (protocol, auth, TLS, engine). state is
 * proven exactly when source is a proven source. */
export interface Cell {
  value: string;
  state: string;
  source: string;
  rows: string[];
  /** Per value counts when the members differ (state mixed). */
  counts: Record<string, number> | null;
}

export interface Manifest {
  file: string;
  package: string | null;
  ecosystem: string | null;
  name: string | null;
  unread: string | null;
}

export interface Marker {
  kind: string;
  label: string;
  file: string | null;
  row: string | null;
  /** CD4: Compose's default merge or the assumption that two compose
   * files are combined (also appended to label). */
  basis: string | null;
}

/** Ingress is an AT2 ingress: a Traefik router with tls=true of a compose
 * service mapped to the component, "as configured", never the link's TLS. */
export interface Ingress {
  router: string;
  service: string;
  file: string;
  row: string;
  text: string;
  basis: string | null;
  rule: string | null;
  entrypoints: string | null;
  certResolver: string | null;
}

export interface ComponentCounts {
  files: number;
  test_files: number;
  manifests: number;
  routes: number;
  client_calls: number;
  ipc_commands: number;
  ipc_calls: number;
  /** Calls with status wrapper (a wrapper's forwarding call): neither
   * linked nor in the ghost; their requests are the wrapper's call sites. */
  wrapper_calls: number;
  tables: number;
}

export interface Component {
  id: string;
  directory: string;
  name: string;
  state: string;
  reason: string | null;
  manifests: Manifest[];
  kinds: string[];
  frameworks: string[];
  visibility: string;
  hidden_by: string | null;
  rows: string[];
  markers: Marker[];
  ingress: Ingress[];
  counts: ComponentCounts;
}

/** LinkAssumption is one assumption a stored calls_route link rests on
 * (T033): an origin nothing proves to be the server (id
 * "origin", row LF-1) or an unknown-prefix row assumption (UP-V1, UP-V2). */
export interface LinkAssumption {
  id: string;
  text: string;
}

/** parseLinkAssumptions reads an optional list of link assumptions. */
export function parseLinkAssumptions(value: unknown, path: string): LinkAssumption[] {
  if (value === undefined || value === null) return [];
  return array(value, path).map((a, i) => {
    const ap = `${path}[${String(i)}]`;
    const ao = object(a, ap);
    return { id: str(ao, "id", ap), text: str(ao, "text", ap) };
  });
}

export interface LinkMember {
  source: string;
  target: string;
  method: string | null;
  path: string | null;
  /** The route's path as served (router prefixes, FastAPI's trailing
   * slash); null when the route has none or the member is not HTTP. */
  full_path: string | null;
  /** T033 UP-33: the route's path whose prefix the code does not fix,
   * "…" for the prefix; set only when full_path is null. */
  prefix_path: string | null;
  /** The assumptions the member's link rests on (origin, UP-V1, UP-V2). */
  assumptions: LinkAssumption[];
  command: string | null;
  tier: string;
  auth: string | null;
  /** CE8 (serve --project): the other member whose call (source) or
   * route (target) this member names; null otherwise. */
  source_repository: string | null;
  target_repository: string | null;
}

export interface Origin {
  origin: string;
  text: string;
}

export interface Link {
  id: string;
  family: string;
  row: string;
  source: string;
  target: string;
  count: number;
  back_count: number;
  by_kind: Record<string, number>;
  tiers: Record<string, number>;
  weakest: string | null;
  methods: string[];
  members: LinkMember[];
  origins: Origin[];
  cells: { protocol: Cell; auth: Cell; tls: Cell };
  /** CE8 (serve --project): the other member, the stored assumption
   * texts, row assumptions and stale reasons of its project links. */
  repository: string | null;
  assumptions: string[];
  assumes: string[];
  stale_reasons: string[];
}

export interface Accessor {
  component: string;
  link: string | null;
  reads: number;
  writes: number;
  /** Table edges of a query builder's entry: "builds a query on". */
  builds: number;
  may_also_use: number;
}

export interface Database {
  id: string;
  set: string;
  tool: string;
  tables: number;
  engine: Cell;
  evidence: string[];
  accessors: Accessor[];
}

export interface Stub {
  component: string;
  row: string;
  packages: number;
  text: string;
  protocol: Cell;
}

export interface Ghost {
  component: string;
  family: string;
  row: string;
  total: number;
  by_status: Record<string, number>;
}

export interface DocumentsEntry {
  source: string;
  target: string;
  row: string;
  by_kind: Record<string, number>;
}

export interface Hidden {
  test_components: number;
  docs_components: number;
  configured_components: number;
  test_edges: Record<string, number>;
}

export interface NotProven {
  kind: string;
  row: string;
  component: string | null;
  text: string;
  count: number | null;
  /** service_not_mapped: the compose service, its file and the reason. */
  service: string | null;
  file: string | null;
  reason: string | null;
  /** may_also_use: the database card; service_not_mapped: the card AE2
   * ties the service's database image to. */
  database: string | null;
}

export interface Libraries {
  components: string[];
  links_in: number;
  links_out: number;
}

/** RemoteMember is the muted card of another project member (CE8). */
export interface RemoteMember {
  id: string;
  repository: string;
  name: string;
  row: string;
  calls_out: number;
  calls_in: number;
  /** This repository's OpenAPI operations a stored documents_route
   * project link connects to the member's routes (CE8 R1); 0 from a
   * server that does not send it. */
  operations_out: number;
  /** The member's operations documenting this repository's routes. */
  operations_in: number;
}

/** Across is one component's calls linked across repositories, beside
 * the repository's own count of calls without a link. */
export interface Across {
  component: string;
  linked_across: number;
  repository_not_linked: number;
  /** OpenAPI operations a stored project link connects to another
   * member's route, beside the repository's own "documented, no route". */
  operations_across: number;
  repository_no_route: number;
  repositories: string[];
}

/** ArchProject is the link pass's view of a member (serve --project);
 * null on a single-repository server. */
export interface ArchProject {
  project: string;
  linkPass: { state: string; pass: number | null; staleReasons: string[] };
  members: RemoteMember[];
  across: Across[];
  assumptions: string[];
}

/** ArchitectureResponse is GET /api/v0/architecture. */
export interface ArchitectureResponse {
  repository: string;
  index: IndexRun | null;
  scope: string;
  manifests: string;
  notice: string | null;
  /** Files the run could not parse (T074); 0 when the server omits it. */
  parse_failed: number;
  components: Component[];
  links: Link[];
  databases: Database[];
  stubs: Stub[];
  ghosts: Ghost[];
  documents: DocumentsEntry[];
  hidden: Hidden;
  not_drawn: Record<string, number>;
  not_proven: NotProven[];
  libraries: Libraries;
  project: ArchProject | null;
}

export interface ExternalPackage {
  identity: string;
  ecosystem: string;
  name: string;
  imports: number;
}

/** ComponentResponse is GET /api/v0/architecture/component. */
export interface ComponentResponse {
  repository: string;
  index: IndexRun | null;
  component: Component;
  files: { path: string; test: boolean }[];
  routes: PresentedNode[];
  calls: PresentedNode[];
  commands: PresentedNode[];
  tables: PresentedNode[];
  unlinked: Record<string, number>;
  external_packages: ExternalPackage[];
}

// Small readers. Optional fields read as their "unknown" value when absent.

function optStr(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

function numAt(obj: JSONObject, key: string, path: string, fallback?: number): number {
  const v = obj[key];
  if (v === undefined && fallback !== undefined) return fallback;
  if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${key} is not a number`);
  return v;
}

function optNum(obj: JSONObject, key: string, path: string): number | null {
  const v = obj[key];
  if (v === undefined || v === null) return null;
  if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${key} is not a number`);
  return v;
}

function strList(value: unknown, path: string): string[] {
  if (value === undefined || value === null) return [];
  return array(value, path).map((v, i) => {
    if (typeof v !== "string") throw new ApiFormatError(`${path}[${String(i)}] is not a string`);
    return v;
  });
}

function countMap(value: unknown, path: string): Record<string, number> {
  if (value === undefined || value === null) return {};
  const obj = object(value, path);
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(obj)) {
    if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${k} is not a number`);
    out[k] = v;
  }
  return out;
}

/** UNKNOWN_CELL stands in for a cell the server did not send: unknown,
 * never a value. */
export function unknownCell(what: string): Cell {
  return { value: `${what} ?`, state: "unknown", source: "unknown: not sent by this server", rows: [], counts: null };
}

function parseCell(value: unknown, path: string, what: string): Cell {
  if (value === undefined || value === null) return unknownCell(what);
  const obj = object(value, path);
  return {
    value: str(obj, "value", path),
    state: str(obj, "state", path),
    source: str(obj, "source", path),
    rows: strList(obj.rows, `${path}.rows`),
    counts: obj.counts === undefined || obj.counts === null ? null : countMap(obj.counts, `${path}.counts`),
  };
}

function parseComponent(value: unknown, path: string): Component {
  const obj = object(value, path);
  const c = obj.counts === undefined ? {} : object(obj.counts, `${path}.counts`);
  const cp = `${path}.counts`;
  return {
    id: str(obj, "id", path),
    directory: str(obj, "directory", path),
    name: str(obj, "name", path),
    state: str(obj, "state", path),
    reason: optStr(obj, "reason", path),
    manifests: (obj.manifests === undefined || obj.manifests === null ? [] : array(obj.manifests, `${path}.manifests`)).map((m, i) => {
      const mp = `${path}.manifests[${String(i)}]`;
      const mo = object(m, mp);
      return {
        file: str(mo, "file", mp),
        package: optStr(mo, "package", mp),
        ecosystem: optStr(mo, "ecosystem", mp),
        name: optStr(mo, "name", mp),
        unread: optStr(mo, "unread", mp),
      };
    }),
    kinds: strList(obj.kinds, `${path}.kinds`),
    frameworks: strList(obj.frameworks, `${path}.frameworks`),
    visibility: str(obj, "visibility", path),
    hidden_by: optStr(obj, "hidden_by", path),
    rows: strList(obj.rows, `${path}.rows`),
    markers: (obj.markers === undefined || obj.markers === null ? [] : array(obj.markers, `${path}.markers`)).map((m, i) => {
      const mp = `${path}.markers[${String(i)}]`;
      const mo = object(m, mp);
      return { kind: str(mo, "kind", mp), label: str(mo, "label", mp), file: optStr(mo, "file", mp), row: optStr(mo, "row", mp), basis: optStr(mo, "basis", mp) };
    }),
    ingress: (obj.ingress === undefined || obj.ingress === null ? [] : array(obj.ingress, `${path}.ingress`)).map((g, i) => {
      const gp = `${path}.ingress[${String(i)}]`;
      const go = object(g, gp);
      const w: JSONObject = go.as_written === undefined || go.as_written === null ? {} : object(go.as_written, `${gp}.as_written`);
      return {
        router: str(go, "router", gp),
        service: optStr(go, "service", gp) ?? "",
        file: optStr(go, "file", gp) ?? "",
        row: optStr(go, "row", gp) ?? "AT2",
        text: str(go, "text", gp),
        basis: optStr(go, "basis", gp),
        rule: optStr(w, "rule", `${gp}.as_written`),
        entrypoints: optStr(w, "entrypoints", `${gp}.as_written`),
        certResolver: optStr(w, "cert_resolver", `${gp}.as_written`),
      };
    }),
    counts: {
      files: numAt(c, "files", cp, 0),
      test_files: numAt(c, "test_files", cp, 0),
      manifests: numAt(c, "manifests", cp, 0),
      routes: numAt(c, "routes", cp, 0),
      client_calls: numAt(c, "client_calls", cp, 0),
      ipc_commands: numAt(c, "ipc_commands", cp, 0),
      ipc_calls: numAt(c, "ipc_calls", cp, 0),
      wrapper_calls: numAt(c, "wrapper_calls", cp, 0),
      tables: numAt(c, "tables", cp, 0),
    },
  };
}

function parseLink(value: unknown, path: string): Link {
  const obj = object(value, path);
  const cells = obj.cells === undefined || obj.cells === null ? {} : object(obj.cells, `${path}.cells`);
  return {
    id: str(obj, "id", path),
    family: str(obj, "family", path),
    row: str(obj, "row", path),
    source: str(obj, "source", path),
    target: str(obj, "target", path),
    count: numAt(obj, "count", path),
    back_count: numAt(obj, "back_count", path, 0),
    by_kind: countMap(obj.by_kind, `${path}.by_kind`),
    tiers: countMap(obj.tiers, `${path}.tiers`),
    weakest: optStr(obj, "weakest", path),
    methods: strList(obj.methods, `${path}.methods`),
    members: (obj.members === undefined || obj.members === null ? [] : array(obj.members, `${path}.members`)).map((m, i) => {
      const mp = `${path}.members[${String(i)}]`;
      const mo = object(m, mp);
      return {
        source: str(mo, "source", mp),
        target: str(mo, "target", mp),
        method: optStr(mo, "method", mp),
        path: optStr(mo, "path", mp),
        full_path: optStr(mo, "full_path", mp),
        prefix_path: optStr(mo, "prefix_path", mp),
        assumptions: parseLinkAssumptions(mo.assumptions, `${mp}.assumptions`),
        command: optStr(mo, "command", mp),
        tier: str(mo, "tier", mp),
        auth: optStr(mo, "auth", mp),
        source_repository: optStr(mo, "source_repository", mp),
        target_repository: optStr(mo, "target_repository", mp),
      };
    }),
    origins: (obj.origins === undefined || obj.origins === null ? [] : array(obj.origins, `${path}.origins`)).map((o, i) => {
      const op = `${path}.origins[${String(i)}]`;
      if (typeof o === "string") return { origin: o, text: o };
      const oo = object(o, op);
      const origin = optStr(oo, "origin", op) ?? optStr(oo, "url", op) ?? "";
      return { origin, text: optStr(oo, "text", op) ?? origin };
    }),
    cells: {
      protocol: parseCell(cells.protocol, `${path}.cells.protocol`, "protocol"),
      auth: parseCell(cells.auth, `${path}.cells.auth`, "auth"),
      tls: parseCell(cells.tls, `${path}.cells.tls`, "TLS"),
    },
    repository: optStr(obj, "repository", path),
    assumptions: strList(obj.assumptions, `${path}.assumptions`),
    assumes: strList(obj.assumes, `${path}.assumes`),
    stale_reasons: strList(obj.stale_reasons, `${path}.stale_reasons`),
  };
}

function parseProject(value: unknown, path: string): ArchProject | null {
  if (value === undefined || value === null) return null;
  const obj = object(value, path);
  const pass = obj.link_pass === undefined || obj.link_pass === null ? {} : object(obj.link_pass, `${path}.link_pass`);
  const list = (key: string) => (obj[key] === undefined || obj[key] === null ? [] : array(obj[key], `${path}.${key}`));
  return {
    project: str(obj, "project", path),
    linkPass: { state: optStr(pass, "state", `${path}.link_pass`) ?? "none", pass: optNum(pass, "pass", `${path}.link_pass`), staleReasons: strList(pass.stale_reasons, `${path}.link_pass.stale_reasons`) },
    members: list("members").map((m, i) => {
      const mp = `${path}.members[${String(i)}]`;
      const o = object(m, mp);
      return { id: str(o, "id", mp), repository: str(o, "repository", mp), name: str(o, "name", mp), row: optStr(o, "row", mp) ?? "CE8", calls_out: numAt(o, "calls_out", mp, 0), calls_in: numAt(o, "calls_in", mp, 0), operations_out: numAt(o, "operations_out", mp, 0), operations_in: numAt(o, "operations_in", mp, 0) };
    }),
    across: list("across").map((a, i) => {
      const ap = `${path}.across[${String(i)}]`;
      const o = object(a, ap);
      return { component: str(o, "component", ap), linked_across: numAt(o, "linked_across", ap, 0), repository_not_linked: numAt(o, "repository_not_linked", ap, 0), operations_across: numAt(o, "operations_across", ap, 0), repository_no_route: numAt(o, "repository_no_route", ap, 0), repositories: strList(o.repositories, `${ap}.repositories`) };
    }),
    assumptions: strList(obj.assumptions, `${path}.assumptions`),
  };
}

/** parseArchitecture validates GET /api/v0/architecture. */
export function parseArchitecture(value: unknown): ArchitectureResponse {
  const obj = object(value, "architecture");
  version(obj, "architecture");
  const p = "architecture";
  const hidden = obj.hidden === undefined || obj.hidden === null ? {} : object(obj.hidden, `${p}.hidden`);
  const libs = obj.libraries === undefined || obj.libraries === null ? {} : object(obj.libraries, `${p}.libraries`);
  const list = (key: string) => (obj[key] === undefined || obj[key] === null ? [] : array(obj[key], `${p}.${key}`));
  return {
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    scope: optStr(obj, "scope", p) ?? "production",
    manifests: optStr(obj, "manifests", p) ?? "read",
    notice: optStr(obj, "notice", p),
    parse_failed: numAt(obj, "parse_failed", p, 0),
    components: list("components").map((c, i) => parseComponent(c, `${p}.components[${String(i)}]`)),
    links: list("links").map((l, i) => parseLink(l, `${p}.links[${String(i)}]`)),
    databases: list("databases").map((d, i) => {
      const dp = `${p}.databases[${String(i)}]`;
      const o = object(d, dp);
      return {
        id: str(o, "id", dp),
        set: str(o, "set", dp),
        tool: str(o, "tool", dp),
        tables: numAt(o, "tables", dp, 0),
        engine: parseCell(o.engine, `${dp}.engine`, "engine"),
        evidence: strList(o.evidence, `${dp}.evidence`),
        accessors: (o.accessors === undefined || o.accessors === null ? [] : array(o.accessors, `${dp}.accessors`)).map((a, j) => {
          const ap = `${dp}.accessors[${String(j)}]`;
          const ao = object(a, ap);
          return {
            component: str(ao, "component", ap),
            link: optStr(ao, "link", ap),
            reads: numAt(ao, "reads", ap, 0),
            writes: numAt(ao, "writes", ap, 0),
            builds: numAt(ao, "builds", ap, 0),
            may_also_use: numAt(ao, "may_also_use", ap, 0),
          };
        }),
      };
    }),
    stubs: list("stubs").map((s, i) => {
      const sp = `${p}.stubs[${String(i)}]`;
      const o = object(s, sp);
      return {
        component: str(o, "component", sp),
        row: optStr(o, "row", sp) ?? "CE6",
        packages: numAt(o, "packages", sp, 0),
        text: str(o, "text", sp),
        protocol: parseCell(o.protocol, `${sp}.protocol`, "protocol"),
      };
    }),
    ghosts: list("ghosts").map((g, i) => {
      const gp = `${p}.ghosts[${String(i)}]`;
      const o = object(g, gp);
      return {
        component: str(o, "component", gp),
        family: str(o, "family", gp),
        row: optStr(o, "row", gp) ?? "",
        total: numAt(o, "total", gp),
        by_status: countMap(o.by_status, `${gp}.by_status`),
      };
    }),
    documents: list("documents").map((d, i) => {
      const dp = `${p}.documents[${String(i)}]`;
      const o = object(d, dp);
      return { source: str(o, "source", dp), target: str(o, "target", dp), row: optStr(o, "row", dp) ?? "CE5", by_kind: countMap(o.by_kind, `${dp}.by_kind`) };
    }),
    hidden: {
      test_components: numAt(hidden, "test_components", `${p}.hidden`, 0),
      docs_components: numAt(hidden, "docs_components", `${p}.hidden`, 0),
      configured_components: numAt(hidden, "configured_components", `${p}.hidden`, 0),
      test_edges: countMap(hidden.test_edges, `${p}.hidden.test_edges`),
    },
    not_drawn: countMap(obj.not_drawn, `${p}.not_drawn`),
    not_proven: list("not_proven").map((n, i) => {
      const np = `${p}.not_proven[${String(i)}]`;
      const o = object(n, np);
      return {
        kind: str(o, "kind", np),
        row: optStr(o, "row", np) ?? "",
        component: optStr(o, "component", np),
        text: str(o, "text", np),
        count: optNum(o, "count", np),
        service: optStr(o, "service", np),
        file: optStr(o, "file", np),
        reason: optStr(o, "reason", np),
        database: optStr(o, "database", np),
      };
    }),
    libraries: {
      components: strList(libs.components, `${p}.libraries.components`),
      links_in: numAt(libs, "links_in", `${p}.libraries`, 0),
      links_out: numAt(libs, "links_out", `${p}.libraries`, 0),
    },
    project: parseProject(obj.project, `${p}.project`),
  };
}

/** parseComponentDetail validates GET /api/v0/architecture/component. */
export function parseComponentDetail(value: unknown): ComponentResponse {
  const p = "component";
  const obj = object(value, p);
  version(obj, p);
  const nodes = (key: string) => (obj[key] === undefined || obj[key] === null ? [] : array(obj[key], `${p}.${key}`)).map((n, i) => parseNode(n, `${p}.${key}[${String(i)}]`));
  return {
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    component: parseComponent(obj.component, `${p}.component`),
    files: (obj.files === undefined || obj.files === null ? [] : array(obj.files, `${p}.files`)).map((f, i) => {
      const fp = `${p}.files[${String(i)}]`;
      const o = object(f, fp);
      return { path: str(o, "path", fp), test: o.test === true };
    }),
    routes: nodes("routes"),
    calls: nodes("calls"),
    commands: nodes("commands"),
    tables: nodes("tables"),
    unlinked: countMap(obj.unlinked, `${p}.unlinked`),
    external_packages: (obj.external_packages === undefined || obj.external_packages === null ? [] : array(obj.external_packages, `${p}.external_packages`)).map((e, i) => {
      const ep = `${p}.external_packages[${String(i)}]`;
      const o = object(e, ep);
      return { identity: str(o, "identity", ep), ecosystem: str(o, "ecosystem", ep), name: str(o, "name", ep), imports: numAt(o, "imports", ep, 0) };
    }),
  };
}

/** ChainDirection is the chain request's direction (default both). */
export type ChainDirection = "up" | "down" | "both";

/** ChainMember is one member of a chain step (rows CH1-CH9). */
export interface ChainMember {
  id: string;
  kind: string;
  name: string;
  qualified_name: string;
  file: string | null;
  line: number | null;
  /** The component directory of its file, or of a component member. */
  component: string | null;
  /** A table's database card ("database:<set>"). */
  database: string | null;
  /** CH13 (serve --project): the other member this node belongs to; it
   * is drawn in that member's muted card. */
  repository: string | null;
  method: string | null;
  /** A route's pattern as written; full_path is its served path. */
  path: string | null;
  full_path: string | null;
  /** T033 UP-33: a route's path whose prefix the code does not fix ("…"
   * for the prefix); set only when full_path is null. */
  prefix_path: string | null;
  command: string | null;
  status: string | null;
  /** The CH row of the edge that first reached it ("" for the start). */
  row: string;
  /** The weakest edge on its first path and the step of that edge. */
  tier: string;
  tier_step: number;
  /** Members of the previous step with an edge to it (all branches). */
  parents: string[];
}

export interface ChainBound {
  cap: string;
  step: number;
  beyond: number;
  text: string;
}

export interface ChainStep {
  step: number;
  members: ChainMember[];
  hidden_test: number;
  bounds: ChainBound[];
  /** The map budget's hint: members drawn, the rest folded. */
  map: { shown: string[]; folded: number };
}

export interface ChainEvidence {
  file: string;
  line: number | null;
}

/** ChainEdge is one followed stored edge, from the member nearer the
 * start. */
export interface ChainEdge {
  from: string;
  to: string;
  kind: string;
  row: string;
  wording: string;
  dashed: boolean;
  tier: string;
  count: number;
  evidence: ChainEvidence[];
  /** A CH3 (calls_route) edge's link assumptions (origin, UP-V1, UP-V2). */
  assumptions: LinkAssumption[];
}

export interface ChainEnd {
  member: string;
  row: string;
  text: string;
}

export interface ChainSide {
  steps: ChainStep[];
  edges: ChainEdge[];
  ends: ChainEnd[];
  not_followed: Record<string, number>;
  caps: string[];
  members: number;
  hidden_test: number;
}

/** ChainResponse is GET /api/v0/architecture/chain. */
export interface ChainResponse {
  repository: string;
  index: IndexRun | null;
  start: { id: string; kind: string; level: string; row: string | null };
  direction: string;
  scope: string;
  /** depth is the walked depth; null when the server did not send it
   * (unknown, never a guessed default). */
  depth: number | null;
  /** caps.depth is the largest depth the server accepts; null when not
   * sent (unknown). */
  caps: { depth: number | null; members: number; fan_out: number };
  up: ChainSide | null;
  down: ChainSide | null;
  map: { steps: number; per_step: number; total: number; shown: number; folded: number; open_in_flow: boolean };
}

function optInt(obj: JSONObject, key: string, path: string): number | null {
  return optNum(obj, key, path);
}

function parseChainSide(value: unknown, path: string): ChainSide | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  const list = (key: string) => (o[key] === undefined || o[key] === null ? [] : array(o[key], `${path}.${key}`));
  return {
    steps: list("steps").map((st, i) => {
      const sp = `${path}.steps[${String(i)}]`;
      const so = object(st, sp);
      const mp = so.map === undefined || so.map === null ? {} : object(so.map, `${sp}.map`);
      return {
        step: numAt(so, "step", sp),
        members: (so.members === undefined || so.members === null ? [] : array(so.members, `${sp}.members`)).map((m, j) => {
          const p = `${sp}.members[${String(j)}]`;
          const mo = object(m, p);
          return {
            id: str(mo, "id", p),
            kind: str(mo, "kind", p),
            name: str(mo, "name", p),
            qualified_name: optStr(mo, "qualified_name", p) ?? str(mo, "name", p),
            file: optStr(mo, "file", p),
            line: optInt(mo, "line", p),
            component: optStr(mo, "component", p),
            database: optStr(mo, "database", p),
            repository: optStr(mo, "repository", p),
            method: optStr(mo, "method", p),
            path: optStr(mo, "path", p),
            full_path: optStr(mo, "full_path", p),
            prefix_path: optStr(mo, "prefix_path", p),
            command: optStr(mo, "command", p),
            status: optStr(mo, "status", p),
            row: optStr(mo, "row", p) ?? "",
            tier: optStr(mo, "tier", p) ?? "fact",
            tier_step: numAt(mo, "tier_step", p, 0),
            parents: strList(mo.parents, `${p}.parents`),
          };
        }),
        hidden_test: numAt(so, "hidden_test", sp, 0),
        bounds: (so.bounds === undefined || so.bounds === null ? [] : array(so.bounds, `${sp}.bounds`)).map((b, j) => {
          const bp = `${sp}.bounds[${String(j)}]`;
          const bo = object(b, bp);
          return { cap: str(bo, "cap", bp), step: numAt(bo, "step", bp, 0), beyond: numAt(bo, "beyond", bp, 0), text: str(bo, "text", bp) };
        }),
        map: { shown: strList(mp.shown, `${sp}.map.shown`), folded: numAt(mp, "folded", `${sp}.map`, 0) },
      };
    }),
    edges: list("edges").map((e, i) => {
      const ep = `${path}.edges[${String(i)}]`;
      const eo = object(e, ep);
      return {
        from: str(eo, "from", ep),
        to: str(eo, "to", ep),
        kind: str(eo, "kind", ep),
        row: optStr(eo, "row", ep) ?? "",
        wording: optStr(eo, "wording", ep) ?? str(eo, "kind", ep),
        dashed: eo.dashed === true,
        tier: optStr(eo, "tier", ep) ?? "fact",
        count: numAt(eo, "count", ep, 1),
        assumptions: parseLinkAssumptions(eo.assumptions, `${ep}.assumptions`),
        evidence: (eo.evidence === undefined || eo.evidence === null ? [] : array(eo.evidence, `${ep}.evidence`)).map((v, j) => {
          const vp = `${ep}.evidence[${String(j)}]`;
          const vo = object(v, vp);
          const span = vo.span === undefined || vo.span === null ? {} : object(vo.span, `${vp}.span`);
          return { file: str(vo, "file", vp), line: optInt(span, "start_line", `${vp}.span`) };
        }),
      };
    }),
    ends: list("ends").map((e, i) => {
      const ep = `${path}.ends[${String(i)}]`;
      const eo = object(e, ep);
      return { member: str(eo, "member", ep), row: optStr(eo, "row", ep) ?? "CH11", text: str(eo, "text", ep) };
    }),
    not_followed: countMap(o.not_followed, `${path}.not_followed`),
    caps: strList(o.caps, `${path}.caps`),
    members: numAt(o, "members", path, 0),
    hidden_test: numAt(o, "hidden_test", path, 0),
  };
}

/** parseChain validates GET /api/v0/architecture/chain. */
export function parseChain(value: unknown): ChainResponse {
  const p = "chain";
  const obj = object(value, p);
  version(obj, p);
  const start = object(obj.start, `${p}.start`);
  const caps = obj.caps === undefined || obj.caps === null ? {} : object(obj.caps, `${p}.caps`);
  const map = obj.map === undefined || obj.map === null ? {} : object(obj.map, `${p}.map`);
  return {
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    start: { id: str(start, "id", `${p}.start`), kind: str(start, "kind", `${p}.start`), level: optStr(start, "level", `${p}.start`) ?? "node", row: optStr(start, "row", `${p}.start`) },
    direction: optStr(obj, "direction", p) ?? "both",
    scope: optStr(obj, "scope", p) ?? "production",
    depth: optNum(obj, "depth", p),
    caps: { depth: optNum(caps, "depth", `${p}.caps`), members: numAt(caps, "members", `${p}.caps`, 2000), fan_out: numAt(caps, "fan_out", `${p}.caps`, 200) },
    up: parseChainSide(obj.up, `${p}.up`),
    down: parseChainSide(obj.down, `${p}.down`),
    map: {
      steps: numAt(map, "steps", `${p}.map`, 3),
      per_step: numAt(map, "per_step", `${p}.map`, 5),
      total: numAt(map, "total", `${p}.map`, 40),
      shown: numAt(map, "shown", `${p}.map`, 0),
      folded: numAt(map, "folded", `${p}.map`, 0),
      open_in_flow: map.open_in_flow === true,
    },
  };
}

/** chainPath is the chain request; depth 1 to 10 (T042), omitted: the
 * server's default 6. */
export function chainPath(id: string, direction: ChainDirection, scope: ArchScope, depth?: number): string {
  return `/api/v0/architecture/chain?id=${encodeURIComponent(id)}&direction=${direction}&scope=${scope}${depth !== undefined ? `&depth=${String(depth)}` : ""}`;
}

export function architecturePath(scope: ArchScope): string {
  return `/api/v0/architecture?scope=${scope}`;
}

export function componentPath(id: string, editor?: string): string {
  return `/api/v0/architecture/component?id=${encodeURIComponent(id)}${editor !== undefined ? `&editor=${encodeURIComponent(editor)}` : ""}`;
}

/** ArchitectureClient is the section's requests; tests pass their own. */
export interface ArchitectureClient {
  overview(scope: ArchScope, options: Signal): Promise<ArchitectureResponse>;
  component(p: { id: string; editor?: string }, options: Signal): Promise<ComponentResponse>;
  chain(p: { id: string; direction: ChainDirection; scope: ArchScope; depth: number }, options: Signal): Promise<ChainResponse>;
}

export const httpArchitectureClient: ArchitectureClient = {
  overview: (scope, options) => getJSON(architecturePath(scope), parseArchitecture, options),
  component: (p, options) => getJSON(componentPath(p.id, p.editor), parseComponentDetail, options),
  chain: (p, options) => getJSON(chainPath(p.id, p.direction, p.scope, p.depth), parseChain, options),
};
