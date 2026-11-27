# Start here

Graphvein reads your code and shows what it finds: your API routes and the
calls that reach them, your database tables, how the parts of the system
connect, and what a change reaches. It does this without an LLM, and it
sends nothing about your code anywhere.

## How to read a label

Everything Graphvein shows carries one of three labels. The examples
below are illustrations, not taken from your code.

**Fact.** Read straight from the code, for example a route registration. A
fact always has the number 1.0.

<!-- graphvein:example fact -->
**Example:** `GET /api/users` · Fact · 1.0 · from its registration in the server code

**Inferred · number.** Graphvein matched two things by a rule, for example a
frontend call to a route by its URL. The number is a fixed rank per match
rule, not a probability: an exact path match is 0.9, or 0.7 when the call's
base URL is assumed rather than proven, and a path suffix match is 0.6.

<!-- graphvein:example inferred -->
**Example:** `fetch("/api/users")` · Inferred · 0.9 · a frontend call matched to `GET /api/users` by its exact path

**Unknown.** The code does not say, for example a URL built at runtime.
Graphvein lists these instead of guessing, and never counts an unknown as
zero or as "no change".

<!-- graphvein:example unknown -->
**Example:** `fetch(baseUrl + path)` · Unknown · a URL built at runtime: listed, never linked

Labels come in two styles. Plain, the default, uses words and the number
("Exact path match · 0.9"). Technical shows the stored values and rule ids
("inferred 0.9 exact"). Choose one under [Settings](#/settings?at=labels).

## A first look

The [Overview](#/overview) is the home page: the Graphvein logo at the top
of the sidebar opens it. It shows the parts of the repository and how they
connect, its routes and calls, its database tables and what Graphvein could
not read, each with a link into the section that has the details.

1. Open [API](#/api) and pick a route. The Callers tab lists every call
   matched to it, each with its label.
2. Open [Changes](#/changes) to see what changed since an earlier index
   and what each change reaches.
3. Open [What Graphvein couldn't read](#/unread) to see what is missing
   from the picture.
