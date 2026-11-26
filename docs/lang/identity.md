# Language symbol identities

Contract v1 canonical escaping remains defined in `docs/contract/v1.md`.
Examples below show decoded qualified names; delimiters such as `@`, parentheses
and colons are percent-encoded in emitted identities. Body edits preserve names;
source spans and hashes include declaration prefixes and decorators where present.
Go and Python files are labelled `precise` (rule rows PL-L1 and PL-L2, `docs/extract/precision.md`) unless their structure claims nothing; every other language is `best_effort`.

## OpenAPI

Imported specs use `api_spec:<file>#spec` (level 2); declared operations use
`api_operation:<file>#<METHOD> <raw path>` (level 4), canonically escaped, for
example `api_operation:openapi.yaml#GET%20%2Fuser`. Fixed methods are uppercase;
OpenAPI 3.2 additionalOperations keys preserve their exact case. Referenced path
items retain the root spec file identity. Server paths, summary and deployment
metadata do not participate in identity. See [OpenAPI import](../extract/openapi.md)
for source regions, hashes and the fragment-edit hash gap.

## TypeScript, TSX and JavaScript

Orval invocation client_call identities use the nearest emitted owner, the
operation's uppercase method, and compact generated callee text in the existing
URL-expression position: `load.GET getGetArticleQueryOptions` or
`load.POST api.login`. Repeated sites append @2/@3 in source order. Generated
output paths and operation metadata do not participate in identity. Spans and
hashes cover the application call expression (ADR 0027).

Lexical owners join with dots: `function:src/jobs.ts#run.validate`,
`method:src/user.ts#User.save`. Functions, bound arrow/function expressions,
classes, interfaces, aliases, enums and exported variables retain their declared
names. Anonymous defaults use `default.function`, `default.arrow`, or
`default.class` only for the function or class that is itself the
default-exported value; named defaults retain their declared name. Since
extractors-41 (ADR 0070) declarations inside namespace and module blocks,
exported binding patterns and type positions get no identity
(`docs/lang/typescript-structure.md`).

HTTP routes use the uppercase method and literal unprefixed pattern, for example
`route:src/api.ts#GET%20%2Fusers%2F%3Aid`. Repeated registrations in one file
append `@2`, `@3` in declaration order (canonically escaped as `%402`, `%403`).
Mount prefixes never participate in identity; see ADR 0013.

Overload signatures append `@overload` followed by parameter and return-type
text, with whitespace/comments outside literals removed; the implementation
keeps its plain name. Example decoded name: `parse@overload(value:string):number`.
Parameter names participate: renaming a parameter changes the overload identity.
This is accepted as semantic in ADR 0006's developer review note. Return-type
changes also change TypeScript overload identity. Literal whitespace is retained.

An object literal directly initializing a declared `const`, `let` or `var`
adds that variable's name as a lexical owner, exported or not. Nested literals
under identifier-keyed or string-keyed properties add the property key:
`first.nested.leaf.save`. Computed keys and unbound literals (arguments, returns,
array elements and spreads) add no owner segment. Member node kinds remain
unchanged. The nearest emitted lexical owner contains a member: the owner
variable if emitted, otherwise the enclosing emitted named declaration, and
the file only when no such declaration exists. Currently only exported object
variables are emitted.

Anonymous functions and callbacks add no owner segment. After semantic naming,
including object owners and overload suffixes, repeated identities within a
file keep the first plain qualified name and append `@2`, `@3`, and so on to
later declarations in source order. Counts are per file and per unsuffixed
identity (including kind). The emitted second helper can be
`function:tests.ts#run.helper%402`. Edges and evidence use the suffixed identities.
Insertion or reordering can change which body receives which suffix; body edits
and moving preceding lines without changing declaration order do not.
Calls to repeated callable identities remain unresolved, including imports,
because anonymous callback bindings are not modeled; unique calls retain their
existing best-effort resolution. See ADR 0011.

### Angular UI routes

