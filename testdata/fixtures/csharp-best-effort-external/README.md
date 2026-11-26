# csharp-best-effort-external fixture (T021-B3)

The main C# fixture (`../csharp-best-effort/`) has a refused (BE-2) file,
which row BE-8 lets hide the usings it may declare (T021-B3; narrowed to
its file-scoped namespace in T021-B4). This root has no refused or failed
file, so its usings reach row CS-I2 (external `dotnet:` nodes), or row
CS-I4 (an `unresolved_import` warning) for names under a repository
namespace of two or more segments (T021-F1). `src/Web/Components/App.razor` is
not read as C#: it stands for the Razor components whose namespace no `.cs`
file declares. Golden
`../csharp-best-effort-external.golden.json`, test
`internal/index/csharp_test.go`; row coverage over both roots
`internal/lang/csharp/csharp_test.go` (`TestFixtureCoversEveryRow`). No
file uses an inert name: the fixture has no ignore file and no ignored
path.

| Row | Case |
|---|---|
| CS-I1 | `Program.cs` line 5 (`App.Models`), line 10 (relative `Models` inside `App.Services`) |
| CS-I2 | `Program.cs` lines 1-4 (`global using global::`, plain, `static`, alias) |
| CS-I2 | `src/Web/Startup.cs` line 3 (`App.Components`: its only repository prefix, `App`, has one segment) |
| CS-I3 | `Program.cs` line 11 (`Logging` inside `App.Services`) |
| CS-I4 | `src/Web/Startup.cs` line 1 (`Shop.Web.Components`, the folder namespace of the Razor component `src/Web/Components/App.razor`, which no `.cs` file declares; `Shop.Web` encloses the declared `Shop.Web.Services`), line 2 (`Shop.Web.Services.Generated`, generated code under a declared namespace) |
| D | `Program.cs` line 6 (alias of a tuple type) |
