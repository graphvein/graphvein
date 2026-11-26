# csharp-best-effort fixture (T021-B3)

One case per row of the C# claim contract (`docs/lang/best-effort.md`);
golden `../csharp-best-effort.golden.json`, test
`internal/index/csharp_test.go`, row coverage
`internal/lang/csharp/csharp_test.go` (`TestFixtureCoversEveryRow`, over
this root, `../csharp-best-effort-external/` and
`../csharp-best-effort-hidden/`). There is no C# compiler
oracle (T021 open point O13 (a)). No file uses an inert name: the fixture
has no ignore file and no ignored path.

| Row | Case |
|---|---|
| BE-1 | every `.cs` file: file record `precision: "best_effort"` |
| BE-2 | `src/Broken.cs` (`#if` inside a base list: ERROR; its method `Keep` is not claimed) |
| BE-3 | not a fixture case: `TestJavaExtractorPanicIsFileLocal` covers the shared indexer path |
| BE-4 | every node's `contains` edge; `Order.InField` (a local function in a field's lambda is owned by the type) |
| BE-5 | `Order.Platform@overload(int)@2`, `@3` (`#if` alternatives), `` Result`1@2 `` (same name and arity in two namespaces of one file) |
| BE-6 | `Order.Order@overload(...)`, `Order.this@overload(...)`, `Order.operator -@overload(...)`, `InvoiceExtensions.Touch@overload(this Invoice,int)` (attribute and comment removed) |
| BE-7 | every node's span and `content_hash` (the `///` comment is outside `Order`'s span; `[Serializable]` is inside) |
| BE-8 | `Program.cs` line 2 (`using static Shop.Hidden.Message`): the refused `Broken.cs` has one file-scoped namespace, `Shop.Hidden`, so it hides usings of that namespace, of names under it and of its prefix `Shop` only (T021-B4); a refused file with a block namespace hides every would-be external using (`../csharp-best-effort-hidden/`) |
| CS-T1 | `Order` (partial, in `Order.cs` and `Order.Lines.cs`: `partial: true` on both), `Invoice`, `Archived` (nested block namespace), `Helper` (global namespace) |
| CS-T2 | `Order.Line`, `Order.Span` (`readonly ref struct`) |
| CS-T3 | `ILookup`, `Order.IAudit` |
| CS-T4 | `Order.Money` (`record struct`: `struct: true`), `Order.Note` (`record class`), `` Result`2 `` |
| CS-T5 | `Order.Status` (members not emitted) |
| CS-T6 | file-scoped `namespace Shop.Orders;` (`Order.cs`), block and nested block namespaces (`Order.Lines.cs`, `Invoice.cs`), the `#if`/`#elif`/`#else` branches in `Order` |
| CS-F1 | `Order.Run`, `Order.CompareTo` and `Order.CompareTo@IComparable`, `Order.Equals@IEquatable`, `Order.IAudit.Audit`, `StringExtensions.IsShort` |
| CS-F2 | `Order.Order@static`, `Order.Order@overload()`, `Order.Order@overload(int,ref string,params int[])` |
| CS-F3 | `Order.~Order` |
| CS-F4 | `Order.Run.Local`, `Order.Run.InLambda` (lambda: no owner segment), `Order.Name.Trim` (in an accessor), `Order.InField`; top-level `Add`, `Log`, `Log.Inner`, `InTopLambda` (`src/Tool/Program.cs`) |
| CS-F5 | `operator +`, `operator checked +`, unary and binary `operator -`, `operator >>`, `operator true`, `operator false`, `implicit operator int`, `explicit operator Order` |
| CS-P1 | `Order.Id`, `Order.Name`, `Order.Count@ILookup`, `` Result`1.Value ``, `ILookup.Count` |
| CS-P2 | `Order.this@overload(int)`, `Order.this@overload(string,params object[])`, `Order.this@ILookup`, `ILookup.this` |
| CS-G1 | `src/Shop/StringExtensions.cs`: the C# 14 extension block (a clean misparse as a constructor `extension` with local function `IsBlank`) claims nothing |
| CS-R1 | the event, delegate, field, enum members, accessors and lambdas in `Order` |
| CS-I1 | `Order.cs` lines 6-9 (declared namespace, prefix, `static` nested type, alias of a type), `Order.Lines.cs` line 5 (`Billing` inside `Shop.Orders`, found as `Shop.Billing`) |
| CS-I2 | `Order.cs` lines 2-5 (`global using global::`, plain, `static`, alias), `Order.Lines.cs` line 1, `Program.cs` line 1; also `../csharp-best-effort-external/` |
| CS-I3 | `Order.cs` line 14 (`Shipping` after the file-scoped namespace: may be relative, so not an external) |
| D | `extern alias` (`Order.cs` line 1), alias of a tuple type (line 10) |