Angular route objects use `ui_route:<file>#<owner>.ROUTE <p>` (level 4; ADR
0036). The owner is the nearest emitted lexical owner node containing the
object literal (for example the `routes` variable), else none (file level).
Objects inside a literal `children` array nest under their parent route's
name; objects in separately declared arrays are top-level for their own owner,
however they are reached. `<p>` comes from source literals only: the raw
content of a string or substitution-free, escape-free template literal path,
`""` for an empty path, `(matcher)` when the path is absent and a matcher key
exists, `(no path)` when it is absent otherwise, and `(dynamic)` for any other
path expression (including literals with control characters). Parentheses,
`as` and `satisfies` around the path literal are transparent. Example decoded
name: `routes.ROUTE editor.ROUTE ""`. Repeated qualified names in a file
append `@2`, `@3` in object start order, after semantic naming (children use
their parent's unsuffixed name). Numbering counts every object literal element
of an array literal in the file's cached trees (declaration constants, router
arrays and router call first arguments), reached or not, so a name depends
only on the declaring file's content, never on which objects other files
reach (ADR 0036 Revision 1). The display name is the last segment with its
suffix. Spans and hashes cover the object literal; mount prefixes and evaluated paths never
participate in identity.

react-router routes (T026, `docs/lang/typescript-bridges.md`)
use the same form, `ui_route:<file>#<owner>.ROUTE <p>`, for object literals
and `<Route>` elements: literal `children` objects and nested `<Route>`
elements nest under their parent; `<p>` is the literal `path` (`""` when
empty), `(index)` for an index route without a path, `(no path)`, or
`(dynamic)`. `@N` numbers repeats among the file's route candidates (object
literals in router positions and module-level route consts, and `<Route>`
elements, reached or not) in source order. Spans and hashes cover the
object literal or the JSX element.

## Go

Package-level functions use their name: `function:main.go#Run`. Methods include
the receiver: `method:user.go#User.Save`. Pointer and generic receiver wrappers
are removed: `*User` becomes `User`, `*List[T]` becomes `List`.
Lexical containment stays file → method; types do not own separately declared
methods. Struct/interface/named-type/alias kinds are `struct`, `interface`,
`type`, `type_alias`; exported package values use `variable` or `constant`.
Example: `struct:list.go#List`, `type_alias:user.go#Alias`.

Several `init` declarations are legal. Within each file in source declaration
order, the first is `init`, then `init@2`, `init@3`, and so on. The emitted second
identity is `function:main.go#init%402`. Counts restart per file. Inserting or
reordering init declarations may change their identity; moving preceding lines
without changing declaration order does not.

Blank functions `func _()` follow the same rule (T020 S1, ADR 0068): the first
in a file is `_`, then `_@2`, `_@3` in source order (`function:gen.go#_%402`).
Any other repeated kind and qualified name in one file (a redeclaration, invalid
Go) has no identity: every such declaration goes to the default branch of
`docs/lang/go-structure.md` (row GS-D6) instead of keeping the last record.
Methods use only the receiver forms `T`, `*T`, `T[...]`, `*T[...]`; any other
receiver form emits no node (row GS-D4).

## Python

Python HTTP route identities follow the route rule above: uppercase method plus
the unprefixed literal registration pattern, with declaration-order @2/@3
suffixes. Prefixes are excluded; decorator hashes cover the call only (ADR 0015).

Functions, async functions and classes use lexical owner names:
`function:jobs.py#run.validate`, `class:jobs.py#run.Local`.
Immediate functions in a class use `method:user.py#User.save`; nested functions
in a method remain functions (`User.save.validate`). Module assignments to
UPPER_CASE names use `constant`. Decorated definitions include decorators in
their spans and store dotted decorator names without arguments.

`@typing.overload` and `@overload` signatures append `@overload` and parameter
text only: decoded `convert@overload(value:str="a b")`. Whitespace and comments
outside literals are removed; whitespace/comments inside literals are retained.
The implementation keeps `convert`. Parameter renames change overload identity,
as for TypeScript. Python return annotations do not participate.

Redefinitions of the same name in the same lexical scope retain the first plain
name; later declarations use `name@2`, `name@3`, in source order. This includes
definitions inside conditional branches, which do not introduce a lexical scope.
Distinct overload parameter signatures have separate names. Repeated identical
overload signatures receive the same positional suffix rule. Counts restart per
file and scope. The emitted second conditional definition is
`function:jobs.py#conditional%402`.

Go repeated init and Python repeated definitions are order-based
discriminators. They are declaration-order exceptions to the
semantic naming policy, never line numbers or body hashes. Their limitations
are explicit: insertion/reordering can change which body receives a name.

## Rust

Rust HTTP routes follow the route identity rule above: uppercase method plus the
unprefixed literal registration pattern, with declaration-order @2/@3 suffixes.
Nest prefixes are excluded; each Axum verb hashes its own call (ADR 0016).

