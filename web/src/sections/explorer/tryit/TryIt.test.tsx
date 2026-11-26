// Try it out in the Explorer (T029 part B) with a fake server that plays
// the server-enforced flows: confirmation required, production
// acknowledgement, auth missing, an untrusted or changed certificate, a
// redirect to follow and a truncated response. The UI can only ask and
// forward; these tests check it asks, forwards exactly, and never shows a
// secret.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import synthetic from "../../../../../internal/openapi/testdata/synthetic.golden.json";

import { ApiError, setRequestRepository } from "@/api/client";
import { parseOpenApi, type ExplorerClient } from "@/api/openapi";
import { parseEnvironments, type EditAnswer, type Environments, type Prepared, type Sent, type TryItClient, type TryRequest } from "@/api/tryit";
import type { StatusView } from "@/hooks/useStatus";
import { ExplorerSection } from "@/sections/explorer/ExplorerSection";
import { resetProjectChoice } from "@/sections/explorer/tryit/TryIt";
import { ProjectProvider } from "@/shell/project";
import { goldenStatus } from "@/test/fixtures";
import { project } from "@/test/projectFixtures";
import { ENVIRONMENTS, prepared } from "@/test/tryitFixtures";

function ok(): StatusView {
  return { kind: "ok", status: goldenStatus("fresh") };
}

const explorer: ExplorerClient = {
  document: () => Promise.resolve({ kind: "document", document: parseOpenApi(structuredClone(synthetic)) }),
  memberDocument: () => Promise.resolve(parseOpenApi(structuredClone(synthetic))),
  text: () => Promise.resolve(""),
  pathValues: () => Promise.resolve({ file: "graphvein.yaml", exists: true, hash: "h", editable: true, reason: null, token: "t", values: [], unresolved: [], mounts: [] }),
  editPathValues: () => Promise.reject(new Error("not used")),
  operationTags: () => Promise.resolve(new Map()),
};

interface RawEnv {
  id: string;
  name: string;
  label: string;
  production: boolean;
  source?: string;
  base_url?: string;
  auth?: Record<string, unknown>;
  options?: Record<string, unknown>;
}

/** FakeServer keeps the session state the server keeps (selection,
 * acknowledgement, grants) and records every call. */
class FakeServer {
  envs: RawEnv[];
  selected: string | null = null;
  acknowledged = false;
  granted = false;
  calls: { name: string; body: unknown }[] = [];
  /** prepare and send answers; tests replace them. */
  /** A value typed for the session is held. */
  session = false;
  /** Project mode: the member environments() answers for, each member's
   * base URL of a project environment (a member without one has no such
   * environment), and the manifest's hash in the listing's files. */
  member = "api";
  memberUrls: Record<string, string> | null = null;
  manifest: string | null = null;
  onPrepare: (req: TryRequest) => Prepared = (req) => prepared({ method: req.method, freeForm: req.free_form === true, confirmationRequired: req.method !== "GET" || req.free_form === true, confirmationReasons: req.free_form === true ? ["free_form_path"] : req.method !== "GET" ? ["method"] : [], confirmation: req.method !== "GET" || req.free_form === true ? "c1" : null });
  onSend: (req: TryRequest) => Sent = (req) => sent({ method: req.method });

  constructor(envs: RawEnv[]) {
    this.envs = envs;
  }

  listing(repository = "api"): Environments {
    const raw = structuredClone(ENVIRONMENTS) as Record<string, unknown>;
    const base = ENVIRONMENTS.environments[0];
    raw.repository = repository;
    raw.selected = this.selected;
    const urls = this.memberUrls;
    if (this.manifest !== null) raw.files = [...ENVIRONMENTS.files, { target: "project", file: "graphvein-project.yaml", exists: true, hash: this.manifest, writable: true }];
    raw.environments = this.envs.filter((e) => e.source !== "project" || urls === null || urls[repository] !== undefined).map((e) => ({
      ...base,
      ...e,
      base_url: e.source === "project" && urls !== null ? urls[repository] : (e.base_url ?? base?.base_url),
      source: e.source ?? "repository",
      auth: { ...base?.auth, ...e.auth },
      options: e.options ?? { tls: null, use_system_proxy: false, limits: null },
      selected: this.selected === e.id,
      acknowledged: this.selected === e.id && this.acknowledged,
      remembered: this.selected === e.id && this.granted,
    }));
    return parseEnvironments(raw);
  }

