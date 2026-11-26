import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { UiFetcher } from "@/api/ui";
import { FLAG_LABEL, WrongFactFlag } from "@/components/WrongFactFlag";
import { HOST, issueLink, issueURL, LOCAL_PATH, MAX_ISSUE_URL, redactText, ReportProvider, reportHost, SHORTENED_LINE, shownText, type WrongFact } from "@/shell/report";

const FACT: WrongFact = {
  fact: "articlePageLoader (react) calls GET /api/articles/{slug} (express)",
  where: ["react src/pages/article/article.loader.ts:15", "express apps/article/route.ts:36"],
  rule: "calls_route project link, inferred 0.7",
  shown: "Inferred across repositories",
};

const FORM = "https://github.com/example/graphvein/issues/new";

function ui(report: string | null | undefined): UiFetcher {
  return () => Promise.resolve({ api_version: 0, labels: null, error: null, ...(report !== undefined ? { report_url: report } : {}) });
}

describe("WrongFactFlag", () => {
  for (const report of [null, undefined]) {
    it(`renders nothing while report_url is ${String(report)}`, async () => {
      let asked = false;
      const fetcher: UiFetcher = (o) => {
        asked = true;
        return ui(report)(o);
      };
      const { container } = render(
        <ReportProvider fetcher={fetcher} version="graphvein dev">
          <div className="group/fact">
            <WrongFactFlag fact={() => FACT} />
          </div>
        </ReportProvider>,
      );
      await waitFor(() => {
        expect(asked).toBe(true);
      });
      expect(container.querySelector('[data-testid="wrong-fact-flag"]')).toBeNull();
      expect(screen.queryByRole("button", { name: FLAG_LABEL })).toBeNull();
    });
  }

  it("is a muted, named button that previews the prefilled issue before GitHub", async () => {
    render(
      <ReportProvider fetcher={ui(FORM)} version="graphvein dev · extractors-60 · contract 1">
        <div className="group/fact">
          <WrongFactFlag fact={() => FACT} />
        </div>
      </ReportProvider>,
    );
    const flag = await screen.findByRole("button", { name: FLAG_LABEL });
    // Hidden until hover or keyboard focus: opacity only, no layout change.
    expect(flag.className).toContain("opacity-0");
    expect(flag.className).toContain("focus-visible:opacity-100");
    expect(flag.className).toContain("group-hover/fact:opacity-100");
    fireEvent.click(flag);
    const preview = await screen.findByTestId("wrong-fact-preview");
    expect(preview.textContent).toContain("Report a wrong fact on GitHub");
    expect(screen.getByTestId("wrong-fact-text").textContent).toBe(shownText(FACT, "graphvein dev · extractors-60 · contract 1"));
    const open = screen.getByRole("link", { name: /Open on GitHub/ });
    expect(open.getAttribute("href")).toBe(issueURL(FORM, FACT, "graphvein dev · extractors-60 · contract 1"));
    expect(open.getAttribute("target")).toBe("_blank");
    expect(open.getAttribute("rel")).toBe("noopener noreferrer");
    // A1: the preview never claims that nothing is sent.
    expect(preview.textContent).not.toMatch(/Nothing is sent/i);
    expect(preview.textContent).toContain("travels to GitHub in the link when you press Open on GitHub");
    expect(preview.textContent).toContain("Copy text");
    expect(screen.queryByTestId("wrong-fact-shortened")).toBeNull();
  });

  it("says when the link text was shortened", async () => {
    const long: WrongFact = { ...FACT, rule: Array.from({ length: 400 }, (_, i) => `calls_route inferred 0.7 edge ${String(i)}`).join("; ").replace(/; /g, "\n") };
    render(
      <ReportProvider fetcher={ui(FORM)} version="graphvein dev">
        <div className="group/fact">
          <WrongFactFlag fact={() => long} />
        </div>
      </ReportProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: FLAG_LABEL }));
    expect((await screen.findByTestId("wrong-fact-shortened")).textContent).toContain("Copy text has all of it");
    // The preview and Copy text keep the whole text.
    expect(screen.getByTestId("wrong-fact-text").textContent).toBe(shownText(long, "graphvein dev"));
    const href = screen.getByRole("link", { name: /Open on GitHub/ }).getAttribute("href") ?? "";
    expect(href.length).toBeLessThan(8000);
  });

  it("puts fact, file:line and rule in What Graphvein shows", () => {
    const text = shownText(FACT, "graphvein dev");
    expect(text.split("\n")).toEqual([
      "Fact: articlePageLoader (react) calls GET /api/articles/{slug} (express)",
      "Shown as: Inferred across repositories",
      "Rule: calls_route project link, inferred 0.7",
      "Where: react src/pages/article/article.loader.ts:15",
      "  and: express apps/article/route.ts:36",
      "Version: graphvein dev",
    ]);
    const u = new URL(issueURL(FORM, FACT, "graphvein dev"));
    expect(u.origin + u.pathname).toBe(FORM);
    expect(u.searchParams.get("template")).toBe("wrong-fact.yml");
    expect(u.searchParams.get("version")).toBe("graphvein dev");
    expect(u.searchParams.get("shown")).toBe(text);
    expect([...u.searchParams.keys()]).toEqual(["template", "version", "shown"]);
    expect(reportHost(FORM)).toBe("github.com/example/graphvein");
  });

  // B1: the review's inputs. A call to an internal host, a project link's
  // assumed origin, an OpenAPI servers origin and local paths.
  it("replaces hosts and local paths in the prefill, keeping URL paths", () => {
    const f: WrongFact = {
      fact: 'loadUsers calls GET /api/users (fetch("https://billing.corp.internal:8443/api/users"))',
      where: ["/home/someone/work/app/src/users.ts:12", "web C:\\Users\\someone\\app\\routes.ts:4"],
      rule: "calls_route project link, inferred 0.7; assumption: matched across project repositories; base URL not proven; origin https://billing.corp.internal:8443/v1 assumed to be api's server",
      shown: "Inferred across repositories",
    };
    const text = shownText(f, "graphvein dev");
    expect(text).toContain(`fetch("${HOST}/api/users")`);
    expect(text).toContain(`origin ${HOST}/v1 assumed to be api's server`);
    expect(text).toContain(`Where: ${LOCAL_PATH}:12`);
    expect(text).toContain(`  and: web ${LOCAL_PATH}`);
    const u = issueURL(FORM, f, "graphvein dev");
    const shown = new URL(u).searchParams.get("shown") ?? "";
    expect(shown).toBe(text);
    for (const leak of ["billing", "corp.internal", "8443", "/home/", "someone", "C:\\", "\\Users"]) {
      expect(shown).not.toContain(leak);
    }
    expect(redactText("origin api.internal:9000 assumed to be web's server")).toBe(`origin ${HOST} assumed to be web's server`);
    expect(redactText("origin (not resolved statically) assumed to be this repository's server")).toBe("origin (not resolved statically) assumed to be this repository's server");
    expect(redactText("`https://${host}/api/v1/items`")).toBe(`\`${HOST}/api/v1/items\``);
    expect(redactText('axios.get("//cdn.example.com/a")')).toBe(`axios.get("${HOST}/a")`);
    // JX-R2 B1: a scheme built in code.
    expect(redactText("fetch(`${window.location.protocol}//internal-api.corp:8080/api/x`)")).toBe(`fetch(\`\${window.location.protocol}${HOST}/api/x\`)`);
    expect(redactText("fetch(`${proto}://api.corp.internal/x`)")).toBe(`fetch(\`\${proto}${HOST}/x\`)`);
    expect(redactText('fetch(`http${secure?"s":""}://api.corp.internal/x`)')).toBe(`fetch(\`http\${secure?"s":""}${HOST}/x\`)`);
    expect(redactText('fetch(location.protocol + "//" + host + "/x")')).toBe(`fetch(location.protocol + "//" + ${HOST} + "/x")`);
    expect(redactText("fetch(window.location.protocol + '//' + cfg.apiHost + '/x')")).toBe(`fetch(window.location.protocol + '//' + ${HOST} + '/x')`);
    expect(redactText('fetch(location.protocol + "//" + "api.corp:8080/x")')).toBe(`fetch(location.protocol + "//" + "${HOST}/x")`);
    expect(redactText('get("https://" + "api.corp.internal" + path)')).toBe(`get("${HOST}" + "${HOST}" + path)`);
    expect(redactText("url = proto + '://' + 'api.corp' + '/x'")).toBe(`url = proto + '://' + '${HOST}' + '/x'`);
    for (const t of ["fetch(`${window.location.protocol}//internal-api.corp:8080/api/x`)", "fetch(`${proto}://api.corp.internal/x`)", 'fetch(`http${secure?"s":""}://api.corp.internal/x`)']) {
      expect(redactText(t)).not.toMatch(/corp/);
    }
    expect(redactText("open file:///Users/someone/x.json")).toBe(`open ${LOCAL_PATH}`);
    // Relative paths and URL paths stay as stored.
    expect(redactText("GET /api/users/{id} src/a.ts:3")).toBe("GET /api/users/{id} src/a.ts:3");
  });

  // B2: the link stays under 8000 characters, cut at a line boundary.
  it("caps the issue link and cuts the text at a line boundary", () => {
    const short = issueLink(FORM, FACT, "graphvein dev");
    expect(short.shortened).toBe(false);
    const where = Array.from({ length: 300 }, (_, i) => `react src/pages/very/long/path/number/${String(i)}/article.loader.ts:${String(i)}`);
    const big: WrongFact = { ...FACT, where };
    const link = issueLink(FORM, big, "graphvein dev");
    expect(link.shortened).toBe(true);
    expect(link.url.length).toBeLessThanOrEqual(MAX_ISSUE_URL);
    expect(link.url.length).toBeLessThan(8000);
    const sent = new URL(link.url).searchParams.get("shown") ?? "";
    const lines = sent.split("\n");
    expect(lines[lines.length - 1]).toBe(SHORTENED_LINE);
    // Whole lines of the full text, in order: the later "and:" lines go
    // first, the Version line is kept (JX-R2 B2).
    const full = shownText(big, "graphvein dev").split("\n");
    const body = lines.slice(0, -1);
    expect(body.slice(0, -1)).toEqual(full.slice(0, body.length - 1));
    expect(body[body.length - 1]).toBe("Version: graphvein dev");
    expect(lines.length).toBeGreaterThan(4);
  });

  // JX-R2 B2: one overlong line is cut within the line; every field keeps
  // at least its first line.
  it("cuts an overlong line and keeps the first line of every field", () => {
    const edges = Array.from({ length: 400 }, (_, i) => `calls_route project link inferred 0.7 edge ${String(i)}`).join("; ");
    expect(edges.length).toBeGreaterThan(14000);
    const where = Array.from({ length: 200 }, (_, i) => `react src/pages/p${String(i)}/article.loader.ts:${String(i)}`);
    const link = issueLink(FORM, { ...FACT, rule: edges, where }, "graphvein dev");
    expect(link.shortened).toBe(true);
    expect(link.url.length).toBeLessThanOrEqual(MAX_ISSUE_URL);
    // Far more than the fact alone (the re-check's 222 characters).
    expect(link.url.length).toBeGreaterThan(MAX_ISSUE_URL - 400);
    const lines = (new URL(link.url).searchParams.get("shown") ?? "").split("\n");
    expect(lines[0]).toBe(`Fact: ${FACT.fact}`);
    expect(lines[1]).toBe("Shown as: Inferred across repositories");
    expect(lines[2]?.startsWith("Rule: calls_route project link inferred 0.7 edge 0; ")).toBe(true);
    expect(lines[2]?.endsWith("…")).toBe(true);
    expect(lines[3]).toBe("Where: react src/pages/p0/article.loader.ts:0");
    expect(lines[lines.length - 2]).toBe("Version: graphvein dev");
    expect(lines[lines.length - 1]).toBe(SHORTENED_LINE);

    const fact = "x".repeat(6000) + " é".repeat(500);
    const one = issueLink(FORM, { ...FACT, fact }, "graphvein dev");
    expect(one.url.length).toBeLessThanOrEqual(MAX_ISSUE_URL);
    const sent = (new URL(one.url).searchParams.get("shown") ?? "").split("\n");
    expect(sent[0]?.startsWith("Fact: xxxx")).toBe(true);
    expect(sent[0]?.endsWith("…")).toBe(true);
    expect(sent[0]?.length).toBeGreaterThan(3000);
    expect(sent.slice(1)).toEqual(["Shown as: Inferred across repositories", `Rule: ${FACT.rule ?? ""}`, `Where: ${FACT.where[0] ?? ""}`, `  and: ${FACT.where[1] ?? ""}`, "Version: graphvein dev", SHORTENED_LINE]);
  });
});
