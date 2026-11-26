# Roadmap

Graphvein's v1 scope is described in [the vision](docs/VISION.md). On the way
to v1 it is released in two public previews, so that real use shapes the
rest of the work. There are no dates: each launch ships when its items are
done and checked.

## Launch 1: the deterministic core, with evidence

1. The core as built: routes and their callers (including Tauri commands),
   schema and the code that reads and writes each table, change impact,
   precision labels, IDE links and the local web UI.
2. Coverage additions: Go HTTP routers (net/http `ServeMux` patterns, chi,
   gin); Next.js App Router route handlers and server actions; the Supabase
   JS client; multi-repository projects with frontend and backend in
   separate repositories.
3. The 2D architecture graph, with protocol and auth annotations where the
   code proves them.
4. OpenAPI generation, an API explorer and a guarded "try it" proxy.
5. Evidence: a read-only MCP server and a benchmark with real numbers; link
   precision and false-link rates of route-to-caller links measured on
   messy real-world repositories; public golden fixtures.
6. Polish: plain default wording in the UI; the precision claim worded
   exactly as far as it holds; a README status table (precise, best effort
   or not supported, per language and framework); a quick start; signed
   release binaries with a provenance note; a short, checked comparison
   with the closest open-source alternatives.

## Launch 2: the grounded wiki

1. A provider mode and an agent-driven mode, so that your own coding agent
   can do the writing without an API key; a task framework with
   content-hash caching.
2. Overview-level wiki pages (repository overview, one page per service or
   module, per-route pages) written by an LLM from bounded fact packs only.
   Every sentence cites the facts it rests on, structural claims are
   checked against the graph, and pages that fail are rejected.
   LLM-written text looks different from facts, and the feature is off by
   default.
3. Partial gap filling: only unknowns whose answers can be checked against
   the source (for example an unresolved router prefix or client base URL).
   Answers are labelled inferred and cached, and the cost is shown before
   anything runs.

## After the two launches (still v1)

- Live database introspection and a drift report.
- The rest of the understanding layer: functional graph, evidence mining
  and the remaining gap filling.
- The judgement layer: rubric-based quality and security assessments,
  findings linked to code, a review queue, and dispositions with rationale
  that stay attached to the code they describe.
- The agent loop: the remaining MCP tools (rationale, dispositions,
  findings), agent-proposed decisions confirmed in the UI, and
  spec-versus-implementation drift.
- The showcase 3D view.
- Release preparation: signed binaries, an npm wrapper package and a
  documentation site.

## After v1

See [the vision](docs/VISION.md), "After v1".
