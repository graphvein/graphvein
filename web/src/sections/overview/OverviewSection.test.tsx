// The Overview (T064): every part reads an answer another section already
// requests, says "none found in the indexed files" only where the answer
// proves it, and the label question is asked here.
import { render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { EndpointsResponse } from "@/api/endpoints";
import type { SchemaSummary } from "@/api/schema";
import type { UnreadCounts, UnreadFetcher } from "@/api/unread";
import type { RequestResult } from "@/hooks/useApiRequest";
import { LabelProvider } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { DEFAULT_SHOW, buildMap } from "@/sections/architecture/model";
import { OverviewSection, cardLines, hiddenRootLines, linksIntoLibraries, unreadLines } from "@/sections/overview/OverviewSection";
import { endpointsGolden, fakeClient } from "@/test/apiFixtures";
import { archGolden, bandAnswer, fakeArchClient } from "@/test/architectureFixtures";
import { goldenStatus } from "@/test/fixtures";
import { craftedSummary, summaryGolden } from "@/test/schemaFixtures";

afterEach(() => {
  window.localStorage.clear();
});

const OK: StatusView = { kind: "ok", status: goldenStatus("fresh") };

function ok<T>(data: T): RequestResult<T> {
  return { state: { kind: "ok", data }, retry: () => undefined };
}

function unread(counts: UnreadCounts, asked: unknown[] = []): UnreadFetcher {
  return (p) => {
    asked.push(p);
    return Promise.resolve({
      kind: "project",
      project: { project: "p", scope: "production", totals: counts, not_available: {}, members: [] },
    });
  };
}

const COUNTS: UnreadCounts = {
  files_not_read: 226,
  files_syntax_errors: 1,
  files_without_facts: 444,
  dynamic_calls: 10,
  wrapper_calls: 21,
  unplaced_routes: 1,
  routes_with_sections_not_read: 260,
  unresolved_calls_unknown: null,
  unresolved_calls_external: null,
};

function emptyEndpoints(): EndpointsResponse {
  const r = endpointsGolden("api-endpoints-production");
  for (const t of r.transports) {
    t.endpoint_count = 0;
    t.calls = { ...t.calls, total: 0, by_status: {} };
    t.endpoints = [];
    t.roots = [];
  }
  return r;
}

function page(opts: { summary?: SchemaSummary; client?: Parameters<typeof OverviewSection>[0]["client"]; asked?: unknown[]; labels?: string | null; status?: StatusView } = {}) {
  return render(
    <LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: opts.labels ?? null, error: null })}>
      <OverviewSection
        status={opts.status ?? OK}
        summary={ok(opts.summary ?? summaryGolden("fixture"))}
        client={opts.client ?? fakeClient()}
        architectureClient={fakeArchClient("components")}
        unreadFetcher={unread(COUNTS, opts.asked)}
      />
    </LabelProvider>,
  );
}

