# Claim contract tables and census data

Extractors and linkers over a large language or framework surface keep a
claim contract: a table in which every form the code models has exactly one
row, every claim the code makes is named by a row, and every other form falls
to a single unknown default branch (`D`). The contract tests keep the code
tables and these tables 1:1.

The files here are the contract tables as they were fixed when each feature
was designed. The feature's public documentation (`docs/extract/`,
`docs/lang/`) holds the same rows, and the tests check that both agree with
the code. Table rows and headings are copied verbatim; surrounding prose was
only edited to replace references to internal planning records.

| File | Area | ADR | Read by |
|---|---|---|---|
| [architecture-graph.md](architecture-graph.md) | components, component links, annotations, deploy config, auth extractors, chain | 0079 | `internal/server`, `internal/extract/packages`, `internal/extract/deploy`, `internal/extract/clients`, `internal/extract/routes` |
| [change-impact.md](change-impact.md) | change rows and impact relations | 0056 | `internal/changes` |
| [couldnt-read.md](couldnt-read.md) | the "What Graphvein couldn't read" page (`GET /api/v0/unread`) | 0089 | `internal/server` |
| [diesel-models.md](diesel-models.md) | Diesel models | 0062 | `internal/extract/models/diesel` |
| [drizzle-models.md](drizzle-models.md) | Drizzle models | 0059 | `internal/extract/models/drizzle` |
| [express5-paths.md](express5-paths.md) | Express route paths under Express 4 and 5 | - | `internal/extract/routes` |
| [go-http-routers.md](go-http-routers.md) | Go HTTP routers | 0076 | `internal/extract/routes` |
| [gorm-models.md](gorm-models.md) | GORM models | 0063 | `internal/extract/models/gorm` |
| [help.md](help.md) | the Help page's live examples | - | `web/src/sections/help` |
| [jump-search.md](jump-search.md) | jump-to search (`GET /api/v0/search`) | 0089 | `internal/server` |
| [mcp-server.md](mcp-server.md) | the read-only MCP server's tools and response envelope | 0082 | `internal/mcp` |
| [nextjs-supabase.md](nextjs-supabase.md) | Next.js routes, server actions, Supabase | 0077 | `internal/index`, `internal/extract/schema`, `internal/extract/models/supabasetypes`, `internal/extract/tableaccess` |
| [openapi-generation.md](openapi-generation.md) | OpenAPI document generation, explorer path values | 0081 | `internal/openapi` |
| [precision-labels.md](precision-labels.md) | precision label rules | 0066 | `internal/precision` |
| [prisma-models.md](prisma-models.md) | Prisma models | 0057 | `internal/extract/models/prisma` |
| [python-models-syntax.md](python-models-syntax.md) | Python model syntax payload | 0053 | `internal/lang/python`, `internal/extract/models/sqlmodel` |
| [python-models-linking.md](python-models-linking.md) | Python model linking | 0054 | `internal/extract/models/sqlmodel` |
| [route-model-types.md](route-model-types.md) | routes that use a database model (MT-20..MT-29: the Schema view's routes, the route detail's tables) | 0081 | `internal/server` |
| [settings.md](settings.md) | the Settings page's keys, value origins and guarded `graphvein.yaml` edit | 0088 | `internal/config`, `internal/server` |
| [sequelize-models.md](sequelize-models.md) | Sequelize models | 0061 | `internal/extract/models/sequelize` |
| [shape-differences.md](shape-differences.md) | request differences of a link (what a call sends, what the route reads) | 0081 | `internal/shapediff`, `internal/extract/routes`, `internal/server` |
| [table-access.md](table-access.md) | code-to-table access | 0060 | `internal/extract/tableaccess` |
| [typeorm-models.md](typeorm-models.md) | TypeORM models | 0064 | `internal/extract/models/typeorm` |
| [unknown-prefix-paths.md](unknown-prefix-paths.md) | routes whose prefix is unknown, their client links | 0084 | `internal/extract/routes`, `internal/extract/clients`, `internal/server` |
| [workspace.md](workspace.md) | the workspace registry, multi-index dispatch, warm indexes, typed folder paths | 0087 | `internal/workspace`, `internal/server` |

## Census data

`data/` holds the corpus censuses and inventories that the corpus tests
compare against (one JSON object per line). They were recorded on public
open-source corpora pinned by commit; the corpus tests skip when the corpus
checkout is absent.

| File | Read by |
|---|---|
| `data/drizzle-models-inventory.jsonl` | `internal/lang/typescript` (Drizzle census) |
| `data/go-http-routers-census.jsonl` | `internal/lang/golang`, `internal/extract/routes` |
| `data/go-http-routers-census-gr5.jsonl`, `-gr6`, `-gr7` | `internal/extract/routes` (later corrections, applied in order) |
| `data/gorm-models-inventory.jsonl` | `internal/lang/golang`, `internal/extract/models/gorm` |
| `data/openapi-census.jsonl` | `internal/index` (OpenAPI corpus) |
| `data/orval-census.jsonl`, `data/orval-composites-r1.jsonl` | `internal/index` (Orval corpus) |
| `data/python-models-inventory.jsonl` | `internal/lang/python` |
| `data/sequelize-models-inventory.jsonl` | `internal/lang/typescript`, `internal/extract/models/sequelize` |
| `data/typeorm-models-inventory.jsonl` | `internal/lang/typescript`, `internal/extract/models/typeorm` |

A change to a contract is a contract change: the table here, the public
documentation, the code table and the tests change together, and the change
is explained in review.
