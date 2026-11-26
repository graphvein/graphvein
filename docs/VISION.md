# Graphvein — Vision and Scope

## The problem

Before coding agents, building a non-trivial system forced people to design it first: how services talk, request/response shapes, the database schema, the trade-offs. That design lived in people's heads and in documents, because the work was impossible without it.

Agentic coding removed that forcing function. A developer states an outcome, a model writes a spec, an agent implements it and makes hundreds of decisions on the fly. The system still has an architecture, but nobody holds it. The spec is consumed and discarded; the decisions are never recorded.

The consequences:

- Engineers cannot sketch the systems they "built". Every question — where does X live, what does changing it break, why is it like this — goes back to an LLM, costing tokens and time, and the answer is probabilistic.
- Experience loses leverage. An expert without a model of the system cannot review an agent's change, only re-ask another agent.
- Legacy and inherited codebases have the same problem for older reasons: the documentation never existed or went stale.

## What Graphvein is

Point Graphvein at any codebase — agent-built, legacy, or unfamiliar — and it builds and maintains the design record that should have existed: architecture, API contracts, data model, functional flows, decisions, and known weaknesses. Every view starts at a high-level overview and drills down, level by level, until it reaches the exact function or line, which opens in the user's IDE.

It is built for three audiences:

1. **Individual engineers** who need to understand and change code without re-asking an LLM every time.
2. **Teams** who need onboarding, shared understanding, and a place where human decisions about the code persist.
3. **Reviewers and auditors** who need a structured list of weaknesses and a record of which were accepted, and why.

## Product goal: less LLM spend, better answers (developer decision 2026-11-09)

Graphvein exists to reduce what people and coding agents spend on LLM calls about a codebase while improving the quality of the answers. Deterministic facts are computed once and cached by content hash, so agents and people query them instead of re-scanning the codebase. Gap filling runs only on Graphvein's explicit list of unknowns, with small, bounded context packs (the span, its resolved neighbours, the assumption the answer would rest on); its answers are cached by content hash, and deterministic configuration rules are tried before any model call.

Honest scope: the savings are largest for cross-boundary and impact questions (which client calls reach this route, what uses this table, what does this change break), where an agent would otherwise read many files across boundaries.

## Core principles

1. **Deterministic first.** Everything that can be derived from code without an LLM is derived that way: structure, calls, routes, contracts, schema. These are facts. LLMs are used only for what code cannot tell you, and only on demand.
2. **Every claim carries provenance.** Each statement in Graphvein belongs to exactly one layer, and the UI shows which:
   - **Fact** — derived deterministically from code; verifiable.
   - **Evidence-backed rationale** — the actual reason for a choice, sourced from commits, PRs, ADRs, agent transcripts, or human input, with a link to the source.
   - **Assessment** — an LLM's independent judgement of a choice: pros, cons, rubric scores, and better alternatives. Explicitly an opinion, never presented as the author's intent.
   - **Agent-proposed rationale** — a decision recorded by a coding agent, pending human confirmation before it becomes evidence.
   - **LLM-inferred structure** — a route, link or other structural element that deterministic analysis reported as a gap and an LLM proposed, validated against the source and shown with a distinct style. It is never merged into facts and never turns an unknown into a fact silently; a user can confirm it or turn it into a configuration rule that makes it deterministic. When a deterministic extractor later covers the same gap, the extractor's output replaces the inference (an inference is never promoted to a fact), and the agreement between the two is recorded as an accuracy measure of gap filling. Next to it, gap filling records its cost per resolved unknown.
3. **Humans decide; the tool records.** Graphvein surfaces weaknesses and lets humans accept or reject them with a reason. Accepted reasons become evidence that informs every future reader and every future assessment. Only items a human marks "fix" are handed to coding agents.
4. **Compute once, read forever.** Results persist. Re-indexing is incremental and keyed on content hashes; LLM output is cached against the content it describes and regenerated only when that content changes.
5. **Offline by design.** With LLM features off, Graphvein makes no outbound network calls except to infrastructure the user explicitly configured (for example, a team Postgres database). This guarantee covers Graphvein's own connections; a user-supplied external indexer is outside its network controls (ADR 0010). This is a verifiable guarantee, not a setting, because it is what makes the tool usable on client and enterprise codebases.
6. **Honest about precision.** Language support is labelled per language as precise or best-effort. Inferred links (for example, a frontend call matched to a route by URL pattern) carry confidence and are shown as inferred.
7. **Built the way it argues code should be built.** Graphvein is developed with coding agents, dogfooded on itself, verified against golden fixtures, and its design decisions are recorded as ADRs.

## Feature scope (all in v1)

v1 is released only when every feature below is complete. The grouping reflects build order (see `ROADMAP.md`), not priority for inclusion.

Before v1, Graphvein goes public in two preview launches (developer decision 2026-11-18, `ROADMAP.md`): first the deterministic core with published evidence, then grounded wiki pages. They are previews on the way to v1, not a change to its scope or success criteria.

**Foundation**
- Unified graph data model with abstraction levels; every view is a projection of it.
- Deterministic multi-language indexer with per-language precision markers and per-file failure isolation.
- Default ignores for agent working folders, build output and vendored code.
- Incremental re-indexing on content hashes, with a visible freshness indicator.
- Configurable IDE deep links (VS Code by default).
- Storage on SQLite (default) or Postgres (team mode).
- Projects that span several repositories (for example a separate frontend and backend), linked across repositories with each repository's identities kept stable.

