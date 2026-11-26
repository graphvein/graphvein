# Python model syntax (SQLModel and SQLAlchemy)

ADR 0053; task T018c1 (capture) and T018c2 (model linking); payload version
2 (query sites and parameter annotations, gate GQ) from ADR 0060, task T018h.
Aggregate extractor version: extractors-49 (29 from T018c1: cached Python
facts gained the `Models` payload; 30 from T018f: provenance attributes on
schema records; 31 from T018g0: model group key, column claims and
verification; 32 from T018g2: the cached TypeScript model payload; 33 and 34
from T018h: payload version 2; 35 from T018g3: TypeScript model payload
version 2; 36 from T018g5: the Go model payload; 37 from T018g6: TypeScript model payload version 5). Contract version unchanged; T018c1 adds
no graph record.

The Python extractor captures the syntax that model linking needs to emit
model-defined `table` and `column` nodes: reduced class headers, the bodies
of SQLModel classes, Core `Table(...)` lines and the sites that mutate a
model table after its definition. The payload is syntax, never a claim. Which
base is `sqlmodel.SQLModel`, which callee is `sqlmodel.Field` and whether a
table exists is decided at link time through the Python resolver
([python-resolution.md](python-resolution.md)), under the claim contract
below. Anything the contract does not list is unknown (row D), never a
guess, and a repository whose model forms are not read is shown as "models
present, not read", never as "no models".

## Payload

`facts.PythonFacts.Models` (JSON `Models`, omitted when absent), type
`facts.PythonModels`, version 2 (`facts.PythonModelsVersion`; version 1
entries are a cache miss once).

- **G1 file gate.** Only a file with an absolute `import`/`from ... import`
  whose module's first dotted segment is `sqlmodel` or `sqlalchemy` gets a
  payload (relative imports never pass). **(ADR 0060)** A file that fails G1
  but passes gate GQ (see "Query sites") gets a payload with `Queries` and
  `Annotations` only; G2-G5 apply to G1 files alone, so model linking reads
  exactly the files it read before. Every other Python file has no
  `Models` field, so its cached facts are unchanged.
- **G2 class headers.** In a G1 file, every class with at least one base,
  keyword or decorator: name, identity, enclosing scope index, span,
  `Conditional` (inside `if`/`try`/`for`/`while`/`with`/`match` or a
  function), bases and decorators as dotted names, keywords with literal
  values (anything else is `other`).
