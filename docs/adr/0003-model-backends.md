# ADR 0003 — Model backends: provider mode and agent-driven mode

**Status:** Proposed
**Date:** 2026-10-05

## Context

Graphvein's understanding and judgement layers (Tiers 3 and 4) need an LLM. Users fall into two groups:

- people with API keys (OpenRouter, Anthropic, OpenAI-compatible endpoints, local models);
- people with subscriptions to coding agents (Codex, Claude Code, OpenCode with a provider) who want to use those instead of paying per token.

The original architecture included a "CLI driver" in which Graphvein would spawn agent CLIs headlessly (for example `claude -p`, `codex exec`) and use them as model backends. This means a third-party tool programmatically driving a user's subscription. Vendor terms for consumer subscriptions may restrict that, and they have changed over time. Graphvein should not depend on a usage pattern that may be disallowed or may break.

CodeWiki demonstrated a different pattern: the user runs their own coding agent, and a skill teaches the agent to call the tool's CLI commands to fetch evidence, write pages, and validate them. The tool never touches the subscription.

## Options considered

1. **API providers only.** Simple and unambiguous, but excludes subscription users.
2. **Headless CLI driver.** Uses subscriptions, but Graphvein drives the agent, with terms-of-service risk, credential handling, and fragile parsing of CLI output.
3. **Provider mode plus agent-driven mode.** API providers for direct calls; for subscriptions, the user's own agent session drives Graphvein through skills.

## Decision

Option 3. The headless CLI driver is removed from the architecture.

### Provider mode

Graphvein calls models directly through a provider interface:

- OpenAI-compatible HTTP (OpenRouter, Ollama, LM Studio, other compatible endpoints);
- Anthropic API;
- further providers only via the same interface.

Provider mode is required for unattended and batch runs (for example regenerating stale pages after re-indexing, or CI use). All calls go through `internal/egress` and are allowed only in `llm` egress mode.

### Agent-driven mode

The user runs Codex, Claude Code or OpenCode as they normally would. Graphvein ships skill/instruction packages for each agent that teach it a fixed workflow using Graphvein's CLI:

- `graphvein task list` — pending generation or assessment work (stale pages, unnamed features, unassessed nodes);
- `graphvein task evidence <task-id>` — the exact inputs for a task: relevant nodes, source excerpts with locations, existing rationale and dispositions, the rubric if any;
- `graphvein task submit <task-id>` — accepts the agent's output (from stdin or a file);
- `graphvein task validate <task-id>` — checks that every claim cites node identities, that cited nodes exist, and that rubric output is well-formed; rejects invalid submissions with a precise reason.

Rules:

- Graphvein never stores, reads or uses the agent's credentials.
- Output submitted in agent-driven mode is stored exactly like provider output: same task cache keys (task, prompt version, content hashes of inputs), same provenance (`assessment` or cited generated prose), with the agent and model recorded as metadata where the agent reports it.
- Validation is identical for both modes. An agent cannot submit uncited claims or malformed scores.
- Graphvein generates the skill packages on request (`graphvein agent setup codex|claude-code|opencode`) and prints where to put them. It never writes into a user's repository or agent configuration automatically.

### Relationship to MCP

The MCP server (Tier 5) is for agents doing *other* work that need context from Graphvein. Agent-driven mode is for agents doing *Graphvein's* generation work. Both can use the same underlying query code; they are separate entry points.

## Consequences

- Subscription users get full Tier 3–4 features without any terms-of-service grey area, since they drive their own agent.
- The task framework must expose tasks as explicit, inspectable units with evidence bundles. This also improves provider mode: every LLM call has a reproducible, reviewable input.
- Agent-driven mode is interactive: it runs when the user runs their agent. Unattended regeneration requires provider mode.
- Three skill packages must be maintained as the agents' skill formats evolve.
- `docs/ARCHITECTURE.md` (Analysis section) and the project plan (M3) are updated to remove the CLI driver and add agent-driven mode.

## Known gaps

- Each agent's skill/instruction format and capabilities differ and change over time; the packages need testing against current versions at M3.
- The developer should re-check current vendor terms before release, both for subscriptions and for the API providers.
- Agents may report their model inconsistently; metadata may be incomplete.
