// The guide in docs/guide (T045): it parses with the build's parser, its
// anchors are unique, and every link resolves: an app link to a route the
// app has (and a Help anchor to a heading of the guide), a relative link to
// a public document of the repository.
import { describe, expect, it } from "vitest";

import { DEFAULT_HASH, parseRoute } from "@/router";
import { GUIDE } from "@/sections/help/guide";
import { docPath } from "@/sections/help/HelpSection";
import { FIELDS, SECTIONS as SETTINGS_SECTIONS } from "@/sections/settings/model";
import { GuideSyntaxError, linksOf, parseGuidePage, parseInline, plainText, slugOf } from "@/sections/help/markdown";

// The repository's Markdown files, by path relative to this file; only the
// keys are used (lazy glob: nothing is loaded).
const DOCS = Object.keys(import.meta.glob("../../../../docs/**/*.md"));

/** PENDING_ROUTES are routes the guide names that the app does not have
 * yet; empty since the 2026-11-24 branches were integrated (Settings,
 * couldn't-read), so every guide link is checked. */
const PENDING_ROUTES: readonly string[] = [];

/** PROJECT_ROUTES exist only under serve --project (AppShell's
 * projectRoute). */
const PROJECT_ROUTES = ["#/project"];

function routePath(href: string): string {
  const q = href.indexOf("?");
  return q === -1 ? href : href.slice(0, q);
}

