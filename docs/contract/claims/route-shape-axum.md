# Claim contract: Axum route shape

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-X1`..`SH-X29` and
the default row `D`. They say which Rust code forms the route shape of an
Axum route (axum 0.3 to 0.8) reads, and what each one may claim in the
shared model of the [route shape claim contract](route-shape.md) (rows
`SM-*`). The Go table `axumShapeRows`
(`internal/extract/routes/axumshape_contract.go`) lists the same rows in
the same order with the same forms, and `TestAxumShapeContractTable`
keeps the two 1:1; `TestAxumShapeFixtureRows` checks that the fixtures
(`testdata/fixtures/route-shape-axum`, axum 0.8, and
`testdata/fixtures/route-shape-axum03`, axum 0.3) exercise every row.

A form no row lists goes to `D`: it claims nothing, and the section it
would have filled says so in its reason.

## How the shape is read

The handler of a route is the function or method the route registration
names (an inline closure or a service is not read: every section but the
path stays `not_read` with the reason). Its parameters are Axum
extractors, classified by their type resolved through the file's imports
(never by the parameter's name). Its result is read from the declared
return type and, where the type does not say it, from the expressions the
body returns. Request values (`HeaderMap`, the raw request, the parts of
a repository extractor) and returned calls are followed through proven
call targets (row SH-X28) up to the run's shape depth
(`index.shape_depth`, default 6).

Precision rules that hold for every row:

- `required` comes only from serde's derived `Deserialize` of a type
  Axum's extractor enforces (Path, Query, Json, Form), of kind `fact`:
  a field that is not an `Option`, has no `default`, no `alias` and no
  custom deserializer. A name read in code (`headers.get("x")`) is
  `read_in_handler`, presence unknown, no schema (SM-12, SM-13).
- A schema is read only from a type that derives the serde trait for the
  direction (Deserialize for requests, Serialize for responses), with
  only the attributes rows SH-X16 and SH-X17 list; anything else in the
  type is `unknown` with the reason (SH-X18).
- Responses are of kind `returned`: in a returned schema a field is never
  `required` (SM-14): presence is `optional` with `skip_serializing_if`,
  else `unknown`. The responses section is always `partial` (panics,
  extractor rejections and middleware are not read).
- Security is `not_read`; cookies are `not_read`.

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SH-X1 | path template parameters | `template:{name}`, `template:{*name}`, `template:none` | The route's composed templates (axum 0.3 `:name` and `*name` normalized to `{name}`, `{*name}`) give the path parameters: `declared`, kind `fact`, `required`; the section is `complete` with the registration as evidence, or `provably_none` when the template has none. Templates of one route with different parameters, or a repeated name, make it `partial` with no items. |
| SH-X2 | Path scalar or tuple | `extractor:Path<scalar>`, `extractor:Path<tuple>` | `Path<T>` with a scalar `T` (one template parameter) or a tuple with one element per parameter, in template order: each parameter takes the element's scalar schema (SH-X19). Any other arity claims no schema. |
| SH-X3 | Path struct | `extractor:Path<struct>` | `Path<T>` with a struct deriving `Deserialize`: a parameter takes the schema of the field of its name after serde's renames (SH-X16). Maps and sequences claim no schema. |
| SH-X4 | Query struct | `extractor:Query<struct>`, `unknown:url-encoded-type` | `Query<T>` with a struct deriving `Deserialize`: one `declared` query parameter of kind `fact` per field serde reads, by its serde name; presence by SH-X16 (`Option` and `default` optional, else required). Only a type serde_urlencoded reads from one value has a schema: `bool`, the integers up to 64 bits, `f32`/`f64`, `String`, `char`, a unit enum, a newtype of one of these, or an `Option` of one. Any other field type (a nested struct, `u128`/`i128`, a sequence, a map, a library type) has schema `unknown`; its presence still comes from serde. |
| SH-X5 | Query map reads | `extractor:Query<HashMap>`, `read:map.get` | `Query<HashMap<..>>` accepts any parameter: the section is `partial`; `map.get("x")` on its binding adds `x` as `read_in_handler`. |
| SH-X6 | Json body | `extractor:Json<T>` | One body item `application/json`, kind `fact`, the `Deserialize` schema of `T` (SH-X16..SH-X20). |
| SH-X7 | Form | `extractor:Form<T>`, `method:ANY`, `method:HEAD` | On a `GET` route `Form` reads the query string: as SH-X4. axum's `RawForm` tests for `GET` only in every version from 0.3 to 0.8.8 (the documentation of 0.6.7 and later also names `HEAD`, the code does not), so on a `HEAD` route `Form` reads the body, as on any other single method. On a route answering several methods (`any`) it reads the query on GET and the body otherwise: query and body `partial`, no items. On any other method, `HEAD` included: one body item `application/x-www-form-urlencoded`, kind `fact`, with the URL-encoded schema of `T` (a struct or map; any other type `unknown`). |
| SH-X8 | raw body | `extractor:String`, `extractor:Bytes` | A `String` or `Bytes` parameter reads the whole body: one body item, content type `null` (any is accepted), schema string or `unknown` (raw bytes), kind `fact`. |
| SH-X9 | one body extractor | `body:several` | Only one extractor can read the body (H16; axum 0.6+ refuses others at compile time, 0.3 at run time): with several, no body item is claimed and the section says so. A repository `FromRequest` extractor may read the body (section `partial`); a `FromRequestParts` one cannot. |
| SH-X10 | typed headers | `extractor:TypedHeader<T>`, `extractor:Option<TypedHeader<T>>` | `TypedHeader<T>` with `T` a known type of the headers crate (`UserAgent` -> `user-agent`, `Authorization` -> `authorization`, ...): header `declared`, kind `fact`, schema string, `required`; inside `Option`, `optional`. Other typed headers claim nothing. |
| SH-X11 | HeaderMap reads | `read:headers.get`, `read:headers.get_all`, `read:headers.contains_key`, `name:literal`, `name:header-constant`, `name:const`, `version:HeaderMap-takes` | `get`, `get_all` or `contains_key` on a `HeaderMap` (or `req.headers()`, `parts.headers` of a request value) with a string literal, an `http::header` constant (`AUTHORIZATION` -> `authorization`) or a repository string constant: header `read_by_handler`, kind `read_in_handler`, presence unknown, no schema. Any other use of the map (`iter`, a name not read) makes the section `partial` with the site. Before axum 0.5 the `HeaderMap` extractor takes the headers, so a later `Json`, body `Form` or `TypedHeader` extractor always fails: unless the crate's axum version is proven to be 0.5 or later, such a later extractor claims nothing, the handler's responses are not claimed (it may never run), and the sections say why. |
| SH-X12 | extractors without request parameters | `extractor:State`, `extractor:Extension`, `extractor:Method`, `extractor:MatchedPath`, `extractor:ConnectInfo`, `extractor:NestedPath` | Claim nothing and leave the sections they cannot reach unchanged. |
| SH-X13 | repository extractors | `impl:FromRequestParts`, `impl:FromRequest` | A parameter whose type has exactly one `FromRequestParts` (`from_request_parts`) or `FromRequest` (`from_request`) impl in the repository is followed into that method with its request parts value (one call deep); header reads there and in functions it passes them to are `read_by_dependency` with the chain. A type with no such impl read goes to SH-X15. |
| SH-X14 | raw request values | `extractor:Request`, `extractor:Parts`, `extractor:RequestParts`, `extractor:Uri`, `extractor:OriginalUri`, `extractor:RawQuery`, `read:dynamic`, `read:any-use`, `chain:stopped` | The raw request makes query, cookie and body `partial` (headers read from it are SH-X11); `Uri`, `OriginalUri` and `RawQuery` make the query `partial`. In the handler, an extractor impl (SH-X13) and every followed function, any use of a request or parts value other than a header read of SH-X11 (a field such as `.uri`, a method such as `extract`, a value returned, stored, rebound or passed to a macro) makes query and cookie `partial`, and the body too for a value that holds it (not `http::request::Parts`). A formatting macro whose arguments, outside string literals, are plain names (no call, method, field, index or `?`) only formats the value and is no use, when the macro is proven: a path or import resolving to the `log` or `tracing` level macros (`trace`, `debug`, `info`, `warn`, `error`) or to std's `format`, `print`, `println`, `eprint`, `eprintln`, `write`, `writeln`, or one of those std names unqualified while no `macro_rules!` of that name appears in any Rust source of the repository. Any other macro, a repository macro of the same name included, is a use. So does a chain of such a value that stops anywhere but at its end (a callee not followed, a parameter of another kind, the depth cap), with the chain's reason. |
| SH-X15 | wrapped and unknown extractors | `extractor:Option<X>`, `extractor:Result<X,E>`, `extractor:unknown` | An `Option` or `Result` around an extractor (except SH-X10), or an extractor type not read (a library type, a type without a read impl): query, header and body are `partial` and cookie `not_read`, each with the reason. |
| SH-X16 | serde structs | `serde:derive`, `serde:rename`, `serde:rename_all`, `serde:alias`, `serde:default`, `serde:skip`, `serde:skip_serializing`, `serde:skip_deserializing`, `serde:skip_serializing_if`, `serde:deny_unknown_fields`, `type:Option`, `type:Box<Option>` | A struct deriving the direction's serde trait is an object of its fields by serde name (`rename`, else the container's `rename_all`, applied as serde_derive's `RenameRule::apply_to_field`: lowercase and snake_case leave the field unchanged, UPPERCASE and SCREAMING_SNAKE_CASE upper-case it, PascalCase drops each `_` and upper-cases the character after it, camelCase is PascalCase with the first character lowered, kebab-case and SCREAMING-KEBAB-CASE replace `_` by `-`); skipped fields are left out per direction. Deserialized: additional `forbidden` with `deny_unknown_fields`, else `allowed`; a field is `optional` with `default` (field or container) or an `Option` type (also inside `Box`, `Rc` or `Arc`), `unknown` with `alias`, else `required`; `Option` is nullable. Serialized: additional `forbidden`; `skip_serializing_if` makes a field `optional`, else `unknown`. A newtype struct is its field; a unit struct is `null`. |
| SH-X17 | serde unit enums | `serde:enum-unit` | An enum of unit variants deriving the trait, without a tag attribute: an enum of the variant names after `rename`, `rename_all` (serde_derive's `RenameRule::apply_to_variant`) and `skip`. |
| SH-X18 | refused serde forms | `unknown:no-derive`, `unknown:with`, `unknown:flatten`, `unknown:tagged-enum`, `unknown:data-enum`, `unknown:container-attribute`, `unknown:recursive`, `unknown:tuple`, `unknown:attribute` | `unknown` with the reason: a type that does not derive the trait (a custom impl or none); `with`, `serialize_with`, `deserialize_with` (the field's schema; presence unknown unless `default`); `flatten` (the field is left out, additional `unknown`, the section `partial`); `untagged`, `tag`, `content`, `transparent`, `from`, `into` and any other container attribute; variants with data; a recursive type; a tuple or tuple struct of several fields; an attribute argument not read; a rename rule serde itself refuses. A type is `unknown` too when the item, a field or a variant carries an attribute that may rewrite serde's view and is not read: an attribute macro (`serde_as`, `skip_serializing_none`, any attribute not in the inert list), a `cfg_attr` applying `serde`, a derive or another such attribute, or `cfg` on a field or variant. The inert list: `doc`, lint attributes, `deprecated`, `must_use`, `non_exhaustive`, `repr`, `default`, and the helper attributes of `validate`, `garde`, `schemars`, `schema`, `sqlx`, `diesel`, `ts`, `param`, `into_params`, `oai`, `graphql`, `builder`, `error`, `from`, `source`, `backtrace`. |
| SH-X19 | scalar and library types | `type:scalar`, `type:sequence`, `type:map`, `type:Uuid`, `type:chrono`, `type:serde_json::Value`, `type:external` | `bool` boolean; integers integer; `f32`/`f64` number (serialized: nullable, since serde_json writes NaN and the infinities as `null`); `String`, `str`, `char` string; `Box`, `Rc`, `Arc`, `Cow` their content; `Vec`, `VecDeque`, `HashSet`, `BTreeSet`, arrays an array; `HashMap`/`BTreeMap` an object with additional `allowed`; `uuid::Uuid` and chrono date and time types string; `serde_json::Value` and any other library type `unknown`. |
| SH-X20 | generics and aliases | `type:generic-instance`, `type:alias`, `unknown:generic` | A generic struct's parameters take the arguments written at the use (`UserBody<NewUser>`); type aliases are expanded with their defaults (`type Result<T, E = Error>`); a parameter not instantiated is `unknown`. |
| SH-X21 | Json response | `response:Json<T>`, `response:Json(value)`, `unknown:rebound-name` | A declared `Json<T>` result, or a returned `Json(value)`: response `200`, `application/json`, kind `returned`, the `Serialize` schema of `T`, or of the value's proven type (a struct literal of a non-generic struct, a name with a single `let` binding to one or with a type, a parameter's type when no pattern of the body binds the name again); otherwise schema `null` (not read). Every binding of the body counts: lets of any pattern, match arms, `if let`, `while let`, `for`, closure parameters. |
| SH-X22 | status tuples | `response:(StatusCode,T)`, `parts:content-type` | A returned tuple whose first element is a `StatusCode` constant: a response with that status and the body of the last element (declared `Json<T>`, `String`, or a returned `Json(value)`); a status that is not a constant is not read. The elements between are response parts applied after the body: the body's content type stays only when each is an array of `(header name, value)` pairs none of which names `content-type`; a `content-type` pair with a literal value gives that value; any other part leaves the content type `null` (not read). |
| SH-X23 | status codes | `response:StatusCode`, `status:no-body` | A returned `StatusCode::X` constant (axum, http, reqwest or hyper): a response with that status and an empty body (schema `null`). A 1xx, 204 or 304 response never has a body (schema `null`, content type `null`). |
| SH-X24 | redirects | `response:Redirect::temporary`, `response:Redirect::permanent`, `unknown:Redirect::to` | `Redirect::temporary` is 307 and `Redirect::permanent` 308, body empty. `Redirect::to` is 307 before axum 0.5 and 303 after; the version is not read, so its status is not claimed. |
| SH-X25 | text and empty responses | `response:String`, `response:&str`, `response:Html`, `response:()`, `response:literal`, `version:text` | A declared `String` or `&str`, or a returned string literal: `200`, schema string; `Html<T>` likewise; `()`: `200` with an empty body. The content type depends on the axum version: `text/plain` and `text/html` in axum 0.3, with `; charset=utf-8` since 0.4. It is claimed only when the crate's axum version is proven (see "Axum version"); otherwise it is `null` and the section says why. |
| SH-X26 | Result and error variants | `response:Result<T,E>`, `response:Err(E::V)`, `unknown:?`, `arm:self-match`, `arm:guard`, `unknown:never-ok` | `Result<T, E>` (after aliases): the `Ok(..)` values are read as `T`; when every value is an `Err(..)`, `T` is not answered and claims nothing. An `Err(E::V..)` with `E` a repository enum adds the status of variant `V` when `E`'s single `IntoResponse` impl proves it. Every result of `into_response` (and of a `self.method()` it calls for the status) is read with the match arms on its path: an arm of a `match` on `self` narrows the variants the result answers for (`E::V`, `Self::V` and `\|` alternatives name variants; `_`, a binding or a pattern not read covers every variant no earlier arm of that match names); other matches, `if` branches and early returns only add results. `V` is proven when every result that can answer for it is the same `StatusCode` constant (alone, first in a tuple, before or after `.into_response()`) and no arm on `self` that can match it has a guard; otherwise its status is not read. Content and schema are not read. Errors returned by `?` are not read (the section says so). |
| SH-X27 | impl IntoResponse | `response:impl IntoResponse`, `response:Response`, `response:into_response`, `merge:different-schemas` | For `impl IntoResponse` and `Response`, each returned expression (the tail, `return`, through `match`, `if` and blocks, without `.into_response()`) is read by SH-X21..SH-X25, and a call is followed (SH-X28); any other expression is not read (the section says so). Values with the same status and content type give one response; when their schemas differ (or one is not read), its schema is `unknown`, never one of them. |
| SH-X28 | call following | `call:function`, `call:Self::f`, `call:self.f`, `call:extractor-impl`, `unknown:local-binding` | A call is followed when its target resolves through the module tree to one repository function or method whose shape syntax was read: a path (`helpers::f`), `Self::f`/`Type::f`, `self.f()` in an impl, or the extractor impl of SH-X13. A request value is followed only into a parameter of the same kind (`HeaderMap`, request or parts), a returned call into the callee's result. Methods of other values are `unresolved`, library functions `external`, several definitions `ambiguous`, another parameter kind `opaque_type`; `cycle`, `joined` and `depth_cap` as SM-23..SM-25. A single-segment callee that the function binds itself (a parameter, a local closure, any pattern, a nested function) is `unresolved`, never an item of that name. |
| SH-X29 | section states and chains | `state:path`, `state:query`, `state:header`, `state:cookie`, `state:body`, `state:responses`, `state:security` | Path by SH-X1. Query: `complete` when its items are all `fact` and nothing else may read it, `provably_none` when no extractor can read it, else `partial` with the reasons. Header: `partial` when anything was read (middleware is not read), else `not_read`. Body: `complete` with one item and no other reader, `provably_none` with no body extractor (the signature as evidence), else `partial`. Cookie and security `not_read`; responses `partial`. Header chains are stored under `header`, followed response chains under `responses`. |
| D | anything no row covers | | Not claimed: the section stays `not_read` or `partial` with its reason. |

## Axum version

Forms `version:*` (rows SH-X11, SH-X25) answer differently between
the axum versions the contract covers, and are claimed only when the
crate's axum version is proven. The crate is the nearest `Cargo.toml`
above the handler's file. When a `Cargo.lock` is found at or above it,
the lock wins: the version is the one the lockfile resolves for the
crate's own package (its `axum` dependency entry, or the only locked
`axum`); a lockfile that does not lock the package, or locks no single
axum for it, proves nothing. Without a lockfile the manifest's entry is
read: a requirement of one comparator (`0.8`, `^0.8.8`, `=0.6.7`,
`~0.6.2`) proves its range, `axum = { workspace = true }` reads the
workspace root's entry, and a path dependency its manifest's version.
Several entries that differ, other comparators (`>=`, `*`, several), a
pre-release, a git dependency or a manifest that cannot be read prove
nothing.

## Hazards

| Hazard | Row |
|---|---|
| H16 custom extractors read headers; only the last extractor reads the body; generic wrappers | SH-X13, SH-X9, SH-X20 |
| H17 serde attributes change the schema (`validator` constraints are not read) | SH-X16, SH-X17, SH-X18 |

## Payload

The Rust payload (`facts.RustShape`, version 2, in `facts.RustFacts.Shape`)
is cached for every file whose text mentions `axum`, `serde` or `http`:
type declarations (struct, enum, alias) with their derives, serde
container, field and variant attributes, the names of their other
attributes, generics and field types; and for every function and method
with a body: its parameters (pattern and type), declared return type,
impl self type and trait, the expressions it returns (array literals
included) with the match arms on their path (scrutinee, pattern, guard),
its single-name `let` bindings, every name its patterns bind, the reads,
calls and other uses of its parameter values, and whether it uses `?`.
The axum version is read at link time from the retained `Cargo.toml` and
`Cargo.lock` files, never cached.
