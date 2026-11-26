# Deploy configuration

T026, ADR 0079 option 2A; extractors-48 (the stack's number, no
own bump: the pass reads retained sources at link time and adds no cache
payload); contract v1 unchanged (`deploy_service` and `dockerfile` are
extensible node kinds). The claim contract is the deploy-config table of
`docs/contract/claims/architecture-graph.md` (rows DC-S1, DC-S2, DC-T1, DC-T2,
DC-F1, DCD), implemented by `internal/extract/deploy` (`Contract`).
`TestDeployConfigContract` keeps code and table 1:1;
`TestDeployComposeFixture` reaches every row and modelled form on
`testdata/fixtures/deploy-compose` (golden `deploy-compose.golden.json`).

`DefaultPair` states Docker Compose's default file discovery (compose-go
`DefaultFileNames` and `DefaultOverrideFileNames`) for the server's row
CD4. Every file is read on its own: compose files are never merged
(`compose.yml` and `compose.override.yml` are two sets of facts),
`extends` and `include` are not followed, and nothing runs. Mapping
services to components, ingress TLS and database engines are server
computations (rows CD1, CD3, AT2, AE2 in `internal/server/architecture*.go`).

## Nodes

**`deploy_service`** (level 2), one per service of a compose file's
`services` mapping: identity `deploy_service:<compose file>#<service>`,
span from the service key to its last non-blank, non-comment line.
Attributes (values as written, `null` when absent):

