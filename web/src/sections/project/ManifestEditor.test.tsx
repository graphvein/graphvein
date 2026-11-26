import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { EditResult, Manifest, Suggestions } from "@/api/manifest";
import { LabelProvider } from "@/hooks/useLabels";
import { LABELS_KEY } from "@/lib/labelChoice";
import { ManifestEditor } from "@/sections/project/ManifestEditor";

// The react + express case of T022 B6c: react's Orval calls go through
// __API_URL__, which env files, webpack DefinePlugin and a project Compose
// port tie to express.
const MANIFEST: Manifest = {
  project: "react-published",
  file: "graphvein-project.yaml",
  hash: "sha256:base",
  content: "",
  token: "tok-1",
  members: [
    { repository: "react", repository_source: "manifest", checkout: true, consumes: [], scopes: [] },
    {
      repository: "express",
      repository_source: "member graphvein.yaml",
      checkout: true,
      consumes: [],
      scopes: [{ name: "app", path: "apps/app", consumes: [] }],
    },
  ],
};

const SUGGESTIONS: Suggestions = {
  hash: "sha256:base",
  proposals: [
    {
      number: 1,
      member: "react",
      provider: "express",
      select: { bases: ["__API_URL__"] },
      evidence: [
        { row: "SG-E1", member: "react", file: ".env.development", line: 1, key: "API_URL", value: "http://localhost:30400/api" },
        { row: "SG-C1", member: "", file: "compose.yaml", line: 6, key: "express ports", value: "30400" },
      ],
      declared: false,
    },
  ],
};

const DIFF = "--- a/graphvein-project.yaml\n+++ b/graphvein-project.yaml\n@@ -3,2 +3,6 @@\n   - repository: react\n     path: react\n+    consumes:\n+      - repository: express\n";

function result(over: Partial<EditResult> = {}): EditResult {
  return {
    applied: false,
    base_hash: "sha256:base",
    result_hash: "sha256:next",
    diff: DIFF,
    valid: true,
    problems: [],
    changes: ["accepts suggestion 1: react consumes express (bases: __API_URL__)"],
    reindex: { kind: "link_pass", reason: "only declarations change: the link pass re-links the stored indexes" },
    ...over,
  };
}

interface Call {
  path: string;
  method: string;
  body: unknown;
  token: string | null;
}

/** serve answers the editor's requests; post decides each POST's answer. */
function serve(post: (path: string, body: Record<string, unknown>) => unknown): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = typeof init?.body === "string" ? (JSON.parse(init.body) as Record<string, unknown>) : {};
      const headers = (init?.headers ?? {}) as Record<string, string>;
      calls.push({ path, method, body, token: headers["X-Graphvein-Token"] ?? null });
      const answer = method === "POST" ? post(path, body) : path.startsWith("/api/v0/project/suggestions") ? SUGGESTIONS : MANIFEST;
      return Promise.resolve(new Response(JSON.stringify(answer), { status: 200 }));
    }),
  );
  return calls;
}

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

