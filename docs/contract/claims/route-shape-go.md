# Claim contract: gin, chi and net/http route shape

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-G1`..`SH-G38` and
the default row `D`. They say which Go code forms the route shape of a gin
v1, chi v5 or net/http route reads, and what each one may claim in the
shared model of the [route shape claim contract](route-shape.md) (rows
`SM-*`). The Go table `goShapeRows`
(`internal/extract/routes/goshape_contract.go`) lists the same rows in the
same order with the same forms, and `TestGoShapeContractTable` keeps the
two 1:1; `TestGoShapeFixtureRows` checks that the go-shape fixture
(`testdata/fixtures/go-shape`) exercises every row.

A form no row lists goes to `D`: it claims nothing, and the section it
would have filled says so in its reason when the form is a known one (a
gin call such as `c.XML`, `c.ShouldBindWith` or `c.QueryMap`, `w.Write`, a
name passed as a value).

## How the shape is read

The handler of a route (its function, method, or the function literal of
the registration) is read from the cached syntax of every function that
declares a parameter of type `*gin.Context`, `*http.Request` or
`http.ResponseWriter` (the "request values"; section "Payload"). Its calls
are read in source order. A call that passes a request value to a function
of the module is followed when its target is proven (row SH-G32), up to
the run's shape depth (`index.shape_depth`, default 6). Every item names
its row, the file and line where it was read, and the calls followed to
reach it.

Precision rules that hold for every row:

- A name read in code (`c.Query("q")`) is a parameter of kind
  `read_in_handler`: no type, presence `unknown` (SM-12). A conversion of
  the read value is noted by the row (SH-G4), never as a type.
- `required` comes only from a gin binder's validation (`binding:"required"`)
  of kind `fact`; `optional` only from a proof (`omitempty` on a response
  field, a form `default=`).
- A type is read only from a declaration in the module; anything that
  makes encoding/json or gin read it differently than its fields (a
  marshaling method, a generic, an interface, a type outside the
  repository) is `unknown` with the reason (SH-G31).
- A status is read only from a literal, a `net/http` `Status` constant or
  a parameter its caller passed one to (SH-G25); a write whose status is
  not read adds no response.
- Every read section is `partial`: middleware is not read, and the reason
  lists what else may be missing (calls not followed, the depth cap,
  names passed as values). Security comes from the route and router
  middleware (attribute `group_middleware`) by the rows of the [route
  security claim contract](route-shape-security.md); without a source it
  is `not_read`.

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SH-G1 | gin path parameter read | `gin:Param` | `c.Param("id")` on a `*gin.Context`: path parameter `id`, `read_by_handler`, `read_in_handler`, presence unknown, no schema. |
| SH-G2 | chi path parameter read | `chi:URLParam` | `chi.URLParam(r, "id")` with `r` a request value and `chi` the chi (v5 or earlier) import: as SH-G1. |
| SH-G3 | net/http path parameter read | `http:PathValue` | `r.PathValue("id")` on a `*http.Request`: as SH-G1. |
| SH-G4 | conversion of a read value | `strconv:Atoi`, `strconv:ParseInt`, `strconv:ParseUint`, `strconv:ParseFloat`, `strconv:ParseBool` | A parameter read (SH-G1..SH-G3, SH-G5, SH-G6, SH-G9, SH-G10) whose value is parsed by the strconv function, directly or through a local declared once from the read: the item takes this row (the conversion is noted), still `read_in_handler` with no schema and presence unknown: the parse proves what the handler expects, not what the route enforces. |
| SH-G5 | gin query read | `gin:Query`, `gin:DefaultQuery`, `gin:GetQuery`, `gin:QueryArray`, `gin:GetQueryArray` | Query parameter by name, as SH-G1. |
| SH-G6 | net/http query read | `http:URL.Query().Get`, `http:URL.Query().Has` | `r.URL.Query().Get("x")`, or `Get`/`Has` on a local declared once from `r.URL.Query()`: query parameter, as SH-G1. |
| SH-G7 | FormValue | `http:FormValue` | `r.FormValue("x")` reads the query and form bodies: a query parameter and a body form field, both as SH-G1; the query section notes it. |
| SH-G8 | form body field read | `gin:PostForm`, `gin:DefaultPostForm`, `gin:GetPostForm`, `gin:PostFormArray`, `gin:GetPostFormArray`, `gin:FormFile`, `http:PostFormValue`, `http:FormFile` | One body item of kind `read_in_handler` per route: content type not read (null), an object of the names read, each presence unknown and type unknown, additional unknown. |
| SH-G9 | header read | `gin:GetHeader`, `http:Header.Get`, `http:Header.Values` | `c.GetHeader("X")`, `r.Header.Get("X")`, `r.Header.Values("X")`: header parameter, as SH-G1 (names compare case-insensitively). |
| SH-G10 | cookie read | `gin:Cookie`, `http:Cookie` | `c.Cookie("n")`, `r.Cookie("n")`: cookie parameter, as SH-G1. |
| SH-G11 | gin JSON bind | `gin:ShouldBindJSON`, `gin:BindJSON` | `c.ShouldBindJSON(&v)` / `c.BindJSON(&v)` with `v` of a type SH-G30 reads: body `application/json` of kind `fact` (SH-G15 otherwise), the schema of the type by SH-G26 with gin's validation (SH-G27); objects of the type have additional `unknown` (a program may set gin's decoder to refuse unknown fields). |
| SH-G12 | gin Bind by Content-Type | `gin:ShouldBind`, `gin:Bind` | On a route whose method is not `GET` (H18): gin chooses the binder from the Content-Type, so the body item's content type is null; the schema is the type's (as SH-G11) only when its JSON and form names agree field by field, else `unknown`; no field is required (the form binder also takes values from the query, and XML, YAML and TOML read their own names), types stay enforced. On a `GET` route gin binds the query: as SH-G13. |
| SH-G13 | gin query bind | `gin:ShouldBindQuery`, `gin:BindQuery` | `c.ShouldBindQuery(&v)` (or `Bind`/`ShouldBind` on a `GET` route): one `declared` query parameter per field gin's form mapping reads (SH-G28), kind `fact` (SH-G15 otherwise), with the field's schema and presence (SH-G27). A struct with a field type the mapping is not modelled for claims nothing (noted). |
| SH-G14 | gin uri bind | `gin:ShouldBindUri`, `gin:BindUri` | As SH-G13 for the `uri` tag, in the path section, only for names that are parameters of the registration's own pattern (others noted); a `fact` path parameter is required (SM-16). |
| SH-G15 | unchecked bind or decode | `check:unchecked` | A `ShouldBind*` or `Decode` whose error is not tested against nil in an `if` whose branch ends in `return` (directly, or by the statement right after the assignment), or that is read in a followed function some call of which up the chain is not itself checked that way: the type is not enforced, so the item is `intended` (and never required). gin's `Bind*` answer 400 themselves and stay `fact`. |
| SH-G16 | encoding/json decode | `json:Decoder.Decode` | `json.NewDecoder(r.Body).Decode(&v)` with `r` a request value (or gin's `c.Request`): body `application/json`, kind `fact` (SH-G15 otherwise); fields presence unknown and nullable (encoding/json accepts a missing field and null for every field), additional `allowed` (a fresh decoder accepts unknown fields). |
| SH-G17 | gin automatic 400 | `gin:bind-400` | `BindJSON`, `Bind`, `BindQuery`, `BindUri`: response `400` of kind `fact`, content type and body not read (gin aborts with 400 when binding fails). |
| SH-G18 | gin JSON response | `gin:JSON`, `gin:IndentedJSON`, `gin:PureJSON`, `gin:AbortWithStatusJSON` | `c.JSON(code, v)`: response with the status (SH-G25), `application/json`, the schema of `v` (SH-G29, SH-G30, SH-G26), kind `returned`. |
| SH-G19 | gin string response | `gin:String` | `c.String(code, ...)`: `text/plain`, schema string, kind `returned`. |
| SH-G20 | gin status only | `gin:Status`, `gin:AbortWithStatus`, `gin:AbortWithError`, `gin:Redirect` | A response with the status, content type and body not read, kind `returned`. `c.Status` only sets the status: it is claimed only in the handler itself and when no later write (a render, `WriteHeader`, an abort, a call passing `c`) can replace it; `AbortWithStatus`/`AbortWithError` write it at once. |
| SH-G21 | net/http WriteHeader | `http:WriteHeader` | `w.WriteHeader(code)`: a response with the status, body not read; dropped when another item of the same status reads the body. A status written after the response was already sent in the same or an enclosing block (any `WriteHeader`, abort or body write before it) is never sent and is not claimed; nor is a 1xx, 204 or 304 body. |
| SH-G22 | net/http Error and NotFound | `http:Error`, `http:NotFound` | `http.Error(w, msg, code)`, `http.NotFound(w, r)` (404): `text/plain`, schema string. |
| SH-G23 | net/http Redirect | `http:Redirect` | `http.Redirect(w, r, url, code)`: a response with the status, body not read. |
| SH-G24 | encoding/json encode | `json:Encoder.Encode`, `http:Header().Set` | `json.NewEncoder(w).Encode(v)` (also on gin's `c.Writer`): the status is the last `WriteHeader`, gin `c.Status` or abort before it in the same or an enclosing block, when no write to the writer in another block can reach it (a block ending in `return` cannot); with no earlier write at all, `200` in the handler itself (never in a followed function: its caller may have written). Content type from an earlier `w.Header().Set("Content-Type", "...")` on every path, set before any status was written (a header set after `WriteHeader` is not sent), else not read. The schema of `v` as SH-G18; none for 1xx, 204 and 304. |
| SH-G25 | status values | `status:literal`, `status:constant`, `status:argument` | An integer literal 100..599, a `net/http` `Status...` constant, or a parameter its caller passed one of these to. |
| SH-G26 | struct schema | `json:struct` | A module struct by encoding/json's rules: exported fields; the `json` tag's name or the field name; `-` skipped; `omitempty`/`omitzero` make a response field optional; the `string` option makes a scalar a string; embedded structs flattened breadth first with Go's dominance (the shallowest name wins, a tagged one breaks a tie, otherwise the name is dropped); fields promoted through an embedded pointer are optional in responses (a nil pointer writes none); pointers, slices and maps nullable in responses; `[]byte` a string; `time.Time` (used directly) a string; maps of string or integer keys objects with additional allowed. Response objects have additional `forbidden`. |
| SH-G27 | gin validation presence | `tag:binding-required`, `tag:form-default` | Under a gin binder of kind `fact`, `binding:"required"` (not after `omitempty` or `dive`) makes a field required, which for a non-pointer scalar also refuses its zero value; never on a struct-kind field (other than `time.Time`, which validator does not check), on a field promoted through an embedded pointer (nil is not validated), or on the elements of a slice, array or map field without `dive` (the elements of the bound value itself are validated). A form `default=` option makes a query field optional. A `validate` tag is not gin's and claims nothing. |
| SH-G28 | gin form mapping | `tag:form`, `tag:uri` | gin's form, query and uri binders read exported fields by the tag's name or the field name, skip `-`, flatten untagged embedded structs, and read scalars, slices of scalars and `time.Time` (an integer with `time_format:"unix"` and its milli, micro and nano variants, else a string). A type with an `UnmarshalParam`, `UnmarshalText` or JSON method is not read (gin may call it). |
| SH-G29 | inline map response | `gin:H`, `literal:map` | A `gin.H{...}` or `map[string]T{...}` literal with distinct string literal keys: an object of those keys, additional `forbidden`; values typed when literals or typed values, else unknown. |
| SH-G30 | value types | `type:composite`, `type:var`, `type:new`, `type:parameter`, `type:argument` | The type of a value: a composite literal `T{...}` or `&T{...}`, `new(T)`, `&x`, a name declared once with a type or one of those values, a parameter's declared type, or, for a parameter of type `any`/`interface{}`, the type of what the caller passed. Anything else is not read (no schema). |
| SH-G31 | refused types | `unknown:marshaler`, `unknown:external`, `unknown:generic`, `unknown:interface`, `unknown:recursive`, `unknown:embedded`, `unknown:defined` | `unknown` with the reason: a type with (or embedding one with) `MarshalJSON`, `UnmarshalJSON`, `MarshalText` or `UnmarshalText`; a type outside the module; a generic type or instance; an interface; a recursive reference; an embedded type outside the module or generic (its promoted methods are not visible: the whole value is unknown); a defined type of another named type (`type Stamp time.Time` keeps no methods). |
| SH-G32 | proven call targets | `call:function`, `call:package-function`, `call:receiver-method`, `call:typed-method`, `call:result-method` | A call that passes a request value is followed when its target is the unique module function `f` or `pkg.F`, a method of the function's own receiver type, a method of the declared concrete module type of a parameter or local, or a method of the single declared result type of the module function that declared the local (`v := NewV(); v.Bind(c)`); the callee must declare a request parameter of the same role. Literal names and statuses and the types of values passed to `any` parameters go with it (SH-G25, SH-G30, SH-G34), unless the callee assigns that parameter. |
| SH-G33 | calls not followed | `stop:unresolved`, `stop:ambiguous`, `stop:external`, `stop:opaque_type`, `stop:transformed` | A method of an interface type (`ambiguous`), a function of another package (`external`), a function value, a field's method or no unique definition (`unresolved`), a callee parameter of another type (`opaque_type`), a request value the function reassigns (`transformed`). Recorded with the site; the sections say so. |
| SH-G34 | names passed in | `name:literal`, `name:constant`, `name:argument` | A read's name is a string literal, a package-level string constant, or a parameter its caller passed one of these to (H20). Any other name claims nothing (noted). |
| SH-G35 | gin's request and writer | `gin:Request`, `gin:Writer` | `c.Request` is a request value and `c.Writer` a writer value: the net/http rows read them, and calls passing them are followed. |
| SH-G36 | generated servers | `oapi:generated` | A route of an oapi-codegen generated server (H21): every section `not_read` with reason "generated from spec"; the committed spec is the shape source. |
| SH-G37 | section states | `state:partial`, `security:not_read` | Every section the handler was read for is `partial` with a reason (middleware is not read; what was not followed); a section with nothing read says so. Security stays `not_read` unless a row of the route security claim contract reads a source (SH-S19..SH-S22). |
| SH-G38 | chain records | `follow:one-per-route` | One follow per route carries all request values; each chain is stored once, under the first section its last function contributed to, else `responses`. |
| D | anything no row covers | | Not claimed: the section stays `partial` with its reason. |

## Hazards

| Hazard | Row |
|---|---|
| H18 gin `Bind`/`ShouldBind` choose the binder from method and Content-Type | SH-G12, SH-G13 |
| H19 auth from router-group middleware, not stored | SH-G37 (security `not_read`) |
| H20 parameters and bodies read through helpers | SH-G32, SH-G34 |
| H21 oapi-codegen generated servers | SH-G36 |

## Payload

The Go payload (`facts.GoFacts`, version 10) caches, for every non-test
file that imports `net/http` or gin:

- `Shapes`: each function, method and function literal declaring a
  parameter of type `*gin.Context`, `*http.Request` or
  `http.ResponseWriter` (with the role of each), its receiver name, its
  block tree and the blocks that end in `return`, the names its body
  declares (with the declared type or the single value, and how often),
  the parameters it reassigns, and every outermost call with its tree, the
  names its results are bound to, whether its error is checked (SH-G15)
  and whether it runs in a closure;

and, for every non-test file, `Marshalers`: the `MarshalJSON`,
`UnmarshalJSON`, `MarshalText`, `UnmarshalText` and gin's `UnmarshalParam` methods by receiver
type. Struct and named types come from the existing `Structs` and `Types`.
