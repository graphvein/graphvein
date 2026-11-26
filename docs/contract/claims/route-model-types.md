# Claim contract: route model types and change impact through models

ADR 0081 (**Accepted**) of T029. Rows `MT-1`..`MT-19` and the
default row `D`; rows `MT-20`..`MT-29` (the schema view) are in
their own section at the end of this file.
The Go table `routeshape.ModelTypeRows`
(`internal/extract/routeshape/modeluse_contract.go`) lists the same rows in
the same order with the same forms, and `TestModelTypeContract` keeps the
two 1:1: every row's forms are written in the table below, and every form
of the code is the form of exactly one row.

A route's request and response shape (rows SM-1..SM-29 in
[route-shape.md](route-shape.md)) names the models its items use. The
shape extractors resolve a named model to one declaration (the same
resolution they use to read its fields); these rows record that
resolution as a fact of the graph, so change impact can reach a route
from a changed model, and say how the impact is labelled.

## Vocabulary

- **Declaration**: an emitted source node of kind `class`, `struct`,
  `type`, `interface`, `type_alias`, `enum` or `union`, or the `variable`
  or `constant` node of a module-level named zod or Joi schema.
- **Use**: one item of the shape (a parameter, a body variant, a response)
  whose schema was read from the declaration, directly or nested.