| attribute | content |
|---|---|
| `service` | the service name |
| `image`, `image_name`, `image_tag`, `image_digest` | the image as written and split as `[registry/][path/]name[:tag][@digest]`; not split when interpolated |
| `build_context_as_written`, `build_dockerfile_as_written` | `build` (string form: the context) |
| `build_context` | the context resolved against the compose file's directory, repository-relative; absent when interpolated, absolute, remote (URL, `git@`) or outside the repository; `.` when `build` gives no context (the specification's default, per file) |
| `build_dockerfile` | `dockerfile` resolved against `build_context`; absent when not written (the context's default `Dockerfile` is not assumed), interpolated, absolute or unresolvable. `dockerfile_inline` is not read |
| `depends_on` | names, short (list) or long (mapping) form |
| `ports` | short entries `{as_written, interpolated}`, long entries `{target, published, protocol, interpolated}` |
| `environment_urls` | DC-S2 entries `{name, scheme, host, interpolated}` |
| `traefik_routers` | DC-T1 entries `{router, rule, entrypoints, tls, tls_enabled, tls_certresolver, interpolated}`; `tls_enabled` is the `tls` value parsed as a boolean (`strconv.ParseBool`), else null |
| `traefik_enable`, `traefik_enabled` | Review fix: the `traefik.enable` label as written and parsed as a boolean (`strconv.ParseBool`, as Traefik's label decoder), null when absent or not parsed |
| `traefik_entrypoints` | only on a service whose image is Traefik's official image: DC-T2 entries `{entrypoint, address, redirect_to, redirect_scheme, interpolated}` |
| `traefik_exposed_by_default` | only on a Traefik service: `--providers.docker.exposedbydefault` as `true` / `false` (a bare flag is true), `"unknown"` when not parsed, null when absent (review fix |
| `dotenv_compose_file` | Review fix: the path of the compose directory's `.env` when it has a `COMPOSE_FILE` line (`COMPOSE_FILE=`, `export COMPOSE_FILE=`, `COMPOSE_FILE:`), else null. Compose reads it and it replaces the default file discovery; no value of the `.env` is stored |
| `interpolated` | the attributes whose value holds a compose interpolation (`$NAME`, `${...}`; `$$` is literal) |

**`dockerfile`** (level 3), one per Dockerfile: identity
`dockerfile:<path>#<base name>`, span over the whole file. Attribute
`stages`, one entry per `FROM`: `index`, `line`, `alias` (`AS <name>`),
`platform` (`--platform=`), `image` and its split as above, `from_stage`
(the alias of an earlier stage the `FROM` names, case-insensitive; then the
image is not split), `expose` (the `EXPOSE` words of the stage, as
written) and `interpolated`.

## Rows

- **DC-S1** `compose.yml`, `compose.yaml`, `docker-compose*.yml`/`.yaml`,
  `compose.*.yml`/`.yaml` with a `services` mapping: one node per service.
  YAML anchors and merge keys (`<<`) are resolved. Refused, with one
  `deploy_compose_unread` info diagnostic and no node from the file: YAML
  syntax errors, an unknown anchor, several documents, duplicate keys, a
  `services` value or a service that is not a mapping, malformed `build`,
  `depends_on`, `ports`, `environment`, `labels` or Traefik `command`.
- **DC-S2** an `environment` value (list or mapping form) of the form
  `scheme://[userinfo@]host[:port][/...]` with a literal scheme and a port
  of digits or one interpolation: the variable name, scheme and host only.
  User info, password, port, path and query are never stored. An
  interpolated host is null and named in `interpolated`. A value with an
  `@` after its authority (a raw `/` in a password) or any other value is
  not stored.
- **DC-T1** service `labels` (list or mapping form)
  `traefik.http.routers.<r>.{rule,entrypoints,tls,tls.certresolver}`;
  the prefix and option are matched case-insensitively, the router name is
  kept as written, an interpolated name is flagged. Other labels (other
  router options, middlewares, services, swarm `deploy.labels`): DCD.
  Review fix: also `traefik.enable` (`traefik_enable`, `traefik_enabled`).
  The server states no TLS from a service Traefik's Docker provider
  ignores: `traefik.enable` false (for a CD4 service, the agreeing label of
  the same-name services it combines with), or a Traefik service of the
  same directory with `exposedbydefault=false` (or a value not read) and no
  `traefik.enable=true`; the routers are listed as Not proven instead.
  Traefik static configuration files are not read (DCD), so a provider
  setting written only there is not seen.
- **DC-T2** on a `traefik` image service, `command` arguments (list, or a
  string split like shell words) `--entrypoints.<e>.address=<v>`,
  `--entrypoints.<e>.http.redirections.entrypoint.to=<v>` and `.scheme=<v>`.
  Review fix: also `--providers.docker.exposedbydefault[=<bool>]`
  (`traefik_exposed_by_default`). Other arguments and these without `=`:
  DCD.
- **DC-F1** `Dockerfile`, `Dockerfile.*`, `*.Dockerfile` (not
  `*.dockerignore`, BuildKit's per-Dockerfile ignore file): line
  continuations, comments, parser directives (`# escape=`) and heredoc
  bodies of `RUN`, `COPY` and `ADD` are handled as BuildKit does. Refused,
  with one `deploy_dockerfile_unread` info diagnostic: no `FROM`, an
  instruction outside BuildKit's list (a `Dockerfile.md`), an instruction
  other than `ARG` before the first `FROM`, a `FROM` that is not
  `FROM [--flag ...] <image> [AS <name>]`, an unterminated heredoc.
- **DCD** (the unknown branch) everything else: nginx, CDK, Kubernetes,
  Helm, Traefik static configuration files, `.env` (beyond DC-S1's
  `COMPOSE_FILE` line) and `env_file`,
  `extends`, `include`, compose files under other names
  (`dev.docker-compose.yml`, `podman-compose.yml`), merges across files.

## Oracle

`scripts/oracle/deploy_config.sh` (never in CI) builds
`scripts/oracle/deploy_oracle` in a scratch module with
`github.com/compose-spec/compose-go/v2` v2.16.2 (Apache-2.0; loader per
file, interpolation, extends, include and normalization off, schema
validation on, paths resolved) and `github.com/moby/buildkit` v0.34.2
(Apache-2.0; `frontend/dockerfile/parser`). `TestDeployOracle` compares
services, images, resolved build paths, `depends_on`, port counts, DC-T1
labels and DC-S2 hosts (against Go's `net/url`), and per Dockerfile stage
images, aliases, platforms and `EXPOSE` words; any disagreement fails.
2026-11-18: the fixture and 21 corpora (92 files) agree after one fix (a
`tcp://host:2375|name` value is not a URL). Revision R1 adds, per directory
with a compose file, compose-go's default file discovery
(`cli.WithDefaultConfigPath`), compared with `deploy.DefaultPair`, and for
each default pair the merged model: a service of one file with neither
build nor image must carry the same-name service's build of the other
file (the basis of server row CD4's "Compose's default merge"). Facts the oracle does not
cover (DC-T2 entrypoint arguments, the `interpolated` lists) are mapped.