  client(): TryItClient {
    const record = (name: string, body: unknown) => {
      this.calls.push({ name, body: structuredClone(body) });
    };
    return {
      environments: () => Promise.resolve(this.listing(this.member)),
      memberEnvironments: (repository) => Promise.resolve(this.listing(repository)),
      suggestions: () =>
        Promise.resolve({
          repository: "api",
          notice: "suggested, not used until you pick one",
          suggestions: [
            { url: "http://127.0.0.1:8002", sourceKind: "compose_published_port", row: "TI-31", file: "docker-compose.yml", line: 2, reason: "Compose service api publishes 8000 on 8002" },
            { url: "http://127.0.0.1:8000", sourceKind: "framework_default", row: "TI-41", file: null, line: null, reason: "framework default, not read from your code" },
          ],
        }),
      edit: (body, token) => {
        record("edit", { ...body, token });
        const b = body as { apply: boolean; environment?: { name: string; label?: string; base_url: string }; options?: { name: string } };
        const answer: EditAnswer = { applied: b.apply, target: "repository", file: "graphvein.yaml", baseHash: "sha256:abc", resultHash: "sha256:result", diff: "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n+    - name: x", valid: true, problems: [], change: "adds environment", environment: b.environment !== undefined ? `repository:${b.environment.name}` : `repository:${b.options?.name ?? ""}` };
        if (b.apply && b.environment !== undefined) {
          const label = b.environment.label ?? "local";
          this.envs.push({ id: `repository:${b.environment.name}`, name: b.environment.name, label, production: label === "production", base_url: b.environment.base_url });
        }
        if (b.apply && b.options !== undefined) {
          // A trust change is a new destination: the server drops the selection.
          this.selected = null;
          this.acknowledged = false;
        }
        return Promise.resolve(answer);
      },
      select: (body, token) => {
        record("select", { ...body, token });
        if (this.selected !== body.environment) {
          this.acknowledged = false;
          this.granted = false;
        }
        this.selected = body.environment === "" ? null : body.environment;
        if (body.acknowledge_production) this.acknowledged = true;
        const e = this.envs.find((x) => x.id === body.environment);
        return Promise.resolve({ selected: this.selected, production: e?.production === true, acknowledged: this.acknowledged, acknowledgementRequired: e?.production === true && !this.acknowledged, remembered: this.granted });
      },
      auth: (body, token) => {
        record("auth", { ...body, token });
        this.session = body.clear !== true;
        return Promise.resolve({ environment: body.environment, header: "Authorization", sessionValue: body.clear !== true });
      },
      prepare: (body, token) => {
        record("prepare", { ...body, token });
        try {
          return Promise.resolve(this.onPrepare(body));
        } catch (err) {
          return Promise.reject(err instanceof Error ? err : new Error(String(err)));
        }
      },
      send: (body, token) => {
        record("send", { ...body, token });
        if (body.remember === true) this.granted = true;
        try {
          return Promise.resolve(this.onSend(body));
        } catch (err) {
          return Promise.reject(err instanceof Error ? err : new Error(String(err)));
        }
      },
    };
  }

  names(): string[] {
    return this.calls.map((c) => c.name);
  }

  last(name: string): Record<string, unknown> {
    const c = [...this.calls].reverse().find((x) => x.name === name);
    if (c === undefined) throw new Error(`no ${name} call`);
    return c.body as Record<string, unknown>;
  }
}

function sent(over: Partial<Sent> = {}): Sent {
  return {
    environment: "repository:local",
    method: "GET",
    url: "http://127.0.0.1:8000/users",
    status: 200,
    statusText: "OK",
    headers: [
      { name: "Content-Type", value: "application/json" },
      { name: "Set-Cookie", value: "[redacted]" },
    ],
    body: '{"data":[1]}',
    bodyEncoding: "text",
    bodyBytes: 12,
    truncated: false,
    truncatedReason: null,
    truncatedLimit: null,
    redirect: null,
    redirectFollow: null,
    durationMs: 7,
    remembered: false,
    ...over,
  };
}

const LOCAL: RawEnv = { id: "repository:local", name: "local", label: "local", production: false };
const PROD: RawEnv = { id: "repository:prod", name: "prod", label: "production", production: true, base_url: "https://api.example.com" };

beforeEach(() => {
  window.history.replaceState(null, "", "#/explorer");
  resetProjectChoice();
});

afterEach(() => {
  setRequestRepository(null);
});

async function openTryIt(server: FakeServer, opId: string) {
  render(<ExplorerSection status={ok()} client={explorer} tryItClient={server.client()} />);
  return expandTryIt(opId);
}

/** expandTryIt expands an operation of the page shown and opens Try it out. */
async function expandTryIt(opId: string) {
  const row = (await screen.findAllByTestId("operation")).find((r) => r.getAttribute("data-op") === opId);
  if (row === undefined) throw new Error(`no row ${opId}`);
  const toggle = row.querySelector("button");
  if (toggle === null) throw new Error("no toggle");
  fireEvent.click(toggle);
  fireEvent.click(within(row).getByTestId("try-it-slot"));
  return within(row).findByTestId("tryit-panel");
}