- **G3 members.** `MembersCaptured` and `Members` only for a class that has
  a `table` keyword, has a base whose last segment is `SQLModel`, has a
  base whose last segment names such a same-file class (a subclass of a
  model mixin, T018c1 Revision 3), or is named (last segment) as a base by
  such a same-file class (fixpoint). Docstrings,
  `pass` and `...` are dropped (M0); private members keep kind and name
  (M7); methods keep name and decorator names (M1); a nested `class Config`
  keeps its `key = value` members (M9); `Relationship(...)` keeps the callee
  only (M5); every other statement keeps its kind and line (M12), so an
  unread member is visible, and an unannotated assignment keeps its value
  when it is a literal (a proven import failure, T018c2's L7).
- **G4 Core tables.** The lines of calls whose callee's last segment is
  `Table` or `map_imperatively` (U3, U4). This is a spelling test: the
  census found one such line in langflow that is `rich.table.Table`;
  resolution drops it at link time.
- **G5 sites.** Mutation sites (X1): `append_column`, `append_constraint`,
  `append_foreign_key`, `set_add` (`.indexes.add`, `.constraints.add`,
  `.foreign_keys.add`), `attribute_assign` (a target chain with
  `__table__`, `.c.` or `.columns.`), `constraint_call` (an `Index`,
  `UniqueConstraint`, `CheckConstraint`, `ForeignKeyConstraint` or
  `PrimaryKeyConstraint` call outside `__table_args__` and outside any call's
  arguments, with a positional argument that is not a str literal); DDL hooks
  (U5): `event.listen`/`listens_for` with a `before_create`, `after_create`,
  `before_drop` or `after_drop` event. Each site keeps its scope index for
  resolving local receivers. **(T018c2 Rev 3.1)** Append and set-add sites
  also keep their arguments reduced one level (`Args`: a call keeps its
  callee, the constraint kind; `Keywords`: a literal value, for
  `replace_existing`).
- **Reduced expressions.** Literals are decoded (`LitKind` str, int, float,
  bool, none, ellipsis, bytes); calls keep the dotted callee, every keyword
  name and positional arguments to a depth of 4. `Field(...)` keeps values
  only for `primary_key`, `nullable`, `index`, `unique`, `default`,
  `default_factory`, `foreign_key`, `ondelete`, `sa_type`, `sa_column`,
  `max_length`, `max_digits` and `decimal_places`; `Column(...)` only for
  `name`, the flags, `default`, `server_default`, `onupdate` and
  `server_onupdate`; `Index(...)` only for `unique`; other keyword values
  are `other` (names only). Annotations are reduced to a depth of 8 and keep
  their compacted text; values keep text only for type expressions
  (`sa_type=`, a `Column`'s type position). Function bodies and description
  values are never captured.
- **Validity.** `PythonModels.Valid()` and `PythonFacts.ModelsValid()`
  (scope indexes in range) are checked on every cache read
  (`internal/index/index.go`); another version or a damaged payload is a
  cache miss and the file is re-extracted. A failure to capture one class
  drops that class's record with a `python_model_capture_failed` diagnostic
  and keeps the file's other facts.

## Query sites (payload version 2, ADR 0060)

Code-to-table linking (T018h, `docs/contract/claims/table-access.md`, rows
CT-P1..CT-P5) reads two more fields. They are syntax filtered at capture by
the cheapest test that keeps every site a row can claim or refuse; every
decision (is `select` sqlalchemy's, is the receiver a session, is the
argument a model class) is made at link time through the resolver.

- **Gate G1 ∪ GQ.** GQ: a file with a session-method call (`get`, `get_one`,
  `add`, `add_all`, `delete`, `merge`, `refresh`) that passes the capture
  filter below and whose receiver `<r>` is a dotted name whose last segment
  contains `session` (case-insensitive: `db_session.add(x)`,
  `self.session.refresh(o)`), or whose first segment is a parameter of an
  enclosing function annotated with a type whose text contains `session`
  (`db: DbSession`, `db: AsyncSession`). It admits session calls in files
  that import neither package (T018h). The gate is a capture
  filter only. **(H-B1 widening)** The task's GQ named five methods and the
  receiver spelling only; on langflow that missed 3 inventory sites
  (`async_session.add_all`, two `db.add`/`db.get` with `db: DbSession`), so
  the method list is the full session list and the annotation test was
  added. One inventory site stays outside (`db: Any`, langflow
  `api/v1/memories.py:109`).
- **`Queries`** (`[]facts.PythonModelQuery`, source order). A call chain is a
  call whose callee is an attribute of another call, down to its first link
  (`select(M).join(N).where(c)` has the links `where`, `join`, `select`). The
  outermost call of a chain is captured when one of its links passes its
  name's filter:
  - `select`, `insert`, `update`, `delete`, `text`: the link's callee is a
    bare name, or a dotted name whose first segment an import of the file
    binds (`sa.select`, `sqlalchemy.text`), or a bare name that an import
    binds to one of these names (`from sqlalchemy.dialects.postgresql import
    insert as pg_insert`); `d.update(x)` on a local is not captured.
  - `get`, `get_one`: a method call (`<r>.get`) whose first positional
    argument is a dotted name (the model class); `d.get("key")` is not.
  - `add`, `add_all`, `delete`, `merge`, `refresh`: a method call whose first
    positional argument is not a literal (`router.delete("/items")` is not
    captured). The argument's form is not filtered further: a site whose
    object class is unproven is an unknown record, so it must be seen.
  Calls in a captured chain's arguments are examined as chains of their own
  (`insert(M).from_select(cols, select(N))` gives two sites; an executed
  statement `session.exec(select(M))` gives the inner `select` only). Each
  site keeps `Span` (the outermost call; `Span.StartLine` is its line),
  `Scope` (the enclosing `Scopes` index), `Owner` (the innermost emitted
  function, method or class; for a module-level constant's initialiser that
  constant node; else the file node) and `Expression`: the outermost link
  as a reduced call whose `Object` holds the link it is chained on when the
  receiver is not a dotted name. Arguments are reduced to a depth of 4
  (names, decoded string literals, including implicit concatenation; calls
  with their callee and positional arguments; lists and tuples); keyword
  values are not read by any row and keep their names only (value `other`);
  an f-string with an interpolation, `%` or `.format` is `other`. Decorator
  calls, comments and function bodies outside a captured call are never
  captured. Local single assignments (`item = Item.model_validate(...)`) are
  read from the existing `Scopes[].Bindings` (`Count`, `Stable`).
- **`Annotations`** (`[]facts.PythonModelAnnotations`, by scope index). For
  each function scope that holds a captured site, or encloses one, the
  annotated parameters whose name is the first segment of a dotted name or
  callee in such a site (a receiver `session`, an object `item`), each
  reduced like a member annotation (depth 8, no text): `session: Session`,
  `db: SessionDep`, `item: Item | None`, `Optional[Item]`, a string
  annotation as a str literal. Unannotated parameters are not listed (their
  names stay in `Scopes[].Parameters`).
- **Validity.** `Valid()` also rejects a query with no owner, a span without
  a line, an expression that is no call, an `Object` on anything but a call,
  and annotations that are empty, unsorted or hold a damaged expression;
  `ModelsValid()` checks both scope indexes.
- **Known gaps.** A statement extended after assignment (`stmt =
  stmt.join(N)`) is a chain without a listed link and is not captured (the
  contract claims only the building chain). A receiver reached through
  `await` or a call (`(await get_session()).get(M, 1)`) keeps `Object`
  `other`. SQL-shaped literals passed to unlisted calls (driver-level
  `cursor.execute("...")`, row D) are not in `Queries`; see ADR 0060,
  known gaps.

Tests: `internal/lang/python/queries_test.go` (chains, nested arguments,
keywords, annotations, comments, both gates) and
`internal/lang/facts/facts_test.go` (validity).

## Code-table correspondence

`modelPayloadRows` in `internal/lang/python/models_contract_test.go` maps
every row id of the table below to the payload fields its capture fills.
Every capture branch reports its row id to a test-only hook
(`modelTag`). `TestModelPayloadContract` checks that the row ids parsed from
this page equal the task's table (row text identical), that one snippet per
row fills exactly the listed fields, and that capturing the fixture
`testdata/fixtures/sqlmodel-models/` emits exactly the rows with a capture
side (rows whose payload cell is not `-`, `nothing`, `no new field ...` or
`... already cached ...`). `TestModelCensus` (run with
`GRAPHVEIN_MODEL_CENSUS_CORPUS` set to the corpus directory) checks the
capture against the investigation's census.

## Environment (part of the contract)

- **V1 Library versions.** SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic
  2.13.4 (pydantic-core 2.46.4). Both corpora lock exactly these
  (`fastapi/uv.lock`, `langflow/uv.lock`). Every source citation below is
  to these versions' files (`sqlmodel/main.py`, `sqlmodel/_compat.py`,
  `sqlalchemy/orm/decl_base.py`).
- **V2 Python.** 3.14 (fastapi requires `>=3.14`; langflow allows
  3.10-3.14). Column **order** depends on class-namespace and annotation
  handling and is not claimed.
- **V3 Import succeeds.** **(Rev 2)** The Rev 1 sentences on I1 and I2 are
  replaced by assumption V7 (Revision 2); the rest stands. A table claim means: importing the module
  defines this table in `SQLModel.metadata` with these facts. **(Rev 1)**
  Every form the contract knows may make the import fail is row I1, and I2
  withdraws every claim in the import closure of such a form (claim only
  where the import is proven not to fail through a contract form). Other
  module code (unrelated statements that could raise) is the assumption
  itself, like T018a2's E3. Code that runs at import time and mutates
  the metadata afterwards (for example Alembic's `env.py` assigning
  `SQLModel.metadata.naming_convention`, langflow `alembic/env.py:39`) is
  outside what source shows; that is why database names derived by naming
  conventions are never claimed (PF3).
- **V4 Same metadata.** Every SQLModel table class registers in the one
  default registry (`sqlmodel/main.py` 799-804) unless a `registry=`
  class keyword says otherwise (row C4, D).
- **V5 (Rev 1) Unknown names.** **(Rev 2) After v1** (it served I1; Revision 2). A table whose name is unknown (N3) is
  assumed not to collide with a claimed name (I1's duplicate-name form
  covers claimed names only); T018c2 lists every N3 table in a diagnostic.
- **V6 (Rev 1) Import-free mutations outside G1.** Mutations of a model
  table that need no import (X1's set adds and attribute assignments) in a
  file that does not pass G1 are not captured; the table is assumed
  unmodified by them.
- **V7 (Rev 2) Import succeeds (assumption).** The model modules import
  successfully, as they do when the application runs (Revision 2, with
  rule P1 for failures the source proves).

## Claim contract

Row kinds:

- **C** class header; **N** table name; **M** class-body member; **A**
  annotation reading; **F** `Field(...)` arguments; **K** `Column(...)`
  arguments (inside `sa_column=`); **T** explicit type expressions; **TA**
  `__table_args__` elements; **X** post-definition mutation sites (they
  take completeness away, never add a fact);
- **U** recognized but unsupported: T018c1 captures a marker, T018c2
  claims nothing and emits a diagnostic, so no view shows an absence;
- **D** the single default branch.

"Payload" names the fields of `facts.PythonModels` (section Payload) that
the row needs. "Claim (T018c2)" is the widest fact the row may produce.
"Refusal" lists the conditions under which the row's claims are unknown
(the table or column becomes partial with the row id as the reason) or,
where stated, the whole class claims nothing. "Resolution" means T018c2's
Python resolver (ADR 0015 project roots, ADR 0017 implicit roots), never a
spelling match: a base or callee counts as `sqlmodel.SQLModel`,
`sqlmodel.Field`, `sqlalchemy.Column`, ... only when the import binding
resolves to that module and name, including SQLModel's re-exports of
SQLAlchemy names (a generated table from `sqlmodel/__init__.py` 0.0.39:
`from sqlmodel import Column` is `sqlalchemy.Column`).

| id | form | payload (T018c1) | claim (T018c2) | refusal / unknown |
|---|---|---|---|---|
| C1 | `class N(<bases>, table=True)`: the `table` keyword is the literal `True` | class record: name, span, scope, conditional flag, bases (reduced expressions), keywords (name + reduced value), decorators; members (M rows) | a model table exists; columns from the members of N and its chain (C2) | **(Rev 1)** every direct base of N proven non-table (R-B2 below), otherwise no table claim (diagnostic "model table not proven"); conditional definition (inside `if`/`try`/`for`/`while`/`with`/`match` or a function) or a name bound more than once in its scope: D (no claim, diagnostic); a decorator: D; a keyword other than `table`: C4; not exactly one base: C6; the chain (C2) not proven: table name may still be claimed (N rows), columns partial; an I1 form in N's import closure: I2 (no claim) |
| C1b | `table=False` literal, or no `table` keyword | as C1 | no table; the class is a C2 candidate | - |
| C1c | `table=<non-literal>` | as C1 | none; diagnostic "model table flag unknown" (the class may be a table) | always (present but unknown) |
| C2 | the chain: each base of N, followed upward, is a resolved repository class with exactly one base and no `table` keyword, ending at `sqlmodel.SQLModel` | class records of every class in the chain | the members of every chain class contribute columns (pydantic field inheritance); **(Rev 1)** for every member name, `__tablename__` and `__table_args__`, the definition nearest to N wins wholesale: nothing is merged across the chain (a redefined field takes none of the ancestor's Field keywords; an M5 `Relationship` removes the inherited field) | a base that does not resolve, resolves outside the repository (other than `sqlmodel.SQLModel`), or resolves to a class without a captured record: chain unknown, every column partial ("unknown ancestor"); depth > 20: D |
| C3 | a direct base (or a chain base) that is a table class (`table=True`, or a subclass of one: `model_config` is inherited) | as C1 | **none for N** (SQLModel maps a table class only when no direct base is a table model: `main.py` 634-635, `base_is_table`, checked over every direct base); diagnostic | always |
| C4 | other class keywords (`registry=`, `metaclass=`, any config keyword) on N or a chain class | keyword names and reduced values | none | always D |
| C5 | `model_config` assignment or nested `class Config` in N or a chain class | the member (M8/M9) | nothing, when every key is in the allowlist K-cfg | a `table` or `registry` key, a key outside K-cfg, a non-literal value or a non-dict/non-`ConfigDict` value: D for the class |
| C6 | more than one base, or zero bases, on N or a chain class | bases | **(Rev 1)** a table claim only when every direct base is proven non-table (R-B2); table name only from an N1 `__tablename__` in N's own body | columns partial (multiple-inheritance field order and MRO merge are not modelled); **(Rev 2, re-check)** table name, constraints and schema unknown unless every base is a proven plain mixin whose contributions are read under the same rows; zero bases: not a SQLModel class |
| N1 | `__tablename__ = "<str>"` in N or, nearest first (C2: nearest wins), in a chain class | member with literal value | table name as written | non-literal value, annotated assignment, `declared_attr` or method named `__tablename__` anywhere in the chain: N3 |
| N2 | no `__tablename__` member anywhere in N's chain, the chain proven (C2), N an ASCII identifier | class names and members | table name = `N.lower()` (`main.py` 867-869, SQLModel's `declared_attr`) | chain not proven, non-ASCII class name, or any unread member in the chain (it might be `__tablename__`): N3 |
| N3 | table name unknown | - | none: no table node; diagnostic "model table name unknown" with the class | always |
| M0 | docstring, `pass`, `...` | nothing | nothing | - |
| M1 | method (`def`/`async def`), undecorated or with decorators all in the allowlist K-dec | name, decorator callee spellings | nothing | name in {`__tablename__`, `__table_args__`, `__table__`, `__mapper_args__`, `__init_subclass__`}, a decorator outside K-dec (including `declared_attr` in any spelling), or a name equal to a field of the chain: D for the class (columns partial) |
| M2 | `name: <ann> = Field(<args>)`, `Field` resolving to `sqlmodel.Field` | member name, line, annotation (A), value as a reduced call with all keyword names and reduced values and positional args | a column named `name` (or the K1 name) with the F/K facts, **(Rev 1)** only when the annotation carries no A1m metadata | `**kwargs`, more than one positional argument, a keyword outside F rows and K-meta: column D (name known, facts unknown, table partial); a keyword outside `sqlmodel.Field`'s signature: I1; A1m metadata: A1m |
| M3 | `name: <ann>` (no value) | name, line, annotation | a column: required, no default (`default: absent`), **(Rev 1)** unless the annotation carries A1m metadata | A1m |
| M4 | `name: <ann> = <literal>` (str, int, float, bool, `None`; not bytes) | name, annotation, literal kind and value | a column: default `absent` when the literal is `None`, else `scalar` with the literal, **(Rev 1)** unless the annotation carries A1m metadata | any other value expression (a name, attribute, call of an unresolved callee, f-string, collection): column D; A1m |
| M5 | `name: <ann> = Relationship(...)`, resolving to `sqlmodel.Relationship` | name only (arguments are not captured) | no column for `name` (removes an inherited field of that name) | - |
| M6 | `name: ClassVar[...]` (with or without value), `ClassVar` resolving to `typing.ClassVar` | name, annotation | nothing | - |
| M7 | **(Rev 1)** `_name: <ann> [= ...]` or `_name = ...` (a leading underscore, annotated or not; an unannotated assignment without the underscore is M12) | member kind and name | nothing (pydantic private attribute) | - |
| M8 | `model_config = ConfigDict(<k>=<literal>, ...)` or `= {<str>: <literal>, ...}` | keys and literal values | nothing (C5) | see C5 |
| M9 | nested `class Config:` whose body is only `<key> = <literal>` | nested member keys | nothing (C5) | see C5 |
| M10 | `__mapper_args__ = {<str>: <literal>}` with keys in K-mapper | keys | nothing | another key or value form: D for the class |
| M11 | `__table_args__ = (<elements>)` tuple literal | elements (TA rows) | table-level constraints (TA) | dict literal, a non-literal, a trailing options dict, an annotated assignment: table constraints unknown and table schema unknown (partial) |
| M12 | any other statement in a class body: unannotated assignment (pydantic rejects it or it is not a field), `if`/`try`/`for`/`with` blocks, other nested classes, annotated assignment to a dunder, unpacking targets | member kind and line only; **(Rev 3)** an unannotated assignment's value when it is a literal | none | always D: the class's column set is partial; if the member binds a known name, that column is unknown; **(Rev 3)** a literal value is P1 under T018c2's L7 conditions |
| A1 | annotation reading for nullability (`_compat.py` 157-171: `is_field_noneable`) **(Rev 1)**: a union of any arity that has a `None` member (`X \| None`, `None \| X \| Y`, `Optional[X]`, `Union[..., None]`), at the top level or inside an outermost `Annotated[...]`, after A5 | reduced annotation: name/attribute, subscript base and arguments, `\|` union operands | optional = true | the type mapping (F5) of a union with more than one non-`None` member fails: without `sa_type`/`sa_column` that is I1; with them the column is fine and nullable (reviewer case `satype_union`) |
| A2 | a single type that resolves to a class: a builtin, a pinned external class (F5 list), a repository class, or a generic (`list[...]`, `dict`, `Literal[...]`) | as A1 | optional = false | a generic, `Literal`, a builtin outside F5 (`dict`, `list`, ...) or a repository class that is not an enum: the F5 mapping fails or is unproven, so without `sa_type`/`sa_column` that is I1 |
| A3 | a name that does not resolve, resolves to an import outside the repository other than the F5 classes, or (**(Rev 1)**, B5) resolves to a non-class binding that A5 does not read (a `NewType(...)` call, `type X = ...`, a conditional or repeated binding, a call); a string annotation | as A1 | optional unknown, type unknown | without `sa_type`/`sa_column`: I1 (the mapping may fail) |
| A5 | **(Rev 1)** an alias: a name that resolves (through the resolver, following re-export bindings such as `from lfx.schema.serialize import UUIDstr`) to one unconditional module-level assignment (`X = ...` or `X: TypeAlias = ...`) whose value is a type expression (a name, attribute, subscript or `\|` union; not a call) | the module bindings already cached (`PythonBinding.Value`, imports) | the annotation is read as that expression, in its module, by A1-A3/A1m (depth 5) | anything else bound to the name: A3; **(Rev 2, re-check)** a binding that does not precede the class statement using the alias: A3 |
| A1m | **(Rev 1)** `Annotated[T, m1, ...]` metadata (outermost, directly or through A5): an element is *plain* when it is a constant or a call of `pydantic` `AfterValidator`/`BeforeValidator`/`PlainValidator`/`WrapValidator` (directly or through a name bound to such a call); any other element (a `sqlmodel.Field` call, which SQLModel reads first: `main.py` 217-232; `pydantic.Field`, `annotated_types` constraints such as `MaxLen`, `StringConstraints`; an unresolved or unread element), or a non-plain element inside a nested `Annotated` | reduced annotation including metadata calls | with only plain elements: nothing (T is read by A1-A3) | any non-plain element: the column is D (every F/K fact, type, length, digits and default unknown; table partial); for I1, the type mapping then needs `sa_type`/`sa_column` from the metadata `Field` when there is one, and an unproven element makes it unproven |
| F1 | `Field(primary_key=, nullable=, index=, unique=)` with `True`/`False` literals | reduced keyword values | the flag; absent: `primary_key=false`, `index=false`, `unique=false` (`main.py` 746-751, 763-765) | a non-literal value: that flag unknown (and nullable unknown if primary_key is unknown) |
| F2 | nullability of a Field column (`main.py` 752-758) | - | `nullable=<F1 literal>` if given; else `false` when `primary_key=True`; else A1/A2's optional | A3 and no `nullable=`/`primary_key=True`: unknown |
| F3 | `default=<literal>` or one positional literal argument; `default=None`; `default=...` | reduced literal | `None` and `...`: default `absent` (`Column(default=None)` has no default; `...` is pydantic's required marker); other literal: `scalar` | a non-literal value (name, attribute, call): default unknown (present but unknown) |
| F3b | `default_factory=<lambda>` / a name resolving to a function defined in the repository or an imported callable | reduced expression | default `callable` | literal `None`: absent; an unresolved name or another expression: unknown |
| F4 | `foreign_key="<target>"` str literal, optional `ondelete="<str>"` | literals | a foreign key to the written target with `ondelete`; **(Rev 1, B4)** the target is split at its **last** dot: the part after it is the column, the part before it the table, which may itself be schema-qualified (`"auth.s.id"`: schema `auth`, table `s`, column `id`); **(Rev 2, re-check)** with two or more dots the table part is itself split at its last dot (`"a.b.c.id"`: schema `a.b`, table `c`); T018c2 resolves the table part to a table node (with that schema) and leaves unresolved targets unresolved | non-literal: FK unknown; a target without a dot, or with a quote character: FK unknown; `ondelete="SET NULL"` with nullable not proven true: I1 (SQLModel raises, `main.py` 770-771); `ondelete` without `foreign_key`: no effect |
| F5 | type of a Field column without `sa_type`/`sa_column`: A1/A2's inner type mapped by `get_sqlalchemy_type` (`main.py` 686-737), in its order | annotation | **(Rev 1)** `Enum` for a repository class with a base (directly or through repository classes) resolving to `enum.Enum`, `StrEnum`, `IntEnum`, `Flag` or `IntFlag` (so `class C(str, Enum)` is `Enum`: the enum check comes first); `AutoString` for `str`, `pydantic.EmailStr`, `pathlib.Path`, `ipaddress.IPv4Address`, `ipaddress.IPv4Network`, `ipaddress.IPv6Address`, `ipaddress.IPv6Network`, with `length` = a positive `max_length` int literal when the Field has only keywords in K-len (`max_length=0` or absent: no length, `main.py` 711: `if max_length:`); `Float`, `Boolean` (checked before int), `Integer`, `DateTime`, `Date`, `Interval`, `Time`, `LargeBinary` (bytes), `Numeric` (`decimal.Decimal`; precision/scale from `max_digits`/`decimal_places` literals, absent: none), `Uuid` (`uuid.UUID`) | A1m metadata: column D; any other type (generic, `Literal`, other builtin, non-enum repository class, A3): the mapping fails or is unproven, I1; a non-literal `max_length` or a keyword outside K-len: length unknown, class still claimed |
| F6 | `sa_type=<type expression>` | reduced expression | T1's type | T1 refusals |
| F7 | `sa_column=Column(...)`, `Column` resolving to `sqlalchemy.Column` (directly or through SQLModel's re-export) | reduced call | the column is exactly the K rows' column: **Field `default`/`default_factory`, `max_length` and the annotation do not reach it** (`main.py` 742-744 returns the Column unchanged); nullable from K2 | `sa_column=` any other expression (a helper call such as langflow's `_tz_column()`, a name): column D |
| F7r | `sa_column=` together with any of `primary_key`, `nullable`, `foreign_key`, `ondelete`, `unique`, `index`, `sa_type`, `sa_column_args`, `sa_column_kwargs` (in the assigned `Field` or in an A1m metadata `Field`) | keyword names | none: SQLModel raises at import (`main.py` 116-153) | **(Rev 1)** I1 |
| F8 | `sa_column_args=`, `sa_column_kwargs=` | keyword names | none | always: column D (0 corpus uses; coverage backlog) |
| F9 | K-meta keywords **(Rev 3: `examples` and `json_schema_extra` removed; they are outside the 0.0.39 signature)**: `description`, `title`, `exclude`, `include`, `repr`, `alias`, `schema_extra`, `ge`, `gt`, `le`, `lt`, `min_length`, `max_length`, `max_digits`, `decimal_places` | keyword names (values only for `max_length`, `max_digits`, `decimal_places`) | no column fact (except F5 lengths) | a keyword of `sqlmodel.Field`'s signature in neither F rows nor K-meta (for example `regex`, `discriminator`, `const`): column D; **(Rev 1)** a keyword outside the signature (for example `pattern`): `TypeError` at import, I1 |
| K1 | `Column(["<name>",] [<type>,] <schema items>..., <keywords>)`: an optional str literal first (the column name), then an optional type, then positional schema items | reduced call | column name = the str literal, else `name=` literal, else the attribute name; type per T1 | a first non-name positional that is neither a T1 type, a K1b opaque type nor a K3 item: column D (it may be a schema item); **a `ForeignKey(...)` first positional means no type argument (PF1)** |
| K1b | the type position holds a provable type the pass cannot read: a call or name resolving to a repository class whose base resolves to `sqlalchemy` `TypeDecorator`/`UserDefinedType` or a T1 class; a module-level name bound once to a T1 call chain (`JSON().with_variant(...)`); a T1 class called with unread arguments | reduced expression | type unknown; other K facts claimed | - |
| K2 | `primary_key=`, `nullable=`, `index=`, `unique=` bool literals | reduced keywords | the flags; absent: `primary_key=false`, `index=false`, `unique=false`, `nullable = not primary_key` (SQLAlchemy `Column`) | non-literal: that flag unknown |
| K3 | positional `ForeignKey("<target>"[, ondelete="<str>"])` | reduced call | a foreign key as F4 (last-dot split) | a non-literal target or another keyword: FK unknown (column partial) |
| K4 | `default=<literal \| lambda>`, `server_default=<any>`, `onupdate=`, `server_onupdate=` | keyword names, literal values, `server_default` reduced value | default (`None`: absent; literal: scalar; lambda: callable); `server_default` present (text claimed only for a str literal or `text("<literal>")`) | `default=<name or call>`: default unknown |
| K5 | `comment=`, `doc=`, `info=`, `autoincrement=` | keyword names | nothing | - |
| K6 | any other `Column` keyword or positional (Sequence, Identity, Computed, `key=`, `quote=`, `system=`, ...) | names only | none | always: column D |
| T1 | type expression resolving to a SQLAlchemy type class in T-list (`Uuid`, `String`, `Text`, `Unicode`, `UnicodeText`, `Integer`, `BigInteger`, `SmallInteger`, `Boolean`, `DateTime`, `Date`, `Time`, `Interval`, `Float`, `Numeric`, `JSON`, `LargeBinary`, `Enum`) or `sqlmodel.sql.sqltypes.AutoString`, bare or called | reduced expression | type class; `String`/`AutoString`/`Unicode` length from a literal first argument or `length=`; `DateTime` timezone from a `timezone=` literal; `Enum`: class only | another keyword or a non-literal argument (other than for `Enum`): class claimed, arguments unknown (K1b) |
| TA1 | `UniqueConstraint("<col>", ... [, name=<str literal>])` | reduced call | a unique constraint over the written columns, written name | non-literal column or name: TA-D |
| TA2 | `Index("<name>", <elements> [, unique=<bool literal>] [, <dialect>_<option>=...])` | reduced call (dialect option names only) | an index with the written name, `unique`, and its columns when every element is a str literal (otherwise elements unknown) | non-literal name, another keyword: TA-D |
| TA3 | `CheckConstraint(<sqltext>[, name=<str literal \| conv(<str literal>)>])` | reduced call | a check constraint with its written name; the SQL text only when a str literal | another keyword or more than one positional: TA-D |
| TA-D | any other element (`ForeignKeyConstraint`, `PrimaryKeyConstraint`, an options dict, a starred element, a non-call) | element kind | none | the table's constraint set and schema are unknown (partial) |
| U1 | SQLAlchemy declarative classes: a base resolving to `sqlalchemy.orm.DeclarativeBase`/`DeclarativeBaseNoMeta`/`MappedAsDataclass`, to a name bound by `declarative_base()`/`registry().generate_base()`, or a class decorated by `<registry>.mapped`/`mapped_as_dataclass`/`as_declarative` | class header record (no members) | none; diagnostic "SQLAlchemy declarative models present, not read" with the count | always (0 corpus uses; coverage backlog, needs corpus evidence first, rule 15) |
| U2 | `Mapped[...]` annotations, `mapped_column(...)`, `relationship(...)`, `declared_attr`, `__table__ = Table(...)` inside a SQLModel class | member kind | none | D for the class (0 corpus uses) |
| U3 | SQLAlchemy Core `Table("<name>", <metadata>, ...)` calls (callee resolving to `sqlalchemy.Table`) anywhere in a captured file | line of each call | none; diagnostic "tables defined in code, not read" with the count | always |
| U4 | imperative mapping (`registry.map_imperatively`) | line | as U3 | always |
| X1 | post-definition mutation sites in a captured file: a call `<receiver>.append_column(...)`, `.append_constraint(...)` or `.append_foreign_key(...)`; **(Rev 1, B6)** a call `<x>.indexes.add(...)`, `<x>.constraints.add(...)`, `<x>.foreign_keys.add(...)`; an assignment to an attribute whose target chain contains `__table__`, `.c.` or `.columns.` (`T.__table__.c.x.nullable = True`); a call of `Index`/`UniqueConstraint`/`CheckConstraint`/`ForeignKeyConstraint`/`PrimaryKeyConstraint` that is neither inside `__table_args__` nor an argument of another call (`Table(...)`, `op.create_table(...)`, `append_constraint(...)` own it) and has an argument that is not a str literal (a bound column attaches it to that column's table) | site kind, line, enclosing scope, receiver or argument expressions (reduced) | none; a receiver that resolves (through the scope's bindings, for example `table = SSOConfig.__table__`) to a model table makes that table's constraint set (append_constraint, append_foreign_key, set adds, constructor calls) or column set and column facts (append_column, attribute assignment) not complete | a receiver or argument that resolves to something other than a model table (a local bound to a Core `Table(...)` call, a string): no effect; a site in a test-scope path (ADR 0021 classifier) does not affect production tables; an unresolved receiver or argument in a production path: every model table of the repository loses the corresponding completeness (sets not complete; existing column facts stay, except after an attribute assignment, which makes the facts of the columns it may name unknown; **(Rev 3.2, T018c2 row L8)** an attribute assignment whose target is not proven to leave the table's or the column's identity unchanged refuses that table or column claim). **Gap (Rev 1):** these mutations need no import, so a file outside G1 can hold them; they are not captured and are assumption V6 |
| U5 | DDL event hooks: `event.listen(<x>, "before_create"\|"after_create"\|"before_drop"\|"after_drop", ...)` and `@event.listens_for(<x>, <those events>)` | line, scope, target expression, event name | none for the metadata facts (hooks do not change `MetaData`); a marker "DDL hooks present" on the resolved table (all tables when unresolved), so T018e2's drift treats the database objects they create as unknown | always (marker only) |
| I1 | **(Rev 2) after v1:** **(Rev 1, B1)** import-failure forms (the class, and the rest of its module, is never created): an F5 mapping that fails or is unproven (A1-A3, A1m) without `sa_type`/`sa_column`; F7r; F4's `SET NULL` case; a keyword outside `sqlmodel.Field`'s signature or a `**` splat in a `Field` call; a chain class's `sa_column=Column(...)` member that is effective (C2) in more than one table class (`Column object already assigned`); a chain class's `__table_args__` that is effective in more than one table class (constraint and index objects cannot move between tables; conservative for unnamed `UniqueConstraint`, which imports); a table whose column set is complete with no primary-key column (`could not assemble any primary key`; **(Rev 3)** only when the constraint set is also proven to hold no primary key: no `__table_args__` in the chain, or a tuple of TA1-TA3 calls only); two claimed tables of the repository with the same name in the default metadata (`already defined`) | no new field: computed from the C/M/A/F/K payload | none | **(Rev 2)** in v1 no module is an I1 module: a proven failure (P1) is a diagnostic and no table for that class; every other form is assumption V7 |
| I2 | **(Rev 2) after v1:** **(Rev 1)** withdrawal: importing a module executes its ancestor packages' `__init__` modules and, transitively, every module they and it import at module level (class bodies and conditional blocks included; function bodies and `if TYPE_CHECKING:` excluded), resolved by the resolver | per-file import syntax already cached (`facts.Import`, scope) | none | **(Rev 2)** in v1 nothing is withdrawn (V7). After v1: when an I1 form occurs in any repository module of that closure, every table claim of the module is withdrawn (diagnostic naming the I1 module and row); langflow: `models/__init__.py` imports every model module, so one I1 form there withdraws all 45 tables |
| D | every other form, and any form above whose text does not parse as its row describes | member kind and line, so the default is visible | none for the class or column it touches; partial with reason D | - |

Allowlists (part of the contract; a change is a contract change):

- **K-dec** method decorators: `classmethod`, `staticmethod`, `property`,
  `field_validator`, `field_serializer`, `model_validator`,
  `model_serializer`, `validator`, `root_validator`, `computed_field`
  (pydantic), `validates` (`sqlalchemy.orm`), each resolving to its module.
- **K-cfg** config keys: `arbitrary_types_allowed`, `validate_assignment`,
  `from_attributes`, `populate_by_name`, `extra`, `frozen`,
  `use_enum_values`, `str_strip_whitespace`, `json_schema_extra`,
  `validate_default`, `ser_json_timedelta`.
- **K-mapper** `__mapper_args__` keys: `confirm_deleted_rows`,
  `eager_defaults`.
- **K-len** Field keywords that keep `max_length` readable for `AutoString`:
  F1-F4 keywords, `default`, `default_factory`, `description`, `title`,
  `exclude`, `repr`, `alias`, `min_length`, `max_length`
  (`get_field_metadata`, `_compat.py` 201-205, returns the first matching
  metadata object, so another constraint could hide `max_length`).

Facts never claimed (class rules from the investigation):

- **Column order** (PF2). Model columns carry no position claim. A view
  lists them in source order and says so.
- **Database names derived by naming conventions** (PF3): index names of
  `index=True` columns, PK/FK/UNIQUE names SQLAlchemy generates, and the
  final database name of a written `name=`. Written names are claimed as
  written; `conv(...)` is recorded as written-name-final.
- **Relationship facts** (M5): relationships are ORM configuration, not
  schema; their misconfiguration does not change `MetaData` (PF7).
- **Complete sets** (PF9): a table's column set and constraint set are
  complete only when every member of its chain is read and no X1 site can
  touch it (in the investigation: langflow's `auth/sso.py` appends three
  `CheckConstraint`s to `SSOConfig.__table__` in a module-level function
  called at import). X1 sites in files outside G1 are not captured (a
  known gap of the gate; such a file must import `sqlalchemy` or a module
  re-exporting it to build a constraint, which the gate does not follow).
- **Schema** (database schema of the table): claimed `null` (default
  schema) only when no `__table_args__` in the chain, or a tuple without an
  options dict (M11).

## P1 (Rev 2) Failures the source proves stay diagnostics

A form is a **proven failure** when the pinned libraries' source (V1)
raises while creating the class for the inputs as written, and every input
the failing check reads is proven by the contract's rows from that one
class statement and its proven chain (C2): literals and resolved names,
no unknown value, no unread member, and no dependence on other classes or
on which modules are imported together. For a proven failure T018c2 emits
a diagnostic naming the row and the library check, and claims no table for
that class (a class that cannot be created is never shown as a table); it
withdraws nothing else. Every other import-failure form is covered by V7.
