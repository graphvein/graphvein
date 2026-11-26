# csharp-best-effort-hidden fixture (T021-B4, B5; row BE-8)

Two refused (BE-2) files whose text does not bound every name they can
declare. `src/Gen/Messages.cs` uses block namespaces (`Gen`, nested
`Messages`, and `Wire.Format`), so it may also declare types in the global
namespace; `src/Gen/Global.cs` has no namespace keyword, so all its types
sit in the global namespace and it declares no namespace. Row BE-8 hides a
plain using (which can name only a namespace) when it is equal to, under or
a dotted prefix of a namespace name found in a refused file's text (a
nested block's composite name `Gen.Messages` is under `Gen`), and every
static and alias using (a global type may be its target); the other usings
stay `dotnet:` externals (CS-I2). The main C# fixture
(`../csharp-best-effort/`) shows the file-scoped case. Golden
`../csharp-best-effort-hidden.golden.json`, test
`internal/index/csharp_test.go`; row coverage over all C# roots
`internal/lang/csharp/csharp_test.go` (`TestFixtureCoversEveryRow`). No
file uses an inert name: the fixture has no ignore file and no ignored
path.

| Row | Case |
|---|---|
| BE-2 | `src/Gen/Messages.cs`, `src/Gen/Global.cs` (`#if` inside a base list) |
| BE-8, plain using under a nested block | `src/App/Client.cs` line 2 (`Gen.Messages`; `Messages.cs` only) |
| BE-8, plain using equal to a name | `src/App/Client.cs` line 3 (`Gen`; `Messages.cs` only) |
| BE-8, plain using that is a dotted prefix | `src/App/Client.cs` line 4 (`Wire` of `Wire.Format`; `Messages.cs` only) |
| BE-8, static and alias usings | `src/App/Client.cs` lines 6-7 (both refused files) |
| CS-I2 | `src/App/Client.cs` lines 1 and 5 (`System`; `GenX.Tools`, whole segments only) |
| CS-I1 | `src/App/Client.cs` line 8 |
| CS-T1, CS-F1, CS-T4 | `Client`, `Client.Send`, `Item` |
