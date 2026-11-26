import { describe, expect, it } from "vitest";

import {
  categoryOf,
  coverageCells,
  linkedCount,
  gapStatuses,
  isNeverRow,
  isPossiblyCalledRow,
  linkedPercent,
  neverCalledLabel,
  type Category,
} from "@/api/categories";
import type { Transport } from "@/api/endpoints";

// F6: every documented status of each transport.
const HTTP: [string, Category][] = [
  ["linked", "linked"],
  ["ambiguous", "ambiguous"],
  ["unmatched", "unmatched"],
  ["dynamic", "dynamic"],
  ["test_only", "test_only"],
  ["wrapper", "wrapper"],
];
const TAURI: [string, Category][] = [
  ["dynamic", "dynamic"],
  ["registry_unproven", "unproven"],
  ["acl_possible", "unproven"],
  ["ambiguous", "ambiguous"],
  ["shadow_possible", "unproven"],
  ["unknown_command", "unproven"],
  ["test_only", "test_only"],
  ["unmatched", "unmatched"],
  ["plugin", "plugin"],
  ["global_unlinked", "global"],
  ["linked", "linked"],
  ["wrapper", "wrapper"],
];

describe("categoryOf", () => {
  it.each(HTTP)("http %s -> %s", (raw, want) => {
    expect(categoryOf("http", raw)).toBe(want);
  });
  it.each(TAURI)("tauri_ipc %s -> %s", (raw, want) => {
    expect(categoryOf("tauri_ipc", raw)).toBe(want);
  });

  const unknown = ["weird", "LINKED", "linked ", " linked", "Linked", "", "constructor", "__proto__", "toString"];
  it.each(unknown)("maps %j to other on both transports", (raw) => {
    expect(categoryOf("http", raw)).toBe("other");
    expect(categoryOf("tauri_ipc", raw)).toBe("other");
  });

  it("maps a tauri status on http to other", () => {
    for (const raw of ["plugin", "registry_unproven", "acl_possible", "global_unlinked", "unknown_command"]) {
      expect(categoryOf("http", raw)).toBe("other");
    }
  });

  it("maps nothing but exactly linked to linked", () => {
    const candidates = [...HTTP, ...TAURI].map(([r]) => r).concat(["LINKED", "linked\n", "linked2", "link", "", "true"]);
    for (const t of ["http", "tauri_ipc"] as Transport[]) {
      for (const raw of candidates) {
        expect(categoryOf(t, raw) === "linked", `${t} ${JSON.stringify(raw)}`).toBe(raw === "linked");
      }
    }
  });
});

describe("linkedPercent", () => {
  it("floors, so 199 of 200 is 99", () => {
    expect(linkedPercent(199, 200)).toBe(99);
    expect(linkedPercent(200, 200)).toBe(100);
    expect(linkedPercent(160, 588)).toBe(27);
    expect(linkedPercent(0, 3)).toBe(0);
  });
  it("is null without calls", () => {
    expect(linkedPercent(0, 0)).toBeNull();
  });
});

describe("coverageCells", () => {
  it("always shows the M5 five, in order, also at 0", () => {
    const cells = coverageCells("http", {}, 0);
    expect(cells.map((c) => (c.kind === "never" ? "never" : c.category))).toEqual([
      "linked",
      "unmatched",
      "dynamic",
      "wrapper",
      "never",
    ]);
    expect(cells.every((c) => c.count === 0)).toBe(true);
  });

  it("adds further categories only when not zero, in D10 order", () => {
    const cells = coverageCells(
      "tauri_ipc",
      { linked: 2, weird: 1, LINKED: 1, "": 1, plugin: 1, acl_possible: 1, shadow_possible: 2, ambiguous: 1 },
      2,
    );
    expect(cells.map((c) => (c.kind === "never" ? "never" : c.category))).toEqual([
      "linked",
      "unmatched",
      "dynamic",
      "wrapper",
      "never",
      "ambiguous",
      "unproven",
      "plugin",
      "other",
    ]);
    const other = cells.at(-1);
    expect(other?.kind === "category" && other.count).toBe(3);
    expect(other?.kind === "category" && other.raws).toEqual([
      { status: "", count: 1 },
      { status: "LINKED", count: 1 },
      { status: "weird", count: 1 },
    ]);
    // LINKED is not linked.
    expect(cells[0]?.count).toBe(2);
  });
});

describe("gapStatuses", () => {
  it("requests exactly the raw values seen for other", () => {
    expect(gapStatuses("http", "other", { linked: 1, weird: 2, "": 1, plugin: 1 })).toEqual(["", "plugin", "weird"]);
    expect(gapStatuses("http", "other", { linked: 1 })).toEqual([]);
  });
  it("falls back to the documented statuses of a category", () => {
    expect(gapStatuses("tauri_ipc", "unproven", {})).toEqual([
      "registry_unproven",
      "acl_possible",
      "shadow_possible",
      "unknown_command",
    ]);
    expect(gapStatuses("tauri_ipc", "unproven", { acl_possible: 1 })).toEqual(["acl_possible"]);
  });
});

describe("neverCalledLabel", () => {
  it("says no linked caller in production in production scope, where test calls are excluded", () => {
    expect(neverCalledLabel("production")).toBe("No linked caller in production");
    expect(neverCalledLabel("production", 5)).toBe("5 with no linked caller in production");
  });
  it("says no linked caller in scope all", () => {
    expect(neverCalledLabel("all")).toBe("No linked caller");
    expect(neverCalledLabel("all", 0)).toBe("0 with no linked caller");
  });
  it("says no linked caller in the project in project mode, whatever the scope", () => {
    expect(neverCalledLabel("production", undefined, true)).toBe("No linked caller in the project");
    expect(neverCalledLabel("all", 2, true)).toBe("2 with no linked caller in the project");
  });
});

describe("project-scope categories (T022 MR-UI)", () => {
  it("maps the project statuses, shows linked across right after linked in project mode only, and counts it as linked-class", () => {
    expect(categoryOf("http", "linked_across")).toBe("linked_across");
    expect(categoryOf("http", "ambiguous_across")).toBe("ambiguous_across");
    expect(categoryOf("http", "Linked_across")).toBe("other");
    const byStatus = { linked: 1, linked_across: 27, dynamic: 1 };
    const project = coverageCells("http", byStatus, 0, true).map((c) => (c.kind === "never" ? "never" : c.category));
    expect(project.slice(0, 3)).toEqual(["linked", "linked_across", "unmatched"]);
    const single = coverageCells("http", { linked: 1, unmatched: 2 }, 0).map((c) => (c.kind === "never" ? "never" : c.category));
    expect(single).toEqual(["linked", "unmatched", "dynamic", "wrapper", "never"]);
    expect(linkedCount(byStatus)).toBe(28);
    expect(linkedCount({ linked: 3 })).toBe(3);
  });
});

describe("possibly called rows", () => {
  it("keeps a route only ambiguous calls may reach out of never called", () => {
    const row = { caller_count: 0, project_caller_count: 0, project_possible_caller_count: 2 };
    expect(isNeverRow(row)).toBe(false);
    expect(isPossiblyCalledRow(row)).toBe(true);
    expect(isNeverRow({ ...row, project_possible_caller_count: 0 })).toBe(true);
    expect(isPossiblyCalledRow({ ...row, project_caller_count: 1 })).toBe(false);
    expect(isNeverRow({ caller_count: 0, project_caller_count: null, project_possible_caller_count: null })).toBe(true);
  });
});