describe("guide files", () => {
  it("are all parsed, in order, starting with Start here", () => {
    expect(GUIDE.map((p) => p.file)).toEqual(["1-start-here.md", "2-sections.md", "3-beyond.md", "4-faq.md", "5-shortcuts.md"]);
    const first = GUIDE[0]?.blocks[0];
    expect(first?.t === "heading" ? first.slug : null).toBe("start-here");
  });

  it("have unique heading anchors across the whole guide", () => {
    const slugs = GUIDE.flatMap((p) => p.blocks.flatMap((b) => (b.t === "heading" ? [b.slug] : [])));
    expect(new Set(slugs).size).toBe(slugs.length);
  });

  it("place the three Start here examples once each, each with its fixed illustration", () => {
    const examples = GUIDE.flatMap((p) => p.blocks.flatMap((b) => (b.t === "example" ? [b] : [])));
    expect(examples.map((b) => b.kind)).toEqual(["fact", "inferred", "unknown"]);
    expect(examples.map((b) => [b.fixed.subject, b.fixed.label])).toEqual([
      ["GET /api/users", "Fact · 1.0"],
      ['fetch("/api/users")', "Inferred · 0.9"],
      ["fetch(baseUrl + path)", "Unknown"],
    ]);
  });

  it("have about ten questions", () => {
    const faq = GUIDE.find((p) => p.file === "4-faq.md");
    const questions = faq?.blocks.filter((b) => b.t === "heading" && b.level === 2) ?? [];
    expect(questions.length).toBeGreaterThanOrEqual(9);
    expect(questions.length).toBeLessThanOrEqual(12);
  });

  it("link only to routes the app has, headings of the guide and public documents", () => {
    const slugs = new Set(GUIDE.flatMap((p) => p.blocks.flatMap((b) => (b.t === "heading" ? [b.slug] : []))));
    const links = linksOf(GUIDE);
    expect(links.length).toBeGreaterThan(10);
    for (const { file, href } of links) {
      const where = `${file}: ${href}`;
      if (href.startsWith("#/")) {
        const path = routePath(href);
        if (PENDING_ROUTES.includes(path) || PROJECT_ROUTES.includes(path)) continue;
        expect(parseRoute(href).kind, where).toBe("section");
        expect(href === DEFAULT_HASH || path !== "#/", where).toBe(true);
        if (path === "#/help") {
          const at = new URLSearchParams(href.slice(href.indexOf("?") + 1)).get("at");
          expect(at !== null && slugs.has(at), where).toBe(true);
        }
        if (path === "#/settings" && href.includes("?")) {
          // A Settings anchor: a section of the page (Storage, outside
          // graphvein.yaml, included) or one of its key rows.
          const at = new URLSearchParams(href.slice(href.indexOf("?") + 1)).get("at");
          expect(at !== null && (at === "storage" || SETTINGS_SECTIONS.some((s) => s.id === at) || Object.hasOwn(FIELDS, at)), where).toBe(true);
        }
        continue;
      }
      const doc = docPath(href);
      expect(doc, where).not.toBeNull();
      // docs/... seen from web/src/sections/help.
      expect(DOCS, where).toContain(`../../../../${doc ?? ""}`);
      // Public documents only (the public-tree rule): the CLI and
      // configuration pages, contracts, ADRs, the guide itself and the
      // known gaps page.
      expect(/^docs\/(cli|guide|contract|adr|extract|lang)\/|^docs\/(config|known-gaps)\.md$/.test(doc ?? ""), where).toBe(true);
    }
  });

  it("names the editor's place in Settings and the first-click chooser", () => {
    const hrefs = linksOf(GUIDE).map((l) => l.href);
    expect(hrefs).toContain("#/settings?at=editor");
    expect(hrefs).toContain("#/settings?at=labels");
    const faq = GUIDE.flatMap((p) => p.blocks).map((b) => (b.t === "paragraph" ? plainText(b.c) : "")).join("\n");
    expect(faq).toContain("Remember for this browser");
    expect(faq).not.toMatch(/\bDisplay\b/);
  });

  it("keeps the pending routes in step with the router", () => {
    // When an integration adds a route, it must leave this list.
    for (const r of PENDING_ROUTES) expect(parseRoute(r).kind, r).toBe("not_found");
  });

  it("never says 'breaks' and calls the number a rank, not a probability", () => {
    const text = GUIDE.flatMap((p) =>
      p.blocks.flatMap((b) => (b.t === "heading" || b.t === "paragraph" ? [plainText(b.c)] : b.t === "list" ? b.items.map(plainText) : b.t === "example" ? [plainText(b.fixed.note)] : [])),
    ).join("\n");
    expect(text).not.toMatch(/\bbreaks?\b(?!\s*")/i);
    expect(text).toContain("fixed rank per match rule, not a probability");
  });
});

describe("guide parser", () => {
  it("reads headings, lists, tables, inline forms and the example marker", () => {
    const page = parseGuidePage(
      "x.md",
      "# Title\n\nSome **bold** and `code` and [a link](#/api).\n\n- one\n  more\n- two\n\n1. first\n2. second\n\n| Keys | What |\n|---|---|\n| `?` | Help |\n\n<!-- graphvein:example fact -->\n**Example:** `GET /x` · Fact · 1.0 · from [its](#/api)\nregistration\n",
    );
    expect(page.blocks.map((b) => b.t)).toEqual(["heading", "paragraph", "list", "list", "table", "example"]);
    const ex = page.blocks[5];
    expect(ex?.t === "example" ? [ex.kind, ex.fixed.subject, ex.fixed.label, plainText(ex.fixed.note)] : null).toEqual(["fact", "GET /x", "Fact · 1.0", "from its registration"]);
    expect(linksOf([page]).map((l) => l.href)).toEqual(["#/api", "#/api"]);
    const plain = parseGuidePage("y.md", "# T\n\n<!-- graphvein:example unknown -->\n\n**Example:** `f(x)` · Unknown\n");
    expect(plain.blocks[1]).toEqual({ t: "example", kind: "unknown", fixed: { subject: "f(x)", label: "Unknown", note: [] } });
    const list = page.blocks[2];
    expect(list?.t === "list" ? list.items.map(plainText) : null).toEqual(["one more", "two"]);
    expect(parseInline("a [b `c`](#/x) d", "x.md", 1)).toEqual([
      { t: "text", v: "a " },
      { t: "link", href: "#/x", c: [{ t: "text", v: "b " }, { t: "code", v: "c" }] },
      { t: "text", v: " d" },
    ]);
  });

  it("refuses forms the page does not render, with file and line", () => {
    const bad = ["# T\n\n<div>x</div>\n", "# T\n\n*em*\n", "# T\n\n```\ncode\n```\n", "# T\n\nan `unclosed\n", "# T\n\n#### deep\n", "# T\n\n## A\n\n## A\n", "no title\n", "# T\n\n# Two\n", "# T\n\n![img](x.png)\n", "# T\n\n| a |\n| b |\n"];
    for (const src of bad) expect(() => parseGuidePage("bad.md", src), src).toThrow(GuideSyntaxError);
    expect(() => parseGuidePage("bad.md", "# T\n\nfine\n<b>x</b>\n")).toThrow(/bad\.md:4/);
    // An example marker takes exactly its kind's fixed illustration.
    expect(() => parseGuidePage("bad.md", "# T\n\n<!-- graphvein:example fact -->\n")).toThrow(/bad\.md:3: the fact example marker/);
    expect(() => parseGuidePage("bad.md", "# T\n\n<!-- graphvein:example fact -->\nJust text.\n")).toThrow(/bad\.md:3/);
    expect(() => parseGuidePage("bad.md", "# T\n\n<!-- graphvein:example fact -->\n**Example:** `x` · Inferred · 0.9\n")).toThrow(/bad\.md:4: the fact example is labelled Inferred · 0.9/);
    expect(() => parseGuidePage("bad.md", "# T\n\n<!-- graphvein:example inferred -->\n**Example:** `x` · Inferred · 1.0\n")).toThrow(/bad\.md:4/);
    expect(() => parseGuidePage("bad.md", "# T\n\n<!-- graphvein:example unknown -->\n**Example:** x · Unknown\n")).toThrow(/bad\.md:4/);
    expect(() => parseGuidePage("bad.md", "# T\n\n**Example:** `x` · Unknown\n")).toThrow(/bad\.md:3: an \*\*Example:\*\* paragraph needs an example marker/);
  });

  it("makes anchors from the heading text", () => {
    expect(slugOf("Is the number beside “Inferred” a probability?")).toBe("is-the-number-beside-inferred-a-probability");
    expect(slugOf("What Graphvein couldn't read")).toBe("what-graphvein-couldnt-read");
  });

  it("resolves a relative document link from docs/guide", () => {
    expect(docPath("../cli/mcp.md")).toBe("docs/cli/mcp.md");
    expect(docPath("faq.md")).toBe("docs/guide/faq.md");
    expect(docPath("https://example.com/x.md")).toBeNull();
    expect(docPath("#/api")).toBeNull();
  });
});
