# The sections

Each part of Graphvein answers one kind of question. Every view links down
to the exact line, which opens in your editor.

## Overview: the repository at a glance

What is this repository? The [Overview](#/overview) opens first. It lists
the parts on the map (and the workspace libraries in the band below them),
the routes and client calls with how many are linked, the database tables
Graphvein reads, and what it could not read. A count it does not have is
left out, never shown as zero. The label-style question is asked here once.

## API: routes and their callers

Which calls reach this route? [API](#/api) lists every route Graphvein found
(HTTP routes, Tauri commands and Next.js server actions) and, for the one you
pick, the calls matched to it. The cards above the list count the calls that
are linked, unmatched, dynamic or behind a wrapper; select a count to see the
calls behind it. The Flow tab draws a route's callers, its handler and the
tables it reads and writes. In a project, calls from the other repositories
are listed too.

## Changes: what a change reaches

What did this change touch, and what depends on it? The compare chip in the
top bar picks what to compare with: the previous index, a pinned index or a
branch. [Changes](#/changes) lists what changed in the facts. The Impact tab
shows what a change reaches: the paths from a changed fact to the code that
depends on it, each with the number of its weakest step. The Depth control
(1 to 10, 3 unless you choose another) sets how many steps it follows.
"Reaches" means a path exists in the facts; Graphvein does not run your code,
so it cannot tell whether the behaviour changes.

## Architecture: the map and the chain

How do the parts connect? [Architecture](#/architecture) draws the
components (apps, services, databases, external systems) and the links
between them. Select a card or a row to follow its chain step by step; the
Depth control (1 to 10, 6 unless you choose another) sets how far.

## Schema: tables and what they rest on

What is in the database? [Schema](#/schema) shows the tables built from your
SQL migrations and ORM models, column by column, with the routes that use
them. The Assumptions tab lists what a schema fact rests on, for example that
the committed migrations apply in order.

## Explorer: API documentation and Try it out

What does this API accept and return? [Explorer](#/explorer) is API
documentation generated from the index. The Spec view shows the OpenAPI
document, which you can download as JSON or YAML. Try it out sends a request
to an environment you set in `graphvein.yaml` (or the project manifest),
always through the Graphvein server so its guardrails apply: a GET is sent
at once, anything else first shows you the exact request to confirm, and an
environment labelled production asks you once before any request.

## Project: several repositories as one

Is the frontend in another repository? A project indexes several
repositories together. Its link pass matches calls in one member to routes
in another; such a link is always inferred and shows what it assumes. When
more than one member could serve a call, it stays ambiguous until you
declare which one it uses. The [Project](#/project) view lists the members
and their links; it appears only when Graphvein serves a project.

## Workspace: every folder you indexed

Started with `graphvein serve` and no folder, the sidebar lists every folder
and project you indexed. Select one to switch to it, or use Add to index
another folder; once it is indexed, Graphvein offers to open it.

## The sidebar

The sidebar has one width on every section. The button beside the logo (or
`[`) collapses it to its icons, and the button under the logo expands it
again; the choice is kept in this browser. In a window narrower than about
1100 px it starts as icons, and expanding opens it over the page until you
pick a section or press Esc.

## Settings

What is configured, and where does it come from? [Settings](#/settings)
shows the values from `graphvein.yaml` and the choices kept in this browser,
each with where its value comes from. [Storage](#/settings?at=storage)
comes first: where this machine keeps indexes, with Move to put one
elsewhere and the folder new indexes go to. It is this machine's, not
`graphvein.yaml`'s, and it opens before any folder is indexed.
[Editor](#/settings?at=editor) is next: the editor the file links open (and
the repository's link template, when `graphvein.yaml` sets one). [Labels and views](#/settings?at=labels)
holds the label style and the Impact and chain depths.

## Jump to

Press Ctrl+K (Cmd+K on a Mac) to find a route, operation, table, symbol or
file by name. Files and symbols open in your editor, the others in their
section. In the workspace it also lists your other folders, to switch to
one.

## What Graphvein couldn't read

What is missing from the picture? [This page](#/unread) lists the routes
without a full path, the calls whose target is not known, the route shapes
with parts not read and the files with no fact stored, each linked to its
code. A count that is not available says so; it is never shown as zero.