describe("OverviewSection", () => {
  it("names the repository, its index time and extractor from the status", async () => {
    page();
    expect((await screen.findByTestId("overview-title")).textContent).toBe(goldenStatus("fresh").repository);
    expect(screen.getByTestId("overview-run").textContent).toContain(goldenStatus("fresh").extractor.current);
    // No file total or language line: no endpoint serves them (T061 Q1).
    expect(screen.getByTestId("overview-page").textContent).not.toMatch(/\d files ·.*indexed/);
  });

  it("lists the components on the default map, the libraries in its band and the hidden ones", async () => {
    page();
    const parts = await screen.findByTestId("overview-parts");
    await within(parts).findAllByTestId("overview-component");
    // Nine components are drawn as cards (four libraries in the band, three hidden).
    expect(within(parts).getAllByTestId("overview-component")).toHaveLength(9);
    expect(parts.textContent).toContain("9 components on the map · 4 workspace libraries in the band · 3 links");
    expect(within(parts).getByTestId("overview-libraries").textContent).toContain("in the band below the cards");
    expect(within(parts).getByTestId("overview-libraries").textContent).toContain("4 packages");
    expect(within(parts).getByTestId("overview-libraries").textContent).toContain("2 edges in");
    // Libraries are on by default (T067): the note never says the map draws no line.
    const note = within(parts).getByTestId("overview-libraries-note").textContent;
    expect(note).toContain("2 in-process edges end in the workspace libraries (2 from web). The map draws them as lines into the libraries band below the cards. Libraries band");
    expect(note).not.toContain("no line");
    expect(note).not.toContain("folded");
    expect(within(parts).getByRole("link", { name: "Libraries band" }).getAttribute("href")).toBe("#/architecture");
    expect(parts.innerHTML).not.toContain("show=libraries");
    expect(within(parts).getByTestId("overview-hidden").textContent).toBe("Hidden on the map: tests 1 component · docs 1 component · configured 1 component");
  });

  it("lists only the lines between two cards, and a map whose lines all end in the band points to the band", async () => {
    const r = bandAnswer("pdfcraft");
    const band = buildMap(r, DEFAULT_SHOW, null).edges.filter((e) => e.band !== undefined);
    expect(band.length).toBeGreaterThan(0);
    expect(cardLines(r).every((e) => e.band === undefined)).toBe(true);
    // Every line of this answer ends in the band: no card-to-card line.
    const noCardLines = { ...r, links: r.links.filter((l) => band.some((e) => e.links.includes(l))) };
    expect(cardLines(noCardLines)).toEqual([]);
    render(
      <LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: null, error: null })}>
        <OverviewSection status={OK} summary={ok(summaryGolden("fixture"))} client={fakeClient()} architectureClient={{ ...fakeArchClient("components"), overview: () => Promise.resolve(noCardLines) }} unreadFetcher={unread(COUNTS)} />
      </LabelProvider>,
    );
    const note = await screen.findByTestId("overview-libraries-note");
    expect(note.textContent).not.toContain("no line");
    expect(screen.queryByTestId("overview-links")).toBeNull();
    const start = screen.getByTestId("overview-start");
    expect(within(start).getByRole("link", { name: "How the components use the libraries" }).getAttribute("href")).toBe("#/architecture");
    expect(start.textContent).toContain("the map's libraries band");
  });

  it("gives each transport its numbers and the empty ones one 'none found' line", async () => {
    page();
    const card = await screen.findByTestId("overview-transport-http");
    expect(card.textContent).toContain("9HTTP routes · express");
    expect(card.textContent).toContain("18client calls");
    expect(card.textContent).toContain("9linked to a route (50 %)");
    expect(card.textContent).toContain("5routes with no linked caller in production");
    expect(screen.getByTestId("overview-none-found").textContent).toBe("");
  });

  it("names the empty transports in the API page's words, where none found holds", async () => {
    const r = endpointsGolden("api-endpoints-production");
    const http = r.transports.find((t) => t.transport === "http");
    const rest = emptyEndpoints().transports.filter((t) => t.transport !== "http");
    if (http === undefined) throw new Error("no http transport");
    r.transports = [http, ...rest];
    page({ client: { ...fakeClient(), endpoints: () => Promise.resolve(r) } });
    const none = await screen.findByTestId("overview-none-found");
    expect(none.textContent).toMatch(/: none found in the indexed production files$/);
    expect(none.textContent).toContain("Tauri IPC");
  });

  it("says once that no interface was found in the indexed files, never 'in this repository'", async () => {
    page({ client: { ...fakeClient(), endpoints: () => Promise.resolve(emptyEndpoints()) } });
    const none = await screen.findByTestId("overview-no-interfaces");
    // The API page's sentence (T065), without its Include tests hint.
    expect(none.textContent).toBe("No HTTP routes or client calls, Tauri IPC commands or invoke sites or server actions or action uses found in the indexed production files.");
    expect(screen.getByTestId("overview-interfaces").textContent).not.toContain("in this repository");
  });

  it("says what found none does not mean, and a tool found but not read is 'not read yet'", async () => {
    page({ summary: craftedSummary({ found: "none", sets: 0, model_groups: 0, sources_read: ["migrations", "models"], unread_tools: [{ tool: "alembic", path: "migrations", marker: "alembic.ini", files: 119 }] }) });
    const data = await screen.findByTestId("overview-data");
    expect(data.textContent).toContain("No migration set or model table that Graphvein reads was found in the indexed files. This does not mean there is no database.");
    expect(within(data).getByTestId("overview-unread-tools").textContent).toBe("Alembic migrations (119 files): found, not read yet");
    expect(within(data).queryByTestId("overview-refused")).toBeNull();
  });

  it("names a model source found and refused 'found, not read' with its reason, as the Schema page does (T070)", async () => {
    page({
      summary: craftedSummary({
        found: "none",
        sets: 0,
        model_groups: 0,
        sources_read: ["migrations", "models"],
        unread_tools: [],
        refused_sources: [{ tool: "gorm", file: "go.mod", code: "schema_model_not_proven", message: "go.mod: GORM module example.com/app is not read: model/main.go:148: dialector not read" }],
      }),
    });
    const data = await screen.findByTestId("overview-data");
    expect(data.textContent).toContain("Graphvein found GORM models but did not read them.");
    expect(data.textContent).not.toContain("No migration set");
    expect(within(data).getByTestId("overview-refused").textContent).toBe("GORM models go.mod: found, not read (GORM module example.com/app is not read: model/main.go:148: dialector not read)");
  });

  it("says the counts are incomplete when the run has files it could not parse (T074)", async () => {
    const status = goldenStatus("fresh");
    status.index.parse_failed = 39;
    page({ status: { kind: "ok", status } });
    const notice = await screen.findByTestId("parse-failed-notice");
    expect(notice.textContent).toBe("Counts are incomplete: 39 files could not be parsed (parse_failed), so their facts are missing. See Couldn't read");
    expect(within(notice).getByRole("link").getAttribute("href")).toBe("#/unread");
  });

  it("shows no parse notice for a run that parsed every file", async () => {
    page();
    await screen.findByTestId("overview-title");
    expect(screen.queryByTestId("parse-failed-notice")).toBeNull();
  });

  it("lists what could not be read from the counts, asked for production code", async () => {
    const asked: unknown[] = [];
    page({ asked });
    const card = await screen.findByTestId("overview-unread");
    await within(card).findByText("files not read · 1 read with syntax errors");
    expect(card.textContent).toContain("444files with no fact stored");
    expect(card.textContent).toContain("31client calls not resolved (10 dynamic, 21 through a wrapper)");
    expect(asked).toEqual([{ scope: "production" }]);
  });

  it("asks the label question here until it is answered; a repository default hides it", async () => {
    const first = page();
    expect(await screen.findByTestId("labels-banner")).toBeTruthy();
    first.unmount();
    page({ labels: "technical" });
    await screen.findByTestId("overview-parts");
    expect(screen.queryByTestId("labels-banner")).toBeNull();
  });
});

