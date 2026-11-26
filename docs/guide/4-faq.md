# Questions

## Is the number beside "Inferred" a probability?

No. It is a fixed rank per match rule: every link made by the same rule has
the same number. A fact is 1.0, an exact path match 0.9 (0.7 when the call's
base URL is assumed rather than proven) and a path suffix match 0.6. On a
path of several steps, the number shown is that of its weakest step.

## Does Graphvein need an LLM, or send my code anywhere?

No. Everything here is read from the code without an LLM. Graphvein opens no
network connection except to what you configured yourself: a team Postgres
database, or the Try it out environment you selected. It collects no usage
data.

## Why does it say "what a change reaches" and not "what breaks"?

Graphvein follows the links in the facts: a route, the calls matched to it,
the tables its handler uses. It does not run your code, so it can show what a
change reaches, but not whether anything stops working.

## Why is a call I expected not linked to its route?

Open [API](#/api) and select the count it falls under. Unmatched: the URL is
known, but no route matches it. Dynamic: the URL is built at runtime.
Wrapper: the call is inside a helper function, so the helper's own call sites
are listed instead. [What Graphvein couldn't read](#/unread) lists these and
the other gaps in one place.

## What doesn't Graphvein see yet?

Some code it does not read or model today: languages it reads only in
part, router forms it cannot follow, schema statements it cannot evaluate.
Each gap shows up as unknown, unresolved or a label, never as a fact. The
[known gaps](../known-gaps.md) page lists them by area; for your own index,
[What Graphvein couldn't read](#/unread) lists what it missed.

## What does Unknown mean in Changes?

Something Graphvein could not establish in one of the two indexes it
compares. An unknown is never shown as removed or added, and something not
extracted is never counted as zero or as "no users".

## Which languages and frameworks are covered?

Go, Python, TypeScript, JavaScript and Rust files can be read precisely;
Java, C# and Dart files get their structure only and are labelled Best
effort. The label that counts is the one on the screen: every file and every
group of facts shows Precise or Best effort, with the reason.

## My frontend and backend are in separate repositories. Does that work?

Yes, as a project: `graphvein project init`, then `graphvein index --project`
and `graphvein serve --project`. Calls in one repository are then linked to
routes in the other, labelled inferred. See [Project](#/project).

## Can my coding agent use this?

Yes. `graphvein mcp` is a read-only MCP server over the same index: an agent
can ask for routes, callers, schema and change impact instead of reading the
code again. See [Beyond the UI](#/help?at=beyond-the-ui).

## How do I open the code in my editor?

The first time you open a link while nothing chose an editor, Graphvein
asks: pick VS Code, VS Code Insiders, Cursor, VSCodium, IntelliJ IDEA
(macOS only) or Zed, and the link opens there (Jump-to asks the same
way). The small icon beside an editor's name says how far its links are
verified; hover, focus or tap it to read why. Leave "Remember for this
browser" checked to keep the choice; unchecked, it lasts until the page
reloads. Change it any time under [Editor in Settings](#/settings?at=editor);
every editor link's tooltip names the editor it opens. A repository can
also set its editor or its own link in `graphvein.yaml`: then links open
directly, without asking.

## Is what I see up to date?

The badge in the top bar says Fresh, or Stale with the number of files
changed since the last index, or Freshness unknown when Graphvein cannot
check (the check failed, for example a file could not be read). When `graphvein.yaml`
changed since the last index it adds "settings changed". Re-index in the
top bar updates it.

## How do I share what I am looking at?

The address in the browser holds the view, so a copied address opens the
same view. The link button after the breadcrumb copies it for you. Every
heading on this page has its own link too.
