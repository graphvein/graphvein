# deploy-compose fixture (T026, ADR 0079)

Every row of the deploy-config claim contract
(`docs/contract/claims/architecture-graph.md`, rows DC) and its modelled forms;
golden `testdata/fixtures/deploy-compose.golden.json` (deploy nodes and
refusal diagnostics), `docs/extract/deploy.md` describes the facts.

Compose files and Dockerfiles are stored under inert names so that the
repository's own index gains no deploy facts from this fixture; the tests
copy the directory to a temporary repository and restore the real names:

| stored as | restored as | rows |
|---|---|---|
| `_compose.yml` | `compose.yml` | DC-S1 (image with tag and digest, build with context and Dockerfile, long `depends_on`, short and long ports, an interpolated image), DC-S2 (URL with a password, interpolated hosts, a password with a raw `/`, a `|` after the port, plain secrets: none stored), DC-T1, DC-T2 and an other argument (DCD) |
| `_compose.deploy.yml` | `compose.deploy.yml` | per-file reading: `build` string form, mapping-form labels with `tls: true` (DC-T1), an interpolated `tls`, a middleware label (DCD), an interpolated router name |
| `_compose.override.yml` | `compose.override.yml` | the directory's default override: a service with neither build nor image (server row CD4, Compose's default merge; the oracle checks the discovery and merge) |
| `ops/_docker-compose.yaml` | `ops/docker-compose.yaml` | anchors and a merge key, `include` and `extends` (DCD), contexts that are relative, outside the repository, remote and interpolated |
| `_docker-compose.broken.yml` | `docker-compose.broken.yml` | unknown anchor: refused |
| `_compose.dup.yml` | `compose.dup.yml` | duplicate key: refused |
| `_compose.empty.yml` | `compose.empty.yml` | no `services`: DCD |
| `api/_Dockerfile` | `api/Dockerfile` | DC-F1: stages with aliases, a stage reference, an `ARG` image, a heredoc, a continued `EXPOSE` |
| `api/_Dockerfile.dockerignore` | `api/Dockerfile.dockerignore` | not a Dockerfile (DCD) |
| `web/_web.Dockerfile_` | `web/web.Dockerfile` | `*.Dockerfile`, `# escape=` directive, `--platform` |
| `web/_Dockerfile.md` | `web/Dockerfile.md` | not a Dockerfile: refused |
| `tools/_Dockerfile` | `tools/Dockerfile` | `RUN` before `FROM`: refused |
| `envset/_env` | `envset/.env` | AG-RF: a `COMPOSE_FILE` line (DC-S1 `dotenv_compose_file`; the value and the other lines are never stored) |
| `envset/_compose.yml`, `envset/_compose.override.yml` | `envset/compose.yml`, `envset/compose.override.yml` | a default pair whose directory's `.env` sets `COMPOSE_FILE` |

AG-RF also adds `--providers.docker.exposedbydefault=false` to
the `compose.yml` proxy (DC-T2) and a `hidden` service with
`traefik.enable: false` and a TLS router to `compose.deploy.yml` (DC-T1).
