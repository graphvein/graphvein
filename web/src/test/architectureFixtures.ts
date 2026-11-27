// The Go server's architecture goldens (internal/server/testdata, T026
// AG-B1), imported so the Architecture section is tested against what the
// server really sends.
import componentApp from "../../../internal/server/testdata/architecture-component-app.golden.json";
import components from "../../../internal/server/testdata/architecture-components.golden.json";
import componentsAll from "../../../internal/server/testdata/architecture-components-all.golden.json";
import stale from "../../../internal/server/testdata/architecture-stale.golden.json";
import synthetic from "../../../internal/server/testdata/architecture-synthetic.golden.json";
import chainComponentFixture from "../../../internal/server/testdata/chain-component-fixture.golden.json";
import chainRoute from "../../../internal/server/testdata/chain-route.golden.json";
import chainTable from "../../../internal/server/testdata/chain-table.golden.json";
import project from "../../../internal/server/testdata/architecture-project.golden.json";
import bandAnswers from "./libraryBand.json";

import { ApiError } from "@/api/client";
import { parseArchitecture, parseChain, parseComponentDetail, type ArchitectureClient, type ArchitectureResponse, type ArchScope, type ChainResponse, type ComponentResponse } from "@/api/architecture";

/** CHAIN_GOLDENS are the server's chain goldens by start id (AG-B2). */
export const CHAIN_GOLDENS: Record<string, unknown> = {
  web: chainComponentFixture,
  "route:api/items.py#GET%20%2Fitems": chainRoute,
  "table:api/models.py#item": chainTable,
};

/** chainGolden parses a chain golden by its start id. */
export function chainGolden(id: string): ChainResponse {
  return parseChain(structuredClone(CHAIN_GOLDENS[id]));
}

export const ARCH_GOLDENS = { components, "components-all": componentsAll, stale, synthetic } as const;

export type ArchGoldenName = keyof typeof ARCH_GOLDENS;

/** archGolden parses a golden (a fresh copy each call). */
export function archGolden(name: ArchGoldenName): ArchitectureResponse {
  return parseArchitecture(structuredClone(ARCH_GOLDENS[name]));
}

/** PROJECT_GOLDENS are the project-mode answers of the projectimpact
 * fixture (TOP-2 of 2026-11-18, rows CE8 and CH13), by name. */
export type ProjectGoldenName = keyof typeof project;

/** projectArchGolden parses a project-mode overview golden. */
export function projectArchGolden(name: "frontend-overview" | "backend-overview"): ArchitectureResponse {
  return parseArchitecture(structuredClone(project[name]));
}

/** projectChainGolden parses a project-mode chain golden. */
export function projectChainGolden(name: "frontend-chain-call" | "backend-chain-route" | "frontend-chain-component"): ChainResponse {
  return parseChain(structuredClone(project[name]));
}

export function componentGolden(): ComponentResponse {
  return parseComponentDetail(structuredClone(componentApp));
}

/** fakeArchClient answers the overview from a golden (production) and its
 * "all" variant, and the component answer for crates/app; other ids are
 * 404. Requests are recorded. */
export function fakeArchClient(name: ArchGoldenName, requests: string[] = [], all: ArchGoldenName = name): ArchitectureClient {
  return {
    overview: (scope: ArchScope) => {
      requests.push(`overview ${scope}`);
      return Promise.resolve(archGolden(scope === "all" ? all : name));
    },
    component: (p) => {
      requests.push(`component ${p.id}`);
      if (p.id === "crates/app") return Promise.resolve(componentGolden());
      return Promise.reject(new ApiError("http", "not_found: no component", 404, "not_found"));
    },
    chain: (p) => {
      requests.push(`chain ${p.id} ${p.direction}`, `chain-depth ${p.id} ${String(p.depth)}`);
      if (CHAIN_GOLDENS[p.id] !== undefined) return Promise.resolve(chainGolden(p.id));
      return Promise.reject(new ApiError("http", "not_found: no node", 404, "not_found"));
    },
  };
}

/** BAND_ANSWERS are the pdfcraft (explore set) and owncast (corpus)
 * overviews of the T062 design round (extractors-63, production scope),
 * reduced to what the libraries band reads: components [id, name,
 * directory, visibility, kinds, files, SDK packages] and links [source,
 * target, family, count, back count]. Names and directories only. */
type BandData = { components: (string | number)[][]; links: (string | number)[][] };

/** bandAnswer builds a full overview answer from a reduced one, on the
 * components golden's shape (cells and counts of its first entries). */
export function bandAnswer(name: "pdfcraft" | "owncast"): ArchitectureResponse {
  const base = archGolden("components");
  const d = (bandAnswers as Record<string, BandData>)[name];
  const comp = base.components[0];
  const link = base.links.find((l) => l.family === "in_process") ?? base.links[0];
  const stub = base.stubs[0];
  if (d === undefined || comp === undefined || link === undefined || stub === undefined) throw new Error("band fixture");
  const components = d.components.map(([id, nm, dir, vis, kinds, files]) => ({
    ...structuredClone(comp),
    id: String(id),
    name: String(nm),
    directory: String(dir),
    visibility: String(vis),
    kinds: String(kinds).split(","),
    state: "read",
    reason: null,
    counts: { ...comp.counts, files: Number(files) },
  }));
  const links = d.links.map(([s, t, fam, n, back]) => ({ ...structuredClone(link), id: `${String(fam)}:${String(s)}->${String(t)}`, family: String(fam), source: String(s), target: String(t), count: Number(n), back_count: Number(back), members: [] }));
  const stubs = d.components.filter((c) => Number(c[6]) > 0).map((c) => ({ ...structuredClone(stub), component: String(c[0]), packages: Number(c[6]), text: `imports ${String(c[6])} external packages` }));
  const libs = components.filter((c) => c.visibility === "folded").map((c) => c.id);
  return { ...base, repository: name, components, links, stubs, ghosts: [], databases: [], not_proven: [], libraries: { ...base.libraries, components: libs } };
}