File modules keep identities scoped to their own file. Inline modules add their
name to lexical owners: `function:src/lib.rs#jobs.run`. Functions, structs,
enums, unions, traits, type aliases and macros use their declared names;
public consts/statics use `constant`/`variable`. `macro_rules! log` uses
`macro:src/lib.rs#log`; macro invocations never produce calls.

A Rust `use` of an external crate points to `external:.#cargo:<crate>`, the
crate named by its package (`-` as `_`): a renamed dependency `ser = {
package = "serde" }` gives `cargo:serde`, so one node stands for one package
name whatever each importer calls it (`docs/lang/rust-structure.md`, RS-I3).

Inherent methods include self type: `method:src/user.rs#User.save`. Trait impl
methods append the trait path's last segment with generic arguments removed:
decoded `User.save@Display`, emitted `method:src/user.rs#User.save%40Display`.
Trait default methods use `Display.fmt`. Methods in impl blocks are lexically
contained by the file/inline module owner; trait defaults by the trait.

An impl's associated consts and types are members named as its methods:
`constant:src/user.rs#User.MAX`, `type_alias:src/user.rs#User.Item%40Iterator`
(decoded `User.Item@Iterator`), contained by the file/inline module owner.

Items in a function body (at any depth of blocks, closures and match arms,
not in a block's `mod` or a `const`/`static` initializer) use lexical owner
names as in Python and TypeScript: the enclosing function's or method's
qualified name without its `@n`, a dot, then the name the item would have at
module level: `function:src/lib.rs#run.helper`, `struct:src/lib.rs#run.Local`,
`function:src/user.rs#User.save.validate`, `method:src/lib.rs#run.Local.new`
(an impl in the body), `method:src/lib.rs#run.Tr.req` (a trait in the body).
The enclosing function contains them (a body trait's methods: that trait);
they carry the attribute `local: true` and are no module items (they bind no
name for calls or imports).

Named self types drop references, lifetimes and generic arguments:
`Foo<T>`, `crate::Foo<T>` and `&'a Foo` become `Foo`. Other self types
(tuples, slices, `dyn`, `Box<Foo>`, `Rc<Foo>`, `Arc<Foo>`, `Pin<Foo>`,
`Cow<Foo>`) retain their type text with whitespace removed, except that one
space stays where two words (letters, digits, `_`, non-ASCII) would
otherwise run together: `impl dyn m::Tr` gives `dyn m::Tr.f`, `*const T`
stays `*const T`, `Box<dyn Error + Send>` becomes `Box<dyn Error+Send>`. Recognized wrapper
names keep their complete text; this is syntactic normalization without type
checking and does not distinguish a user-defined type with a wrapper's name.

Repeated identical qualified names within a file/scope (including cfg-gated
alternatives) keep the first plain name, then `@2`, `@3`, in declaration order.
Numbering runs in two passes over a file: first the module items, then the
impl associated consts and types and the items in function bodies, each
taking the next number of its qualified name. So `mod run { fn helper() }`
keeps `run.helper` and a `fn helper` inside `fn run` becomes `run.helper@2`;
two body items of one name in one function are `run.helper`, `run.helper@2`;
and in cfg alternatives `plat`, `plat@2` each holding `fn h`, the bodies'
items are `plat.h` (contained by `plat`) and `plat.h@2` (by `plat@2`).
Body items and associated items never change a module item's identity.
The same insertion/reordering limitations as Python apply. TypeScript/TSX/
JavaScript repeated identities after semantic naming, Rust repeated names,
Go init and Python redefinitions are the documented order-based exceptions;
insertion/reordering can change which body receives each name. These use neither
line numbers nor body hashes. Cfg conditions and build constraints are indexed
without evaluation.

### Tauri commands

`ipc_command:<file>#<owner>.COMMAND <segment>` (level 4; ADR 0038) names one
`generate_handler!` entry: `<owner>` is the handler's owner function or
method qualified name (none at file level), `<segment>` the last segment of
the entry path as written (`commands::greet` gives `COMMAND greet`, a raw
`r#type` stays `r#type`). Renames, the resolved command function and the
registry proof never enter identity. Repeated names in a file get `@2`, `@3`
in declaration order (handlers in source order, entries in order), so a
duplicate entry is `main.COMMAND greet@2`. Span and hash cover the entry path
only, so entry attributes and other entries do not change them. Example:
`ipc_command:crates/kftray-tauri/src/main.rs#main.COMMAND%20get_configs_cmd`.

### Server actions

`server_action:<file>#<owner>.ACTION <name>` (level 4; ADR 0077, T024b)
names one action of a `'use server'` module (no owner; `<name>` is the
export name, `default` for the default export) or one inline `'use server'`
function (`<owner>` is the enclosing emitted owner's qualified name,
`<name>` the bound name, `(anonymous)` for an anonymous function).
`action_call:<file>#<owner>.ACTION <identifier>` names one linked use:
`<owner>` is the nearest emitted owner of the attribute or call,
`<identifier>` the name as written at the use. Repeated names in a file get
`@2`, `@3` in source order. Span and hash cover the export statement, the
inline function, the JSX attribute or the call. Example:
`server_action:app/(login)/actions.ts#ACTION%20signIn`.

## Java

Best-effort tier (T021, ADR 0067; rows in `docs/lang/best-effort.md`).
Types and callables use lexical owner names; Java packages are not owners:
`class:src/main/java/com/acme/Order.java#Order`,
`class:...#Order.Line` (member type), `method:...#Order.total`. Kinds are
`class`, `interface`, `enum`, `record`, `annotation` (`@interface`),
`method` (methods and annotation elements) and `constructor`. Constructors
(and compact canonical constructors) are named after their type:
`constructor:...#Order.Order`. Local classes are owned by their method or
constructor (`Order.total.Local`); lambdas, initializers, field
initializers, anonymous class bodies and enum-constant bodies add no
segment, and declarations inside anonymous class and enum-constant bodies
are not emitted. Methods of a Java 25 compact source file have no owner
(`method:tools/Hello.java#main`). Spans cover modifiers and annotations,
not the Javadoc.

When two or more methods (or constructors) of one owner share a name, each
appends `@overload(<parameter types>)`: types as written with whitespace,
comments and annotations removed, C-style array dimensions appended, `...`
kept, names, modifiers and a receiver parameter dropped. Decoded
`Strings.join@overload(String,int)`, `Sample.Sample@overload(String[],int...)`;
emitted `method:Strings.java#Strings.join%40overload%28String%2Cint%29`. A
compact canonical constructor takes the record header's types. A method
without a same-named sibling keeps its plain name, so adding an overload
renames the existing one (T021 open point O3, option (a)).
Remaining repeated identities in a file (identical type lists, two local
classes `Local` in one method) keep the first plain and append `@2`, `@3`
in source order per kind, the order-based exception described for Python
and Rust.

## C#

Best-effort tier (T021, ADR 0067; rows in `docs/lang/best-effort.md`).
Types, callables and properties use lexical owner names; namespaces (block
and file-scoped) are not owners: `class:src/Shop/Order.cs#Order`,
`struct:...#Order.Line`, `method:...#Order.Run`,
`function:...#Order.Run.Local` (local function). Kinds are `class`,
`struct`, `interface`, `record` (attribute `struct: true` for `record
struct`), `enum`, `method` (methods, destructors `~Order`, operators
`operator +`, conversions `implicit operator int`), `constructor` (named
after the type), `function` (local functions) and `property` (properties
and indexers, named `this`). Each part of a partial type is its own node in
its own file with attribute `partial: true`. Local functions of top-level
statements have no owner (`function:src/Tool/Program.cs#Add`); lambdas,
anonymous methods and field initializers add no segment. Spans cover
attribute lists and modifiers, not `///` comments.

Discriminators follow the declared name inside its segment: an explicit
interface member appends `@` and the interface's last name segment
(`Order.CompareTo@IComparable`, `Order.this@ILookup`), the static
constructor is `Order.Order@static`, and when type declarations of one
owner share a name but differ in generic arity, each generic one carries
the CLR arity suffix (`Result`, `` Result`1 ``, `` Result`2 ``; members
`` Result`1.Value ``). Overload sets of methods, constructors, operators,
conversions and indexers then append `@overload(<parameter types>)`:
modifiers (`ref`, `out`, `in`, `params`, `this`, `scoped`) and types as
written, types without whitespace, comments or attributes, a space after
each modifier, names dropped: decoded
`Order.Order@overload(int,ref string,params int[])`,
`Order.operator -@overload(Order,Order)`,
`Order.this@overload(string,params object[])`. A member without a same-named
sibling keeps its plain name (T021 open point O3 (a)). Remaining repeated
identities in a file (`#if` alternatives with identical type lists, the
same type in two namespaces of one file) keep the first plain and append
`@2`, `@3` in source order per kind: `Order.Platform@overload(int)@2`,
`` Result`1@2 ``.

A using never names a file. A using of something the repository may declare
claims nothing; any other using at compilation-unit level (or written with
`global::`) is the external node `external:.#dotnet:<name>` with the
target as written, `global::` and whitespace removed
(`dotnet:System.Text.Json.JsonSerializer` for an alias), unless a refused
or failed C# file may declare it (BE-8).

## Dart

Best-effort tier (T021, ADR 0067; rows in `docs/lang/best-effort.md`).
Types and callables use lexical owner names; libraries and parts are not
owners, so a declaration in a `part` file is named in that file:
`class:lib/src/shapes.dart#Shape`, `method:...#Shape.paint`,
`function:...#Shape.paint.stroke` (local function),
`function:lib/main.dart#main`. Kinds are `class` (attribute `mixin: true`
for `mixin class`), `mixin`, `extension`, `extension_type`, `enum`,
`function` (top-level functions, getters and setters, local functions),
`method` (methods, getters, setters and operators in a type) and
`constructor`. Lambdas and field initializers add no segment; a local
function in a field initializer's lambda is owned by the type.

- **Constructors.** The unnamed constructor is `new`, Dart's own name for
  it: `constructor:...#Shape.new` for `Shape()`, `factory Shape()`, Dart
  3.13's `new(...)` and `factory()`. Named constructors use their name:
  `Shape.named` for `Shape.named()`, `factory Shape.named()`, `new named()`
  and `factory named()`.
- **Accessors.** A getter keeps its name and a setter adds `=`, so a pair
  never collides: `method:...#Shape.area` (attribute `accessor: "get"`) and
  `method:...#Shape.area%3D` (decoded `Shape.area=`, `accessor: "set"`).
- **Operators.** `operator <op>` with the operator as written without
  whitespace: `Shape.operator ==`, `Shape.operator []=`.
- **Anonymous extensions.** `extension on <on-type>`: the on-type as
  written, comments removed and whitespace removed except one space
  between two word characters: `extension on List<int>`,
  `extension on Map<String,T>?`, `extension on void Function()`; members
  `extension on List<int>.total`.
- **Repeated names.** Dart has no overloading. Repeated identities in a
  file (unary and binary `operator -`, two anonymous extensions on one
  type, two local functions of one name) keep the first plain and append
  `@2`, `@3` in source order per kind.

Spans start at the first of the annotations directly before a callable
(comments between them included; a top-level `external` too) and end at
the end of its body or its `;`; doc comments are outside. Type spans are
the declaration node, which holds its annotations.

A directive never names a symbol. A relative URI or a URI of the file's own
package names a file (`imports` edge); another package of the repository
names nothing; `dart:<lib>` and other packages are external nodes
`external:.#dart:<lib>` and `external:.#pub:<name>`.

## Client calls

client_call is a level-4 source node with the call-expression span/hash.
Its decoded qualified name is <owner>.<METHOD> <url_expression>, omitting the
owner at file level and using DYNAMIC for an unknown method. URL source text
retains literals and removes external whitespace/comments. The nearest emitted
named lexical owner contains it, otherwise the file. Wrapper-derived calls
use the wrapper invocation span and its URL argument expression; inner sites
are marked wrapper. Repeated identities in a file get declaration-order @2/@3.
Resolved bases, configured bases and normalized templates never enter identity.
Contract escaping applies; see ADR 0018.

## Tauri invoke calls

ipc_call is a level-4 source node with the call-expression span and hash (a
wrapper invocation's own span; an await-attached call starts at its callee).
Its decoded qualified name is `<owner>.INVOKE <command argument>`, omitting the
owner at file level, where the argument is the compact source text of the
command argument (the first argument, or the wrapper's command parameter
position for a wrapper invocation; `(none)` when missing), with literals kept
and external whitespace and comments removed, as client_call URL expressions.
The owner is the nearest emitted lexical owner. Evaluated names, wrapper
identities and match results never enter identity. Repeated names in a file
get `@2`, `@3` in source order, so kftray's three
`reject('start_port_forward_tcp_cmd')` calls in an anonymous test callback are
`INVOKE 'start_port_forward_tcp_cmd'`, `@2` and `@3` (ADR 0038).