describe("overview helpers", () => {
  it("unreadLines leaves out counts that are not available and zero counts but the files", () => {
    expect(unreadLines({ files_not_read: 0, files_syntax_errors: 0, files_without_facts: 3, dynamic_calls: 0, wrapper_calls: 0, unplaced_routes: null, unresolved_calls_unknown: 185_000, unresolved_calls_external: 41_600 })).toEqual([
      { value: 0, text: "files not read · 0 read with syntax errors" },
      { value: 3, text: "files with no fact stored" },
      { value: 185_000, text: "in-code calls to unknown code · 41.6K to external code" },
    ]);
  });

  it("linksIntoLibraries counts the links into folded libraries per source", () => {
    const r = archGolden("components");
    const into = linksIntoLibraries(r);
    expect(into.reduce((a, b) => a + b.count, 0)).toBe(r.libraries.links_in);
  });

  it("hiddenRootLines names the calls of a root whose component the map hides", () => {
    const t = endpointsGolden("api-endpoints-production").transports[0];
    if (t === undefined) throw new Error("no transport");
    t.roots = [{ root: "docs-site", manifest: null, total: 409, by_status: {}, repository_by_status: null }];
    expect(hiddenRootLines(t, archGolden("components"))).toEqual(["409 client calls from docs-site/, hidden on the map (docs)"]);
    expect(hiddenRootLines(t, null)).toEqual([]);
  });
});
