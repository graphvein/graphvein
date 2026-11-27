// T070: model sources a linker found and refused as a whole
// (refused_sources) are named "found, not read" with the reason on the
// found-none page, never "No migrations or model tables found"; the
// unknown page's scan_incomplete wording names what the API says was not
// scanned; unread tools carry plain names.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { parseSchemaSummary, type RefusedSource, type UnreadTool } from "@/api/schema";
import { NoneFound, noneTitle, scanGapKinds, UnknownPage } from "@/sections/schema/EmptyStates";
import { toolLabel, unreadTitle } from "@/sections/schema/SourceCards";
import { refusalReason, refusalText } from "@/sections/schema/tools";
import { craftedFailedOnly, craftedSummary } from "@/test/schemaFixtures";

// new-api's refusal (explore set, T066 stop (2)).
const gorm: RefusedSource = {
  tool: "gorm",
  file: "go.mod",
  code: "schema_model_not_proven",
  message: "go.mod: GORM module github.com/x/new-api is not read: no gorm.Open site is read, so the dialect and the configuration are unknown",
};
// dossier's refusals.
const prismaPackage: RefusedSource = {
  tool: "prisma",
  file: "package.json",
  code: "schema_model_not_proven",
  message: "package.json: no Prisma schema is proven for this package: the schema setting of prisma7.config.ts is not a string literal",
};
const prismaOutside: RefusedSource = {
  tool: "prisma",
  file: "prisma/schema.prisma",
  code: "schema_model_unread",
  message: "1 .prisma file(s) are outside every discovered Prisma schema and not read: prisma/schema.prisma",
};
const prismaMigrate: UnreadTool = { tool: "prisma-migrate", path: "prisma/migrations", marker: null, files: 4 };
const models = ["migrations", "models"];

describe("refused model sources (T070)", () => {
  it("titles the found-none page by what was found and not read", () => {
    expect(noneTitle({ unread_tools: [], sources_read: models, refused_sources: [gorm] })).toBe("Graphvein found GORM models but did not read them");
    expect(noneTitle({ unread_tools: [prismaMigrate], sources_read: models, refused_sources: [prismaPackage, prismaOutside] })).toBe("Graphvein found Prisma models and migrations but did not read them");
    expect(noneTitle({ unread_tools: [{ tool: "drizzle-kit", path: "drizzle", marker: null, files: 1 }], sources_read: models, refused_sources: [gorm] })).toBe(
      "Graphvein found GORM models and Drizzle migrations but did not read them",
    );
    expect(noneTitle({ unread_tools: [{ tool: "gorm-v1", path: "m", marker: null, files: 1 }], sources_read: models, refused_sources: [gorm] })).toBe("Graphvein found GORM and GORM v1 models but did not read them");
    // Without a refusal the title is unchanged (owncast: no refusal).
    expect(noneTitle({ unread_tools: [], sources_read: models, refused_sources: [] })).toBe("No migrations or model tables found");
    expect(noneTitle({ unread_tools: [], sources_read: models, refused_sources: null })).toBe("No migrations or model tables found");
  });

  it("lists each refusal with its file and reason, never an absence", () => {
    render(<NoneFound summary={craftedSummary({ found: "none", repository: "new-api", sources_read: models, unread_tools: [], refused_sources: [gorm] })} />);
    const page = screen.getByTestId("schema-none");
    expect(page.textContent).not.toContain("No migrations or model tables found");
    expect(screen.getByRole("heading").textContent).toBe("Graphvein found GORM models but did not read them");
    const items = Array.from(screen.getByTestId("none-refused").querySelectorAll("li"), (li) => li.textContent);
    expect(items).toEqual(["GORM models found in go.mod, not read: GORM module github.com/x/new-api is not read: no gorm.Open site is read, so the dialect and the configuration are unknown"]);
  });

  it("keeps the message whole unless it starts with the file", () => {
    expect(refusalReason(prismaOutside)).toBe(prismaOutside.message);
    expect(refusalReason({ ...prismaOutside, file: "mongo/schema.prisma", message: "mongo/schema.prisma:2: provider mongodb: collections are out of scope, no table is claimed" })).toBe(
      "line 2: provider mongodb: collections are out of scope, no table is claimed",
    );
    expect(refusalText(prismaPackage)).toBe("Prisma models found in package.json, not read: no Prisma schema is proven for this package: the schema setting of prisma7.config.ts is not a string literal");
  });

  it("reads refused_sources leniently: absent is not known, a malformed entry is not known", () => {
    const base = { api_version: 0, repository: "r", index: null, found: "none", unknown_reason: null, sets: 0, test_only_sets: 0, failed_sets: 0 };
    expect(parseSchemaSummary(base).refused_sources).toBeNull();
    expect(parseSchemaSummary({ ...base, refused_sources: [gorm] }).refused_sources).toEqual([gorm]);
    expect(parseSchemaSummary({ ...base, refused_sources: [{ tool: "gorm" }] }).refused_sources).toBeNull();
  });
});

describe("scan_incomplete wording (T070, baseline defect 4)", () => {
  it("names what the scanner did not read, not a Cargo manifest", () => {
    const sets = craftedFailedOnly();
    sets.scan_gaps = [
      { file: "CLAUDE.md", code: "symlink", message: "CLAUDE.md: symlink not followed" },
      { file: "AGENTS.md", code: "symlink", message: "AGENTS.md: symlink not followed" },
    ];
    render(<UnknownPage summary={craftedSummary({ found: "unknown", unknown_reason: "scan_incomplete", scan_gaps: 2, repository: "app" })} found="unknown" reason="scan_incomplete" current={null} sets={sets} />);
    const page = screen.getByTestId("schema-unknown");
    expect(page.textContent).toContain("The scanner did not read 2 paths (2 symlinks not followed, listed below). A migration set behind a path it did not read would be missed");
    expect(page.textContent).not.toContain("Cargo manifest");
  });

  it("counts each kind of gap", () => {
    expect(scanGapKinds([{ file: "a", code: "scan_read", message: "" }, { file: "Cargo.toml", code: "file_read", message: "" }, { file: "b", code: "symlink", message: "" }, { file: "c", code: "new_code", message: "" }])).toBe(
      "1 directory not listed, 1 Cargo.toml not read, 1 symlink not followed, 1 new_code",
    );
  });
});

describe("plain tool names (T070)", () => {
  it("names every tool unread_tools can send", () => {
    expect(toolLabel("sequelize-migrations")).toBe("Sequelize");
    expect(toolLabel("typeorm-migrations")).toBe("TypeORM");
    expect(toolLabel("diesel-migrations")).toBe("Diesel");
    expect(toolLabel("gorm-v1")).toBe("GORM v1");
    expect(toolLabel("flyway")).toBe("flyway");
    const t = (tool: string): UnreadTool => ({ tool, path: "db", marker: null, files: 1 });
    expect(unreadTitle(t("sequelize-migrations"))).toBe("Sequelize migrations found, not read yet");
    expect(unreadTitle(t("typeorm-migrations"))).toBe("TypeORM migrations found, not read yet");
    expect(unreadTitle(t("diesel-migrations"))).toBe("Diesel migrations found, not read yet");
    expect(unreadTitle(t("gorm-v1"))).toBe("GORM v1 models found, not read yet");
  });
});