**Core views**
- Cross-boundary linking: frontend/mobile/desktop calls ↔ server routes; code ↔ database tables.
- Architecture graph: from a high-level design overview (apps, services, data stores, external systems, with protocols, auth and encryption on the edges) down to the exact line.
- API section: OpenAPI extraction or import, a Swagger-like interface, export, and live "try it" against a user-supplied server with guardrails.
- Database schema view from SQL migrations and from ORM models in every first-tier language (for example SQLModel/SQLAlchemy, Prisma, Diesel, GORM), with optional read-only live introspection and drift detection. Schema facts state the environment assumptions they rest on (for example, that the committed migration history applies cleanly), and the view shows them.
- Change impact: what a change does to the codebase's verified facts and what depends on them, compared with the previous index, a pinned baseline, or a branch's base; shown in the UI and through the MCP tools. An add-on to understanding the code, not a PR-review product: no PR bot, bundled linters or autofix.
- Standard dependency and call graphs.

**Understanding layer**
- Read-only MCP server over the deterministic facts (routes, callers, cross-boundary links, schema including its assumptions, unknowns, change impact), with freshness reporting; built first in this layer, so agents query facts instead of re-reading code. Inferred answers from gap filling go out through the same tools, labelled inferred.
- Model backends: provider mode (bring-your-own-key, OpenRouter, local models) and agent-driven mode, where the user's own Codex, Claude Code or OpenCode session does the generation through Graphvein skills (ADR 0003).
- Lazy, cached LLM generation.
- Functional graph: features traced from entry points (pages, screens, commands, jobs) through APIs and handlers to data, named and grouped by an LLM.
- Generated documentation pages whose claims link to graph nodes.
- Optional gap filling: an LLM examines only the gaps deterministic analysis reports (unresolved routes, mounts, prefixes, client calls and API bases), each with a small, bounded context pack (the span, its resolved neighbours, the assumption it would rest on), answers from constrained choices that are validated against the source and cached by content hash, applies deterministic configuration rules before any model call and prefers proposing new ones, and shows the expected cost before running.
- Evidence mining from git history, commit messages, PR descriptions, ADRs and agent transcripts.

**Judgement layer**
- LLM assessment of design choices: pros, cons, better alternatives, rubric-based scores.
- Integration of deterministic security analyzers, with LLM explanation and prioritisation.
- Review queue with thresholds, quality and security kept separate, failed criteria shown.
- Dispositions: human accept/fix decisions with rationale, shared across a team, expiring when the code changes materially.

**Agent loop**
- MCP tools for rationale, dispositions and findings, added to the read-only MCP server of the understanding layer, with freshness reporting.
- Agent write-back of decisions as agent-proposed rationale.
- Spec-vs-implementation drift detection.

**Showcase**
- 3D layered view: abstraction levels as stacked planes receding in depth, related entities (tables, external services, other components) orbiting the focused element, drilling "into" the scene down to code.

## Non-goals for v1

- Not an editor or IDE. It links into the user's IDE.
- Not an autonomous fixer. It hands explicitly approved items to agents; it does not change code itself.
- Not a hosted SaaS. It runs locally or against the team's own database.
- Not a general Q&A chatbot over code. Coding agents already do that; Graphvein gives them better context instead.
- No v1 release before every feature above is complete. The preview launches before it (`ROADMAP.md`) are previews of the built parts, open to contributions under the contributor licence agreement (`CONTRIBUTING.md`); they do not change v1's scope.

## Target languages and frameworks

Precise support is prioritised for the stacks the test corpus covers, then widened.

- **First:** TypeScript and JavaScript (Node.js/Express, React, Angular including templates), Rust (Axum, Tauri commands), Go, Python (FastAPI).
- **v1, best-effort tier:** Java, C# and Dart get bundled grammars and best-effort structure (files, classes, functions, imports), labelled `best_effort`; no framework extraction (developer decision 2026-11-09).
- **After v1:** precise Java (Spring), C# (ASP.NET Core) and Dart (Flutter) support.
- Every file and node carries its language's precision label (`precise` or `best_effort`), and the UI shows it.

## After v1 (developer decision 2026-11-09)

These were in scope earlier and move after the v1 release:

- The exhaustive error-site audit of SQL migration verdicts (T018a2 Revision 2.0). In v1, migration-derived schema rests on stated assumptions instead of predicting every way Postgres can refuse a statement.
- Alembic migrations (T018d).
- Precise Java/Spring, C#/ASP.NET Core and Dart/Flutter support (the best-effort tier ships in v1).
- Long-tail precision backlogs (for example the Angular router gaps and rare framework forms recorded as known gaps).
- Verification beyond the v1 bar (claim contract, golden fixtures, corpus checks, one independent review, an oracle where cheap): adversarial generators beyond those already built, 100,000-seed runs, repeated fresh re-check rounds and exhaustive failure prediction (for example ORM import failures). Graphvein gives an overview of the project as it is; it does not replace a compiler or a database.

Until they land, what extractors do not establish stays unknown, or, once the understanding layer exists, is filled by LLM gap filling under the provenance rules above.

## Success criteria for v1

- On every repository in the test corpus, the deterministic graph matches the golden expectations.
- A developer can answer "where does X live, what calls it, what breaks if I change it" without an LLM.
- A developer who has never seen a codebase can sketch its architecture, API surface and schema after 30 minutes in Graphvein.
- Running with LLM features off produces zero unexpected network traffic.
- Graphvein's own codebase is fully mapped by Graphvein.

## Open questions

- Final list of frameworks with precise cross-boundary linking in v1.
- Licence (MIT vs Apache-2.0).
- Whether the 3D view ships as part of the main UI or as an optional mode.