describe("manifest editor (T022 B10)", () => {
  it("accepts a suggestion only after showing its diff and a confirm, with the base hash, the previewed result and the token", async () => {
    const calls = serve((path, body) => (path === "/api/v0/index/run" ? {} : result({ applied: body.apply === true })));
    const written = vi.fn();
    render(<ManifestEditor onWritten={written} />);
    fireEvent.click(await screen.findByRole("button", { name: "Read suggestions" }));
    const proposal = await screen.findByTestId("proposal");
    expect(proposal.textContent).toContain("1. react consumes express (select: bases __API_URL__)");
    expect(proposal.textContent).toContain("react: .env.development:1 API_URL=http://localhost:30400/api (SG-E1)");
    expect(proposal.textContent).toContain("(project folder): compose.yaml:6");
    fireEvent.click(within(proposal).getByRole("button", { name: "Accept" }));
    const dialog = await screen.findByRole("dialog", { name: "Manifest diff preview" });
    expect(screen.getByTestId("manifest-diff").textContent).toContain("+      - repository: express");
    expect(dialog.textContent).toContain("accepts suggestion 1");
    expect(dialog.textContent).toContain("the link pass re-links the stored indexes");
    // Nothing is written before the confirm.
    const preview = calls.filter((c) => c.method === "POST");
    expect(preview).toHaveLength(1);
    expect(preview[0]?.body).toEqual({ base_hash: "sha256:base", operations: [{ op: "accept_suggestion", number: 1 }] });
    expect(preview[0]?.token).toBe("tok-1");
    expect(written).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Write graphvein-project.yaml" }));
    await screen.findByRole("status");
    const apply = calls.filter((c) => c.method === "POST")[1];
    expect(apply?.body).toEqual({ base_hash: "sha256:base", operations: [{ op: "accept_suggestion", number: 1 }], apply: true, result_hash: "sha256:next" });
    expect(written).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
    // The offered re-index is the link pass only: no member changed.
    fireEvent.click(screen.getByRole("button", { name: "Run the link pass" }));
    await waitFor(() => {
      expect(calls.at(-1)?.path).toBe("/api/v0/index/run");
    });
    expect(calls.at(-1)?.body).toEqual({ link_pass_only: true });
  });

  it("shows an invalid result's problems and never offers to write it; Cancel writes nothing", async () => {
    const calls = serve(() => result({ valid: false, problems: ["graphvein-project.yaml: line 10: consumes names scope db, which express does not declare"] }));
    render(<ManifestEditor onWritten={vi.fn()} />);
    fireEvent.change(await screen.findByLabelText("consumer"), { target: { value: "react" } });
    fireEvent.change(screen.getByLabelText("provider"), { target: { value: "express#" } });
    fireEvent.change(screen.getByLabelText("path prefixes"), { target: { value: "/api, /v2" } });
    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));
    const dialog = await screen.findByRole("dialog");
    expect(calls.at(-1)?.body).toEqual({
      base_hash: "sha256:base",
      operations: [{ op: "add_consume", member: "react", consume: { repository: "express", default_scope: true, select: { path_prefixes: ["/api", "/v2"] } } }],
    });
    expect(dialog.textContent).toContain("Not valid, so it cannot be written");
    expect(within(dialog).getByRole<HTMLButtonElement>("button", { name: "Write graphvein-project.yaml" }).disabled).toBe(true);
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  });

  it("declares a scope by a folder inside a member, renames only a member the manifest names, and offers a project re-index after a rename", async () => {
    const calls = serve((_, body) => {
      const ops = body.operations as { op: string }[];
      return result({ applied: body.apply === true, reindex: ops[0]?.op === "rename_repository" ? { kind: "project", reason: "a member is renamed" } : result().reindex });
    });
    render(<ManifestEditor onWritten={vi.fn()} />);
    fireEvent.change(await screen.findByLabelText("member"), { target: { value: "express" } });
    fireEvent.change(screen.getByLabelText("scope name"), { target: { value: "user" } });
    fireEvent.change(screen.getByLabelText("folder inside the member"), { target: { value: "apps/user" } });
    fireEvent.click(screen.getByRole("button", { name: "Add scope" }));
    await screen.findByRole("dialog");
    expect(calls.at(-1)?.body).toMatchObject({ operations: [{ op: "add_scope", member: "express", name: "user", path: "apps/user" }] });
    // express's name comes from its own graphvein.yaml: no rename offered.
    expect(screen.getAllByRole("button", { name: "Rename" })).toHaveLength(1);
    expect(screen.getByText("name from its graphvein.yaml")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    fireEvent.change(screen.getByLabelText("New name"), { target: { value: "web" } });
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await waitFor(() => {
      expect(calls.at(-1)?.body).toMatchObject({ operations: [{ op: "rename_repository", member: "react", to: "web" }] });
    });
    fireEvent.click(await screen.findByRole("button", { name: "Write graphvein-project.yaml" }));
    expect(await screen.findByRole("button", { name: "Re-index the project" })).toBeTruthy();
  });

  it("names the stored index a rename deletes and writes only after its explicit second confirm", async () => {
    const retiring = [{ repository: "react", stored: true, nodes: 412, projects: [], standalone: true, confirm: "delete stored index of react (412 nodes)" }];
    const calls = serve((_, body) =>
      result({ applied: body.apply === true, retiring, reindex: { kind: "project", reason: "a member is renamed" }, changes: ["renames repository react to web, in every consumes entry too"] }),
    );
    render(<ManifestEditor onWritten={vi.fn()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Rename" }));
    fireEvent.change(screen.getByLabelText("New name"), { target: { value: "web" } });
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    const box = await screen.findByTestId("retiring");
    expect(box.textContent).toContain("This rename deletes the stored index of react (412 nodes) from the store shared by all projects");
    expect(box.textContent).toContain("may be the store's only copy");
    const write = screen.getByRole<HTMLButtonElement>("button", { name: "Write graphvein-project.yaml" });
    expect(write.disabled).toBe(true);
    fireEvent.click(within(box).getByRole("checkbox", { name: "delete stored index of react (412 nodes)" }));
    expect(write.disabled).toBe(false);
    fireEvent.click(write);
    await screen.findByRole("status");
    expect(calls.filter((c) => c.method === "POST").at(-1)?.body).toMatchObject({ apply: true, confirm_delete: ["react"] });
  });

  it("uses the label table: technical style shows operation and field names", async () => {
    serve(() => result());
    window.localStorage.setItem(LABELS_KEY, "technical");
    render(
      <LabelProvider fetcher={() => Promise.resolve({ api_version: 0, labels: null, error: null })}>
        <ManifestEditor onWritten={vi.fn()} />
      </LabelProvider>,
    );
    expect(await screen.findByRole("button", { name: "add_consume" })).toBeTruthy();
    expect(screen.getByLabelText("path_prefixes")).toBeTruthy();
  });
});
