import { act, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { normaliseHash, parseRoute } from "@/router";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

const schemaClient = () => fakeSchemaClient({ summary: summaryGolden("fixture") });

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));

function setHash(hash: string) {
  window.history.replaceState(null, "", hash === "" ? window.location.pathname : hash);
}

beforeEach(() => {
  setHash("");
});

describe("router", () => {
  it("redirects an empty hash to #/api without a history entry", () => {
    const before = window.history.length;
    for (const hash of ["", "#", "#/"]) {
      setHash(hash);
      normaliseHash();
      expect(window.location.hash).toBe("#/api");
    }
    expect(window.history.length).toBe(before);
  });

  it("parses sections and unknown hashes", () => {
    expect(parseRoute("#/api")).toMatchObject({ kind: "section", section: { id: "api" } });
    expect(parseRoute("")).toMatchObject({ kind: "section", section: { id: "api" } });
    expect(parseRoute("#/schema")).toMatchObject({ kind: "section", section: { id: "schema" } });
    expect(parseRoute("#/schema?tab=overview")).toMatchObject({ kind: "section", section: { id: "schema" } });
    // Disabled sections are not routes yet.
    expect(parseRoute("#/review")).toEqual({ kind: "not_found", hash: "#/review" });
    expect(parseRoute("#/nope")).toEqual({ kind: "not_found", hash: "#/nope" });
  });

  it("shows the API section and Not found for an unknown hash", async () => {
    setHash("#/api");
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={schemaClient()} />);
    expect(await screen.findByRole("heading", { name: "API" })).toBeTruthy();
    expect(await screen.findByTestId("card-http")).toBeTruthy();

    act(() => {
      window.location.hash = "#/nope";
    });
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeTruthy();
    const back = screen.getByRole("link", { name: "Go to the API section" });
    expect(back.getAttribute("href")).toBe("#/api");
  });

  it("links only available sections", async () => {
    setHash("#/api");
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={schemaClient()} />);
    const nav = screen.getByRole("navigation", { name: "Sections" });
    await screen.findAllByText("fixture");
    const api = within(nav).getByRole("link", { name: "API" });
    expect(api.getAttribute("href")).toBe("#/api");
    expect(api.getAttribute("aria-current")).toBe("page");
    const schema = within(nav).getByRole("link", { name: /^Schema/ });
    expect(schema.getAttribute("href")).toBe("#/schema");
    const arch = within(nav).getByRole("link", { name: "Architecture" });
    expect(arch.getAttribute("href")).toBe("#/architecture");
    // T029: the Explorer is its own section.
    expect(within(nav).getByRole("link", { name: "Explorer" }).getAttribute("href")).toBe("#/explorer");
    for (const label of ["Review"]) {
      const item = within(nav).getByText(label).closest("li");
      expect(item?.querySelector("a, button, [tabindex]"), label).toBeNull();
      expect(item?.hasAttribute("aria-disabled"), label).toBe(false);
      // Screen readers hear "coming soon"; the visual "Soon" badge is hidden from them.
      expect(item?.textContent, label).toBe(`${label}, coming soonSoon`);
      expect(item?.querySelector('[aria-hidden="true"][data-slot="badge"]')?.textContent, label).toBe("Soon");
    }
    expect(within(nav).getAllByRole("link").map((a) => a.textContent.replace(/\d+ unreadable$/, ""))).toEqual(["API", "Explorer", "Architecture", "Schema", "Changes", "Couldn't read", "Settings", "Help?", "Third-party licences"]);
  });

  it("treats #/api with a query as the API section", () => {
    expect(parseRoute("#/api?sel=route%3Ax&gap=http%3Adynamic")).toMatchObject({ kind: "section", section: { id: "api" } });
    expect(parseRoute("#/api?")).toMatchObject({ kind: "section", section: { id: "api" } });
    expect(parseRoute("#/apix?sel=1")).toEqual({ kind: "not_found", hash: "#/apix?sel=1" });
  });
});
