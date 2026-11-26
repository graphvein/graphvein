// Fixtures of the try-it endpoints (T029 part B): an environments listing
// as docs/cli/serve.md documents it, and builders for parsed environments
// and prepare answers.
import type { Environment, Prepared } from "@/api/tryit";

export const ENVIRONMENTS = {
  api_version: 0,
  repository: "api",
  token: "tok",
  selected: "repository:local",
  environments: [
    {
      id: "repository:local",
      name: "local",
      label: "local",
      production: false,
      source: "repository",
      file: "graphvein.yaml",
      line: 4,
      base_url: "http://127.0.0.1:8000/api",
      origin: "http://127.0.0.1:8000",
      auth: { header: "Authorization", env: "API_TOKEN", env_set: true, prefix: "Bearer ", session_value: false },
      selected: true,
      acknowledged: false,
      remembered: false,
      options: { tls: { pins: [{ origin: null, sha256: "9595c84a" }], ca_file: null }, use_system_proxy: false, limits: { request_bytes: null, response_bytes: 16777216, timeout_seconds: null } },
      effective_limits: { request_bytes: 1048576, response_bytes: 16777216, timeout_seconds: 30 },
    },
  ],
  files: [{ target: "repository", file: "graphvein.yaml", exists: true, hash: "sha256:abc", writable: true }],
  config_error: null,
  limits: { request_body_bytes: 1048576, response_body_bytes: 4194304, timeout_seconds: 30, confirmation_ttl_seconds: 120 },
  limit_ceilings: { request_bytes: 67108864, response_bytes: 268435456, timeout_seconds: 600 },
};

export function env(over: Partial<Environment> = {}): Environment {
  return {
    id: "repository:local",
    name: "local",
    label: "local",
    production: false,
    source: "repository",
    file: "graphvein.yaml",
    line: 4,
    baseUrl: "http://127.0.0.1:8000",
    origin: "http://127.0.0.1:8000",
    auth: { header: "Authorization", env: "API_TOKEN", envSet: true, prefix: "Bearer ", sessionValue: false },
    selected: true,
    acknowledged: false,
    remembered: false,
    options: { tls: null, use_system_proxy: false, limits: null },
    effectiveLimits: { request_bytes: 1048576, response_bytes: 4194304, timeout_seconds: 30 },
    ...over,
  };
}

export function prepared(over: Partial<Prepared> = {}): Prepared {
  return {
    environment: "repository:local",
    label: "local",
    production: false,
    method: "POST",
    url: "http://127.0.0.1:8000/api/items/",
    headers: ["X-Trace"],
    freeForm: false,
    authHeader: "Authorization",
    authSource: "env",
    authMissing: null,
    sendWithoutAuth: false,
    bodyBytes: 13,
    bodySha256: "abcdef0123456789",
    multipart: null,
    limits: { request_bytes: 1048576, response_bytes: 4194304, timeout_seconds: 30 },
    proxy: null,
    proxyCleartext: false,
    confirmationRequired: true,
    confirmationReasons: ["method"],
    confirmation: "c1",
    expiresAt: null,
    acknowledgementRequired: false,
    selected: true,
    ...over,
  };
}

