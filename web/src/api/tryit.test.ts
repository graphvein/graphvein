// The try-it parsers (T029 part B) on answers shaped as docs/cli/serve.md
// "Try it endpoints" documents them.
import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/client";
import { ENVIRONMENTS } from "@/test/tryitFixtures";
import { authMissingDetails, certificateDetails, limitDetails, parseEnvironments, parsePrepared, parseSent, parseSuggestions } from "@/api/tryit";

describe("try-it parsers (T029 part B)", () => {
  it("reads the environments listing without any auth value", () => {
    const e = parseEnvironments(structuredClone(ENVIRONMENTS));
    expect(e.selected).toBe("repository:local");
    const env = e.environments[0];
    expect(env?.auth).toEqual({ header: "Authorization", env: "API_TOKEN", envSet: true, prefix: "Bearer ", sessionValue: false });
    expect(env?.options.tls?.pins).toEqual([{ origin: null, sha256: "9595c84a" }]);
    expect(env?.options.limits?.response_bytes).toBe(16777216);
    expect(e.ceilings.timeout_seconds).toBe(600);
    // An environment without auth lists auth fields as null.
    const none = structuredClone(ENVIRONMENTS);
    (none.environments[0] as Record<string, unknown>).auth = { header: null, env: null, env_set: false, prefix: null, session_value: false };
    expect(parseEnvironments(none).environments[0]?.auth.env).toBeNull();
  });

  it("reads suggestions with their source kind, row, file and line", () => {
    const s = parseSuggestions({
      api_version: 0,
      repository: "api",
      notice: "suggested, not used until you pick one",
      suggestions: [
        { url: "http://127.0.0.1:8002", source_kind: "compose_published_port", row: "TI-31", file: "docker-compose.yml", line: 2, reason: "Compose service api publishes 8000 on 8002" },
        { url: "http://127.0.0.1:8000", source_kind: "framework_default", row: "TI-41", file: null, line: null, reason: "framework default" },
      ],
    });
    expect(s.suggestions.map((x) => [x.sourceKind, x.file, x.line])).toEqual([
      ["compose_published_port", "docker-compose.yml", 2],
      ["framework_default", null, null],
    ]);
  });

  it("reads prepare's exact request, reasons and missing auth", () => {
    const p = parsePrepared({
      api_version: 0,
      environment: "repository:local",
      label: "local",
      production: false,
      method: "POST",
      url: "http://127.0.0.1:8000/items/",
      headers: ["X-Trace", "Content-Type"],
      free_form: false,
      auth_header: null,
      auth_source: null,
      auth_missing: { header: "X-Api-Key", env: "API_KEY" },
      send_without_auth: false,
      body_bytes: 13,
      body_sha256: "ab",
      multipart: [{ name: "avatar", filename: "a.png", content_type: "image/png", bytes: 3 }],
      limits: { request_bytes: 1048576, response_bytes: 4194304, timeout_seconds: 30 },
      proxy: "http://proxy.example:3128",
      proxy_cleartext: true,
      confirmation_required: true,
      confirmation_reasons: ["method"],
      confirmation: null,
      expires_at: null,
      acknowledgement_required: false,
      selected: true,
    });
    expect(p.authMissing).toEqual({ header: "X-Api-Key", env: "API_KEY" });
    expect(p.confirmation).toBeNull();
    expect(p.confirmationReasons).toEqual(["method"]);
    expect(p.proxyCleartext).toBe(true);
    expect(p.multipart?.[0]?.filename).toBe("a.png");
  });

  it("reads a send answer with truncation and a redirect to follow", () => {
    const s = parseSent({
      api_version: 0,
      environment: "repository:local",
      method: "POST",
      url: "http://127.0.0.1:8000/items/",
      status: 303,
      status_text: "See Other",
      headers: [{ name: "Set-Cookie", value: "[redacted]" }],
      body: "",
      body_encoding: "text",
      body_bytes: 0,
      truncated: true,
      truncated_reason: "size",
      truncated_limit: { limit: "response_bytes", value: 4194304, ceiling: 268435456, hint: "raise limits.response_bytes" },
      redirect: "https://login.example.com/start?next=a",
      redirect_follow: {
        url: "https://login.example.com/start?next=a",
        origin: "https://login.example.com",
        same_origin: false,
        method: "GET",
        keeps_body: false,
        environment: null,
        path: "/start",
        query: [{ name: "next", value: "a" }],
        suggested_environment: { name: "login.example.com-443", base_url: "https://login.example.com" },
      },
      duration_ms: 12,
      remembered: false,
    });
    expect(s.truncatedLimit?.ceiling).toBe(268435456);
    expect(s.redirectFollow?.suggestedEnvironment?.base_url).toBe("https://login.example.com");
    expect(s.redirectFollow?.query).toEqual([{ name: "next", value: "a" }]);
  });

  it("reads the details of the refusals the UI acts on", () => {
    const cert = new ApiError("http", "x", 502, "upstream_certificate_changed", {
      environment: "repository:dev",
      origin: "https://127.0.0.1:8443",
      certificate: { subject: "CN=dev box", issuer: "CN=dev box", sans: ["localhost"], not_before: "2026-11-01T00:00:00Z", not_after: "2027-11-01T00:00:00Z", sha256: "9595", self_signed: true },
      reason: "x509: certificate signed by unknown authority",
      pinned_sha256: ["1111"],
    });
    expect(certificateDetails(cert)).toMatchObject({ changed: true, pinnedSha256: ["1111"], certificate: { sha256: "9595", sans: ["localhost"] } });
    expect(certificateDetails(new ApiError("http", "x", 502, "upstream_error"))).toBeNull();
    expect(limitDetails(new ApiError("http", "x", 413, "body_too_large", { limit: "request_bytes", value: 1048576, ceiling: 67108864, hint: "h" }))?.limit).toBe("request_bytes");
    expect(authMissingDetails(new ApiError("http", "x", 400, "auth_missing", { header: "X-Api-Key", env: "API_KEY" }))).toEqual({ header: "X-Api-Key", env: "API_KEY" });
    // Details that do not have the documented shape are no details.
    expect(certificateDetails(new ApiError("http", "x", 502, "upstream_certificate_untrusted", "oops"))).toBeNull();
  });
});
