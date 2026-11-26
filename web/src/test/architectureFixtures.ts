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