async function choose(id: string) {
  fireEvent.click(await screen.findByTestId("environment-trigger"));
  const option = (await screen.findAllByTestId("environment-option")).find((o) => o.getAttribute("data-id") === id);
  if (option === undefined) throw new Error(`no option ${id}`);
  fireEvent.click(option);
}

describe("Try it out (T029 part B)", () => {
  it("opens the environment picker for a Settings link (edit=environments, T039 ST-F)", async () => {
    window.history.replaceState(null, "", "#/explorer?edit=environments");
    const server = new FakeServer([LOCAL]);
    render(<ExplorerSection status={ok()} client={explorer} tryItClient={server.client()} />);
    expect((await screen.findAllByTestId("environment-option")).map((o) => o.getAttribute("data-id"))).toEqual(["repository:local"]);
  });

  it("says why it cannot try without an environment, and adds one from a suggestion through preview and one confirmation", async () => {
    const server = new FakeServer([]);
    const panel = await openTryIt(server, "GET /users");
    expect((await within(panel).findByTestId("tryit-unavailable")).textContent).toContain("No environment is configured");
    fireEvent.click(screen.getByTestId("environment-trigger"));
    const suggestions = await screen.findAllByTestId("suggestion");
    expect(suggestions[0]?.textContent).toContain("Compose published port");
    expect(suggestions[0]?.textContent).toContain("docker-compose.yml:2");
    expect(suggestions[1]?.textContent).toContain("Framework default, not read from your code");
    fireEvent.click(within(suggestions[0] as HTMLElement).getByTestId("suggestion-use"));
    const dialog = await screen.findByTestId("add-environment");
    expect(within(dialog).getByTestId<HTMLInputElement>("add-url").value).toBe("http://127.0.0.1:8002");
    // Nothing is written before the preview; the preview writes nothing.
    expect(within(dialog).queryByTestId("add-apply")).toBeNull();
    fireEvent.click(within(dialog).getByTestId("add-preview"));
    await within(dialog).findByTestId("edit-diff");
    expect(server.last("edit")).toMatchObject({ apply: false, base_hash: "sha256:abc", environment: { base_url: "http://127.0.0.1:8002", label: "local" } });
    fireEvent.click(within(dialog).getByTestId("add-apply"));
    await waitFor(() => {
      expect(server.last("edit")).toMatchObject({ apply: true, result_hash: "sha256:result" });
    });
    // The new environment is selected and the panel is ready.
    await waitFor(() => {
      expect(server.selected).toBe("repository:127.0.0.1-8002");
    });
    await within(panel).findByTestId("tryit-send");
  });

  it("sends GET on a local environment directly and shows redacted headers and re-indented JSON", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const response = await within(panel).findByTestId("tryit-response");
    expect(server.names().filter((n) => n === "prepare" || n === "send")).toEqual(["prepare", "send"]);
    expect(server.last("send").confirmation).toBeUndefined();
    expect(within(response).getByTestId("response-status").textContent).toBe("200 OK");
    expect(within(response).getByTestId("response-body").textContent).toBe('{\n  "data": [\n    1\n  ]\n}');
    expect(within(response).getByTestId("response-headers").textContent).toContain("redacted");
    // The auth header was sent from the variable; its value is never on the page.
    expect(within(response).getByTestId("request-headers").textContent).toContain("from $API_TOKEN");
  });

  it("asks before POST with the exact request and reasons, and forwards don't ask again", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "POST /api/items/");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const confirm = await within(panel).findByTestId("tryit-confirm");
    expect(server.names()).not.toContain("send");
    expect(within(confirm).getByTestId("confirm-request").textContent).toContain("POST http://127.0.0.1:8000/api/items/");
    expect(within(confirm).getByTestId("confirm-reasons").textContent).toContain("the method may change data");
    fireEvent.click(within(confirm).getByTestId("confirm-remember"));
    fireEvent.click(within(confirm).getByTestId("confirm-send"));
    await within(panel).findByTestId("tryit-response");
    expect(server.last("send")).toMatchObject({ confirmation: "c1", remember: true, method: "POST" });
  });

  it("shows every confirmation reason the server gives: method override and body on GET", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    server.onPrepare = (req) => prepared({ method: req.method, confirmationReasons: ["method_override", "body_on_get_or_head"], confirmation: "c2" });
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const reasons = await within(panel).findByTestId("confirm-reasons");
    expect([...reasons.querySelectorAll("li")].map((l) => l.getAttribute("data-reason"))).toEqual(["method_override", "body_on_get_or_head"]);
    expect(reasons.textContent).toContain("a method override turns this GET or HEAD into another method");
  });

  it("sends a free-form path only after its confirmation, never with don't ask again", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("free-form-toggle"));
    fireEvent.change(within(panel).getByTestId("free-form-path"), { target: { value: "/admin/users/7" } });
    fireEvent.click(within(panel).getByTestId("tryit-send"));
    const confirm = await within(panel).findByTestId("tryit-confirm");
    expect(within(confirm).queryByTestId("confirm-remember")).toBeNull();
    expect(server.last("prepare")).toMatchObject({ free_form: true, path: "/admin/users/7", method: "GET" });
    expect(server.last("prepare").path_values).toBeUndefined();
  });

  it("warns once when switching to production, then marks every operation", async () => {
    const server = new FakeServer([LOCAL, PROD]);
    server.selected = LOCAL.id;
    render(<ExplorerSection status={ok()} client={explorer} tryItClient={server.client()} />);
    await screen.findAllByTestId("operation");
    await choose(PROD.id);
    const dialog = await screen.findByTestId("production-dialog");
    expect(dialog.textContent).toContain("even a GET can change data");
    // Cancel keeps the old environment: nothing is recorded.
    fireEvent.click(within(dialog).getByText("Cancel"));
    expect(server.names()).not.toContain("select");
    await choose(PROD.id);
    fireEvent.click(within(await screen.findByTestId("production-dialog")).getByTestId("production-confirm"));
    await waitFor(() => {
      expect(server.last("select")).toMatchObject({ environment: PROD.id, acknowledge_production: true, token: "tok" });
    });
    await screen.findByTestId("production-marker");
    await waitFor(() => {
      const labels = screen.getAllByTestId("row-env-label");
      expect(labels.length).toBe(screen.getAllByTestId("operation").length);
      expect(labels.every((l) => l.textContent === "production")).toBe(true);
    });
    // No typed name: one click.
    expect(screen.queryByRole("textbox", { name: /type/i })).toBeNull();
  });

  it("re-asks for production when the server says it needs the acknowledgement", async () => {
    const server = new FakeServer([PROD]);
    server.selected = PROD.id;
    server.acknowledged = true;
    server.onSend = () => {
      throw new ApiError("http", "production_acknowledgement_required: acknowledge it", 409, "production_acknowledgement_required");
    };
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const error = await within(panel).findByTestId("tryit-error");
    expect(error.getAttribute("data-code")).toBe("production_acknowledgement_required");
    server.acknowledged = false;
    fireEvent.click(within(error).getByText(/Select prod again/));
    await screen.findByTestId("production-dialog");
  });

  it("offers send without auth or a value for the session when the auth variable is not set", async () => {
    const server = new FakeServer([{ ...LOCAL, auth: { header: "X-Api-Key", env: "API_KEY", env_set: false, prefix: null } }]);
    server.selected = LOCAL.id;
    server.onPrepare = (req) => prepared({ method: req.method, authHeader: null, authSource: null, authMissing: req.send_without_auth === true || server.session ? null : { header: "X-Api-Key", env: "API_KEY" }, sendWithoutAuth: req.send_without_auth === true, confirmationRequired: false, confirmationReasons: [], confirmation: null });
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    let dialog = await screen.findByTestId("auth-missing");
    expect(dialog.textContent).toContain("$API_KEY");
    fireEvent.click(within(dialog).getByTestId("send-without-auth"));
    await within(panel).findByTestId("tryit-response");
    expect(server.last("send")).toMatchObject({ send_without_auth: true });
    // The other way: a value typed for this session, sent to the server
    // and never shown again.
    fireEvent.click(within(panel).getByTestId("tryit-send"));
    dialog = await screen.findByTestId("auth-missing");
    fireEvent.click(within(dialog).getByTestId("enter-value"));
    const auth = await screen.findByTestId("session-auth");
    fireEvent.change(within(auth).getByTestId("session-value"), { target: { value: "s3cret" } });
    fireEvent.click(within(auth).getByTestId("session-save"));
    await waitFor(() => {
      expect(server.last("auth")).toMatchObject({ environment: LOCAL.id, value: "s3cret" });
    });
    await waitFor(() => {
      expect(server.names().at(-1)).toBe("send");
    });
    expect(document.body.textContent).not.toContain("s3cret");
  });

  it("shows an untrusted certificate and pins it through the settings edit", async () => {
    const server = new FakeServer([{ ...LOCAL, base_url: "https://127.0.0.1:8443" }]);
    server.selected = LOCAL.id;
    server.onSend = () => {
      throw new ApiError("http", "upstream_certificate_untrusted: not trusted", 502, "upstream_certificate_untrusted", {
        environment: LOCAL.id,
        origin: "https://127.0.0.1:8443",
        certificate: { subject: "CN=dev box", issuer: "CN=dev box", sans: ["localhost", "127.0.0.1"], not_before: "2026-11-01T00:00:00Z", not_after: "2027-11-01T00:00:00Z", sha256: "9595c84a6a0056655a84aeab1ea353000b20d094d4fdc0247c76540ae5a151df", self_signed: true },
        reason: "x509: certificate signed by unknown authority",
        pinned_sha256: [],
      });
    };
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const dialog = await screen.findByTestId("certificate-dialog");
    const details = within(dialog).getByTestId("certificate-details").textContent;
    for (const s of ["CN=dev box", "self-signed", "localhost, 127.0.0.1", "2026-11-01T00:00:00Z", "2027-11-01T00:00:00Z"]) expect(details).toContain(s);
    // openssl's form, to compare by eye with openssl x509 -fingerprint.
    expect(within(dialog).getByTestId("certificate-sha256").textContent).toBe("95:95:C8:4A:6A:00:56:65:5A:84:AE:AB:1E:A3:53:00:0B:20:D0:94:D4:FD:C0:24:7C:76:54:0A:E5:A1:51:DF");
    expect(dialog.textContent).toContain("openssl x509 -noout -fingerprint -sha256 -in cert.pem prints it in this form");
    expect(within(dialog).getByTestId("certificate-copy")).toBeTruthy();
    fireEvent.click(within(dialog).getByTestId("certificate-trust"));
    const settings = await screen.findByTestId("environment-settings");
    expect(within(settings).getByTestId("pin-list").textContent).toContain("9595c84a6a0056655a84aeab1ea353000b20d094d4fdc0247c76540ae5a151df");
    fireEvent.click(within(settings).getByTestId("settings-preview"));
    await within(settings).findByTestId("edit-diff");
    fireEvent.click(within(settings).getByTestId("settings-apply"));
    await waitFor(() => {
      expect(server.last("edit")).toMatchObject({ apply: true, options: { name: "local", tls: { pins: [{ origin: null, sha256: "9595c84a6a0056655a84aeab1ea353000b20d094d4fdc0247c76540ae5a151df" }], ca_file: null } } });
    });
    // The trust change dropped the selection: the UI selects it again.
    await waitFor(() => {
      expect(server.selected).toBe(LOCAL.id);
    });
  });

  it("shows a changed certificate with the old and new fingerprints and a warning", async () => {
    const server = new FakeServer([{ ...LOCAL, options: { tls: { pins: [{ origin: null, sha256: "1111" }], ca_file: null }, use_system_proxy: false, limits: null } }]);
    server.selected = LOCAL.id;
    server.onSend = () => {
      throw new ApiError("http", "upstream_certificate_changed: changed", 502, "upstream_certificate_changed", {
        environment: LOCAL.id,
        origin: "https://127.0.0.1:8443",
        certificate: { subject: "CN=other", issuer: "CN=Some CA", sans: [], not_before: "a", not_after: "b", sha256: "2222", self_signed: false },
        reason: "x509: certificate signed by unknown authority",
        pinned_sha256: ["1111"],
      });
    };
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const dialog = await screen.findByTestId("certificate-dialog");
    expect(dialog.textContent).toContain("changed");
    expect(within(dialog).getByTestId("certificate-old").textContent).toBe("1111");
    expect(within(dialog).getByTestId("certificate-sha256").textContent).toBe("2222");
    expect(dialog.textContent).toContain("Trust it only if you know why it changed");
    fireEvent.click(within(dialog).getByTestId("certificate-trust"));
    const settings = await screen.findByTestId("environment-settings");
    // The old pin stays listed beside the new one, to remove by hand.
    expect(within(settings).getByTestId("pin-list").textContent).toContain("1111");
    expect(within(settings).getByTestId("pin-list").textContent).toContain("2222");
  });

  it("says which limit cut a response and offers to raise it", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    server.onSend = () => sent({ truncated: true, truncatedReason: "size", truncatedLimit: { limit: "response_bytes", value: 4194304, ceiling: 268435456, hint: "raise" }, body: '{"cut' });
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const cut = await within(panel).findByTestId("response-truncated");
    expect(cut.textContent).toContain("cut at the response size limit");
    expect(cut.textContent).toContain("response size limit 4 MiB, at most 256 MiB");
    fireEvent.click(within(cut).getByTestId("raise-limit"));
    const settings = await screen.findByTestId("environment-settings");
    expect(within(settings).getByTestId<HTMLInputElement>("limit-response_bytes").value).toBe(String(16 * 1024 * 1024));
  });

  it("offers to raise the request limit when the body is too large", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    server.onSend = () => {
      throw new ApiError("http", "body_too_large: over", 413, "body_too_large", { limit: "request_bytes", value: 1048576, ceiling: 67108864, hint: "raise" });
    };
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const error = await within(panel).findByTestId("tryit-error");
    fireEvent.click(within(error).getByTestId("raise-limit"));
    const settings = await screen.findByTestId("environment-settings");
    expect(within(settings).getByTestId<HTMLInputElement>("limit-request_bytes").value).toBe(String(4 * 1024 * 1024));
  });

  it("adds a redirect's origin as an environment and follows it once as a free-form request", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    server.onSend = (req) =>
      req.free_form === true
        ? sent({ url: "https://login.example.com/start?next=a" })
        : sent({
            status: 302,
            statusText: "Found",
            body: "",
            bodyBytes: 0,
            redirect: "https://login.example.com/start?next=a",
            redirectFollow: { url: "https://login.example.com/start?next=a", origin: "https://login.example.com", sameOrigin: false, method: "GET", keepsBody: false, environment: null, path: "/start", query: [{ name: "next", value: "a" }], suggestedEnvironment: { name: "login.example.com-443", base_url: "https://login.example.com" } },
          });
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const redirect = await within(panel).findByTestId("response-redirect");
    expect(redirect.textContent).toContain("https://login.example.com/start?next=a");
    fireEvent.click(within(redirect).getByTestId("redirect-follow"));
    const dialog = await screen.findByTestId("add-environment");
    expect(within(dialog).getByTestId<HTMLInputElement>("add-url").value).toBe("https://login.example.com");
    fireEvent.click(within(dialog).getByTestId("add-preview"));
    fireEvent.click(await within(dialog).findByTestId("add-apply"));
    // Selected, then the target is prepared as a free-form request that
    // asks before it is sent.
    await waitFor(() => {
      expect(server.last("prepare")).toMatchObject({ environment: "repository:login.example.com-443", free_form: true, path: "/start", method: "GET", query: [{ name: "next", value: "a" }] });
    });
    await within(panel).findByTestId("tryit-confirm");
  });

  it("says an http environment's proxy sees the request in clear", async () => {
    const server = new FakeServer([{ ...LOCAL, base_url: "http://10.0.0.5:8000", options: { tls: null, use_system_proxy: true, limits: null } }]);
    server.selected = LOCAL.id;
    server.onPrepare = (req) => prepared({ method: req.method, proxy: "http://proxy.example:3128", proxyCleartext: true });
    const panel = await openTryIt(server, "POST /api/items/");
    expect((await screen.findByTestId("proxy-cleartext-bar")).textContent).toContain("in clear");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const confirm = await within(panel).findByTestId("tryit-confirm");
    expect(confirm.textContent).toContain("http://proxy.example:3128: http://, the proxy sees the request in clear");
    fireEvent.click(within(confirm).getByTestId("confirm-send"));
    expect((await within(panel).findByTestId("proxy-cleartext")).textContent).toContain("in clear");
  });

  it("copies as curl with the variable, never the value", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("copy-curl"));
    const text = (await within(panel).findByTestId("curl-text")).textContent;
    expect(text).toContain("$API_TOKEN");
    expect(server.names()).not.toContain("send");
  });

  it("pre-fills a JSON template marked as a template and offers files for a multipart body", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const doc = parseOpenApi(structuredClone(synthetic));
    const withBody = doc.operations.find((o) => (o.requestBody ?? []).some((m) => m.schema !== null && /json/.test(m.contentType)));
    if (withBody === undefined) throw new Error("no JSON body");
    const panel = await openTryIt(server, `${withBody.method.toUpperCase()} ${withBody.key}`);
    expect((await within(panel).findByTestId("body-template-note")).textContent).toContain("Template");
    fireEvent.change(within(panel).getByTestId("body-mode"), { target: { value: "multipart" } });
    expect(within(panel).getByTestId("part-file")).toBeTruthy();
  });

  /** renderProject shows the Explorer for member (the shell remounts
   * the page when the member changes). */
  function renderProject(server: FakeServer, member: string) {
    window.localStorage.setItem("graphvein.repository.v1.shop", member);
    server.member = member;
    return render(
      <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
        {() => <ExplorerSection status={ok()} client={explorer} tryItClient={server.client()} />}
      </ProjectProvider>,
    );
  }

  /** acknowledgeOnReact switches member react to the project production
   * environment through its one dialog, then leaves the page. */
  async function acknowledgeOnReact(server: FakeServer, listed: string[]) {
    const { unmount } = renderProject(server, "react");
    await screen.findAllByTestId("operation");
    await choose("project:demo");
    const dialog = await screen.findByTestId("production-dialog");
    const members = await within(dialog).findByTestId("production-members");
    await waitFor(() => {
      for (const m of listed) expect(members.textContent).toContain(m);
    });
    fireEvent.click(within(dialog).getByTestId("production-confirm"));
    await screen.findByTestId("production-marker");
    unmount();
    // The next member starts with nothing selected there.
    server.selected = null;
    server.acknowledged = false;
  }

  const DEMO: RawEnv = { ...PROD, id: "project:demo", name: "demo", source: "project" };

  it("acknowledges a project production environment once for every member it covers", async () => {
    const server = new FakeServer([DEMO]);
    server.memberUrls = { react: "https://react.example.com", express: "https://express.example.com", "axum-ddd": "https://axum.example.com" };
    server.manifest = "sha256:m1";
    await acknowledgeOnReact(server, ["react https://react.example.com", "express https://express.example.com", "axum-ddd https://axum.example.com"]);
    // Another member the dialog listed with the same base URL: selected
    // there, acknowledged, without a second dialog.
    const before = server.calls.length;
    renderProject(server, "express");
    await screen.findByTestId("production-marker");
    expect(server.calls.slice(before).filter((c) => c.name === "select").map((c) => c.body)).toEqual([{ environment: "project:demo", acknowledge_production: true, token: "tok" }]);
    expect(screen.queryByTestId("production-dialog")).toBeNull();
    window.localStorage.clear();
  });

  it("asks again in project mode for a member the warning did not list, a changed base URL or a changed manifest", async () => {
    const cases: { name: string; change: (s: FakeServer) => void; reason: string }[] = [
      {
        name: "member added after the warning",
        change: (s) => {
          s.memberUrls = { ...s.memberUrls, express: "http://127.0.0.2:9082" };
        },
        reason: "The warning you confirmed did not list express; demo sends its requests to http://127.0.0.2:9082.",
      },
      {
        name: "base URL changed",
        change: (s) => {
          s.memberUrls = { ...s.memberUrls, "axum-ddd": "https://moved.example.com" };
        },
        reason: "demo's base URL for axum-ddd changed since you confirmed: it was https://axum.example.com, it is now https://moved.example.com.",
      },
      {
        name: "manifest changed",
        change: (s) => {
          s.manifest = "sha256:m2";
        },
        reason: "The project manifest changed since you confirmed this environment.",
      },
    ];
    for (const c of cases) {
      resetProjectChoice();
      const server = new FakeServer([DEMO]);
      server.memberUrls = { react: "https://react.example.com", "axum-ddd": "https://axum.example.com" };
      server.manifest = "sha256:m1";
      await acknowledgeOnReact(server, ["react https://react.example.com", "axum-ddd https://axum.example.com"]);
      c.change(server);
      const member = c.name === "member added after the warning" ? "express" : "axum-ddd";
      const before = server.calls.length;
      const { unmount } = renderProject(server, member);
      const dialog = await screen.findByTestId("production-dialog");
      expect(within(dialog).getByTestId("production-reason").textContent, c.name).toBe(c.reason);
      // Nothing is selected until the user confirms again.
      expect(server.calls.slice(before).filter((x) => x.name === "select"), c.name).toEqual([]);
      fireEvent.click(within(dialog).getByTestId("production-confirm"));
      await screen.findByTestId("production-marker");
      expect(server.last("select"), c.name).toMatchObject({ environment: "project:demo", acknowledge_production: true });
      unmount();
      window.localStorage.clear();
    }
  });

  it("closes an open confirmation when the environment changes, and offers the right action after a refused send", async () => {
    const STAGE: RawEnv = { id: "repository:stage", name: "stage", label: "staging", production: false, base_url: "http://127.0.0.1:9082" };
    const server = new FakeServer([LOCAL, PROD, STAGE]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "POST /api/items/");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    await within(panel).findByTestId("tryit-confirm");
    await choose(PROD.id);
    fireEvent.click(within(await screen.findByTestId("production-dialog")).getByTestId("production-confirm"));
    const dropped = await within(panel).findByTestId("confirm-dropped");
    expect(dropped.textContent).toBe("The confirmation for POST to local was closed because you switched to prod. Nothing was sent. Press Send to prepare it for prod.");
    expect(within(panel).queryByTestId("tryit-confirm")).toBeNull();
    expect(server.names()).not.toContain("send");

    // A send refused because the selection was lost: select the same
    // environment again; after a switch: prepare it for the new one.
    server.onSend = () => {
      throw new ApiError("http", "environment_not_selected: select it first", 409, "environment_not_selected");
    };
    await choose(STAGE.id);
    await waitFor(() => {
      expect(server.selected).toBe(STAGE.id);
    });
    const get = await expandTryIt("GET /users");
    fireEvent.click(await within(get).findByTestId("tryit-send"));
    expect(await within(get).findByTestId("select-again")).toBeTruthy();
    expect(within(get).getByTestId("select-again").textContent).toBe("Select stage again");
    await choose(LOCAL.id);
    const switched = await within(get).findByTestId("switched");
    expect(switched.textContent).toContain("This request was prepared for stage; local is selected now.");
    fireEvent.click(within(switched).getByTestId("prepare-in-selected"));
    await waitFor(() => {
      expect(server.last("prepare")).toMatchObject({ environment: LOCAL.id, method: "GET" });
    });
  });

  it("shows a JSON body exactly as received: re-indented token for token, never re-serialised", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const raw = '{"count": 12345678901234567891, "id": 9007199254740993, "dup": 1, "dup": 2, "price": 1.10}';
    server.onSend = () => sent({ body: raw, bodyBytes: raw.length });
    const panel = await openTryIt(server, "GET /users");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const response = await within(panel).findByTestId("tryit-response");
    expect(within(response).getByTestId("response-body").textContent).toBe('{\n  "count": 12345678901234567891,\n  "id": 9007199254740993,\n  "dup": 1,\n  "dup": 2,\n  "price": 1.10\n}');
    expect(within(response).getByTestId("response-body-head").textContent).toBe("Body · JSON, re-indented");
    expect(within(response).getByTestId("body-duplicate-keys").textContent).toContain('"dup"');
    fireEvent.click(within(response).getByTestId("body-as-received"));
    expect(within(response).getByTestId("response-body").textContent).toBe(raw);
    expect(within(response).getByTestId("response-body-head").textContent).toBe("Body · JSON, as received");

    // A cut JSON body is shown as received, with the reason.
    server.onSend = () => sent({ body: '{"items": [1, 2', bodyBytes: 15 });
    fireEvent.click(within(panel).getByTestId("tryit-send"));
    await waitFor(() => {
      expect(within(panel).getByTestId("response-body").textContent).toBe('{"items": [1, 2');
    });
    expect(within(panel).getByTestId("body-note").textContent).toBe("Body shown as received: incomplete JSON.");
  });

  it("shows the full body SHA-256 in the confirmation", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const sha = "77e3f0f0a19211e5c2f8b1a9d0c3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1";
    server.onPrepare = (req) => prepared({ method: req.method, bodyBytes: 20, bodySha256: sha });
    const panel = await openTryIt(server, "POST /api/items/");
    fireEvent.click(await within(panel).findByTestId("tryit-send"));
    const confirm = await within(panel).findByTestId("tryit-confirm");
    expect(within(confirm).getByTestId("confirm-sha256").textContent).toBe(sha);
    expect(within(confirm).getByRole("button", { name: "Copy the body SHA-256" })).toBeTruthy();
  });

  it("moves focus into the panel on Try it out and back to Send after Cancel", async () => {
    const server = new FakeServer([LOCAL]);
    server.selected = LOCAL.id;
    const panel = await openTryIt(server, "POST /api/items/");
    const request = within(panel).getByTestId("tryit-request");
    await waitFor(() => {
      expect(request.contains(document.activeElement)).toBe(true);
    });
    expect(document.activeElement).toBe(request.querySelector("input, select, textarea"));
    fireEvent.click(within(panel).getByTestId("tryit-send"));
    const confirm = await within(panel).findByTestId("tryit-confirm");
    const cancel = within(confirm).getByRole("button", { name: "Cancel" });
    expect(document.activeElement).toBe(cancel);
    fireEvent.click(cancel);
    await waitFor(() => {
      expect(document.activeElement).toBe(within(panel).getByTestId("tryit-send"));
    });
  });

  it("moves through the environment listbox and the label radiogroup with the arrow keys", async () => {
    const server = new FakeServer([LOCAL, PROD]);
    server.selected = LOCAL.id;
    render(<ExplorerSection status={ok()} client={explorer} tryItClient={server.client()} />);
    fireEvent.click(await screen.findByTestId("environment-trigger"));
    const listbox = await screen.findByRole("listbox", { name: "Environments" });
    const options = within(listbox).getAllByRole("option");
    expect(options.map((o) => o.tabIndex)).toEqual([0, -1]);
    options[0]?.focus();
    fireEvent.keyDown(listbox, { key: "ArrowDown" });
    expect(document.activeElement).toBe(options[1]);
    fireEvent.keyDown(listbox, { key: "ArrowDown" });
    expect(document.activeElement).toBe(options[0]);
    fireEvent.keyDown(listbox, { key: "End" });
    expect(document.activeElement).toBe(options[1]);
    // Moving focus selects nothing: no switch, no production warning.
    expect(server.names()).not.toContain("select");
    expect(screen.queryByTestId("production-dialog")).toBeNull();

    fireEvent.change(screen.getByTestId("picker-url"), { target: { value: "https://api.example.org" } });
    fireEvent.submit(screen.getByTestId("picker-url").closest("form") as HTMLFormElement);
    const dialog = await screen.findByTestId("add-environment");
    const group = within(dialog).getByRole("radiogroup", { name: "Label" });
    const radios = within(group).getAllByRole("radio");
    expect(radios.map((r) => r.tabIndex)).toEqual([0, -1, -1]);
    radios[0]?.focus();
    fireEvent.keyDown(group, { key: "ArrowRight" });
    expect(document.activeElement).toBe(radios[1]);
    expect(radios[1]?.getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(group, { key: "ArrowLeft" });
    fireEvent.keyDown(group, { key: "ArrowLeft" });
    expect(radios[2]?.getAttribute("aria-checked")).toBe("true");
    expect(radios.map((r) => r.tabIndex)).toEqual([-1, -1, 0]);
  });
});
