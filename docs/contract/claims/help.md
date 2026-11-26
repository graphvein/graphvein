# Claim contract: the Help page's examples

Task T045 (no ADR: the page adds no endpoint, dependency or stored state).
The Help page's text comes from the public guide in `docs/guide/`; it
states nothing about the repository being viewed. That includes the fixed
example under each label of "Start here" (`GET /api/users` · Fact · 1.0
and the others): it is written in the guide, marked "Example", the same
with or without an index, and names nothing of the repository, so it has
no row. Only the three live examples of "Start here" state something
about the repository, each shown as an "In your index:" line after the
fixed example: each one names a route, a call or a count of
the index being viewed, with a link to it. Rows `HP-1`..`HP-19` are those
claims; `HP-6`..`HP-19` are unused and stay reserved. `rows.test.ts`
(`web/src/sections/help/`) keeps this table 1:1 with the code's row list
(`HELP_ROWS`) and checks that every row names a test.

Every example is read from the API section's own answers
(`GET /api/v0/api/endpoints` and `GET /api/v0/api/endpoint`, documented
in `docs/cli/serve.md`), in production scope and the whole repository (no
source root). The page computes no fact of its own.

## Examples (HP)

| Row | Claim | Source and rule | When it is not shown |
|---|---|---|---|
| HP-1 | Fact: "<route> Fact · 1.0 from its registration" (a server action: "from its 'use server' directive") | the first endpoint of the list, in the server's order, that is an HTTP route with a method and a full path; else the first endpoint of any transport (a Tauri command by its command, a server action by its action name, else the stored name). The link opens its detail on the Overview tab, whose header shows the same Fact chip and source | no endpoint in the list |
| HP-2 | Inferred: "<route> Inferred · <n> called from <file>:<line>" | among the first three HTTP routes of the list with a caller count above 0, in order, the first caller of the route's detail (in the detail's order) with an edge of provenance `inferred`; `<n>` is that edge's stored confidence in the UI's one number format (1 as 1.0, other values as stored, never rounded). The link opens the route's Callers tab with that caller's code peek | none of those routes has such a caller; a route whose detail request fails is skipped |
| HP-3 | Unknown: "<n> calls whose URL is built at runtime, listed, never linked" | the HTTP transport's count of calls with the raw status `dynamic` (`calls.by_status`, which is the project-scope count under `serve --project`), when it is a positive integer. The link opens the Dynamic gap sheet | the count is absent, 0 or not an integer |
| HP-4 | no example | without an index (no last run in the status), or when the endpoint list request fails, every live example is absent: no "In your index:" line and no link, only the fixed example. While the status or the requests are pending, a placeholder of the line's height holds its place; with an index the place is kept, so the box keeps its height: it reads "In your index: none of this kind yet" only when the answer proves it (the list answered with no case for HP-1 or HP-3; for HP-2, every HTTP route with a caller, at most three, was read with no failed detail request), and stays empty space otherwise (a failed request, or HP-2 not proven: more than three called routes or a failed detail request). No live example is ever taken from another index or written by hand | - |
| HP-5 | one index run | the examples are requested again when the status names a newer last run; an answer for an earlier run is never shown | - |
| HP-D | default | any other form (another transport's counts, a caller without an `inferred` edge, an edge of another provenance) is not an example | - |