- **Kind**: the using item's provenance kind (`fact`, `intended`,
  `returned`, `mapped`; row SM-6..SM-10), never the model's.

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| MT-1 | edge uses_model | `edge:uses_model` | One edge per (route, declaration) from the `route` node to the declaration node. An extensible v1 edge kind (contract v1, "Edge"): no contract version change. Evidence: the file and line of every using item (line-only spans, columns 0). |
| MT-2 | attribute model_uses | `attribute:model_uses` | The route node attribute `model_uses` lists one entry per (declaration, section, status, kind): `model` (the declaration's identity), `name` (its node name), `section`, `status` (responses only), `kind`, `rule`, `file` and `line` of the first using item; `confidence` only for a mapped use. An optional open attribute: no contract version change. A route with no proven use has no attribute. |
| MT-3 | proven declarations only | `declaration:resolved`, `declaration:emitted` | A use is recorded only for the declaration the shape extractor itself resolved (one unique definition, never a name guess) and only when the indexer emits that node with a declaration kind; any other use is dropped from the attribute and has no edge. A declaration in a file that is not emitted (a rejected TypeScript file, a dropped file) is never an edge target. |
| MT-4 | FastAPI models | `rule:SH-F22`, `python:base` | A Pydantic or SQLModel class read by SH-F22, and every repository base class whose fields it inherits, for every item whose schema refers to the model (body, response model, form model). |
| MT-5 | TypeScript types and validators | `rule:SH-E22`, `ts:validator_const` | An interface or type alias read by SH-E22 (Express, Pages Router, App Router), and the module-level const that names a zod or Joi schema read by SH-E20 or SH-E21 (followed through bindings and imports as the reader follows them). |
| MT-6 | Go structs | `go:named`, `go:embedded`, `go:form` | A module named type that gin, chi or net/http encodes or binds (encoding/json, gin's binders), every embedded struct whose fields encoding/json promotes, and a struct gin's form, query or uri binder maps to parameters (each such parameter uses it). |
| MT-7 | Rust serde types | `rust:serde_type` | A struct or enum an Axum extractor or response reads through serde (Json, Form, Query, Path), resolved by the Axum shape reader. |
| MT-8 | nested models | `nested:field`, `nested:items`, `nested:ref` | A declaration read for a field, an array element or a referenced model of the item's schema is a use of the same item. A schema that several values answer with (merged to unknown) keeps the declarations of every value. |
| MT-9 | kind label | `kind:fact`, `kind:intended`, `kind:returned` | The use takes the using item's kind. One declaration used by items of different kinds or sections has one entry per (section, status, kind). |
| MT-10 | mapped uses | `kind:mapped` | A mapped item (a committed spec, a typed frontend client) uses a declaration only if its own model names one; today none does (spec schemas are not code declarations), so no mapped use is stored. When one is, its entry carries the link's `confidence`, and an edge whose uses are all mapped is `inferred` with the strongest mapped confidence. |
| MT-11 | edge provenance | `provenance:fact`, `provenance:inferred` | The edge is a `fact` (confidence 1) when at least one use is read from code (fact, intended or returned): the code names the type. Otherwise MT-10. The kinds stay on the uses: an intended or returned type is labelled as such, never as fact. |
| MT-12 | no type, no use | `kind:read_in_handler`, `schema:not_read` | An item of kind `read_in_handler`, an item without a schema and a section that is not read use nothing. Absence of an edge never means "uses no model". |
| MT-13 | changes: edge on its route | `label:models_changed`, `field:model_edges` | A gained or lost `uses_model` edge is reported on its route's C3 item, labelled `models_changed`, with the declarations gained and lost in the field `model_edges` (as `handler_changed`, Revision 1). A route added or removed carries its edges as context. |
| MT-14 | impact relation I16 | `relation:I16` | From a seed or reached declaration node, follow incoming `uses_model` edges to the route. A changed model (C2 `body_changed`, `added`, `removed`) reaches every route whose stored shape uses it; the traversal continues from the route by I3 (its callers and the operations documenting it). |
| MT-15 | wording | `wording:through_model` | The path's wording names the role and the kinds: "through request body (fact) model UserIn", "through response (returned) model Pet", "through query parameter (fact) model Filter", roles joined by "and". The step carries `model` with the declaration name and the route's uses of it (section, status, kind, rule, file, line). A route that stores no uses keeps the relation wording "through request/response model" and the name. |
| MT-16 | mapped never a fact | `claim:mapped_inferred` | A step whose edge is `inferred` (all uses mapped) makes the path inferred ("may depend on"); a mapped use beside a fact use is still listed with kind `mapped`. |
| MT-17 | caps | `cap:depth`, `cap:fan_out`, `cap:reached` | I16 steps count against the traversal's depth (default 3, maximum 10), fan-out (200) and reached (2,000) caps like every relation; a binding cap is reported. |
| MT-18 | a changed field inside a model | `seed:declaration_body` | A field is part of its declaration's span, so a changed, added or removed field changes the declaration's content hash (C2 `body_changed`) and the declaration is the seed. A field changed in a repository base reaches the routes through the base's own edges (MT-4). |
| MT-19 | UI | `ui:impact_step` | The Changes and Impact views show the I16 step with its wording and one provenance chip per use (fact, intended, returned, mapped), in the existing path list; no new section. |
| D | anything no row covers | | Not recorded: a declaration not resolved by the shape extractor, a type alias that Rust or TypeScript expands (only the final declaration is recorded), the target of a Go type alias (only the alias is recorded), a type declared inside a Go handler or an enclosing function (not resolved by the Go shape reader), server action arguments (their `server_action` node is not a route), a FastAPI `Query()`, `Header()` or `Cookie()` parameter typed with a model (the shape does not read it). |

## Schema view: routes that accept or return a database model (MT-20..MT-29)

The schema view's table detail lists the routes whose request or response
uses the table's model, and the API section's route detail notes which of
its shape's models are also database tables. Both read the stored route
shapes (rows SM-1..SM-29 of the [route shape claims](route-shape.md)) and
the stored model tables; nothing is extracted again. A link needs proven
identity: the shape's model and the table are the same declaration node.
A class that copies a table's fields, or has its name in another file, is
a different declaration and is never linked: there is no matching by
fields or names. Where Graphvein cannot prove that no route uses a model,
it says which routes were not fully read instead of "none".

Implemented by `internal/server/modeltables.go`; the endpoint fields are
documented in [serve](../../cli/serve.md) (`model_routes` on
`GET /api/v0/schema/table`, `model_tables` on `GET /api/v0/api/endpoint`).
`TestModelTableContractTable` keeps this table 1:1 with the code's row
list (`modelTableRows`) and checks that every row names an existing test.

| Row | Topic | Rule | Never |
|---|---|---|---|
| MT-20 | same declaration | A shape model of kind `fact`, `intended` or `returned` is the declaration of a model table when its provenance file and line are the file and start line of a stored `class` node whose name is the model's name (or the file-qualified `Name__...` the FastAPI shape extractor gives a name two classes share), and the table's `class` attribute is that class node's identity. Today only SQLModel tables (`table=True`) store their class node, so the routes linked are FastAPI routes using SQLModel table classes. | a model mapped from a committed spec; another line, file or name; a table whose `class` attribute is not a class node identity |
| MT-21 | request body | A request body item whose schema is a reference to the model, directly or through arrays (`nullable` included): direction `request`, section `body`, the item's content type and provenance. | |
| MT-22 | request parameter | A path, query, header or cookie parameter whose schema refers to the model: direction `request`, the section and the parameter's name. | a security requirement (not a request value) |
| MT-23 | response | A response item whose schema refers to the model, directly or through arrays: direction `response`, section `responses`, the status and content type. | |
| MT-24 | nested in another model | A reference inside an object's fields or another model of the same shape (`TeamList.data: list[Team]`): the use is `nested`, and `via` lists the models it was reached through, outermost first. Each model is entered once per path, so a cycle ends. | |
| MT-25 | table detail list | `model_routes.state` is `listed` when at least one route uses the model: one entry per route and shape model, by served path, method and identity, with every use and its provenance (the chip), and `links`, the editor link of each use's file and line. | |
| MT-26 | absence | When no listed route uses the model: `none` only when every stored route has a valid shape whose path, query, header, cookie, body and response sections are each `complete` or `provably_none` as the code proves them (a spec-only section is not read) and every schema they hold is read to its end (no `unknown`, no unread schema, no object whose `additional` is not `forbidden`, such as a map `dict[str, Hero]` whose value schema is not stored, no reference to a model missing from the shape or mapped from a spec); otherwise `none_found`, with `routes_not_fully_read`; `not_read` when no route stores a shape (or the repository has no route). | "no route uses this model" from a partial read |
| MT-27 | no declaration | A model table whose `class` attribute is not a stored class node identity (every model tool but SQLModel today): `no_declaration`, no list. A migration table has no `model_routes` field: it has no declaration a route could use. | |
| MT-28 | route detail | `model_tables` on the route detail (HTTP routes with a valid shape): each shape model that is a table by MT-20, with the table's identity, name, set and tool, by model name then table identity; `[]` when none is; absent without a shape. | |
| MT-29 | everything else | Every other model is not linked and makes no claim: a TypeScript interface or type alias (never a table's declaration), Go and Rust bodies (their shapes hold inline schemas, not models), a Pydantic class with the same fields or name as a table, a model of a table tool that does not store its class node. | |
