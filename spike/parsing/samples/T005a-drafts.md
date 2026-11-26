# T005a hand-written drafts

The original `c_sharp.cs.golden-draft.json` and `dart.dart.golden-draft.json`
expectations were hand-written in T005a. T005b verified them through run A's
SDK-clang WASI modules and promoted them unchanged to `.golden.json`. Lines are
one-based name locations (C# attributes begin earlier than the names).
Constructors are omitted like the existing Java sample.

C# includes controller and route attributes, GET and POST handlers, parameter
binding and interpolated strings. Dart includes Flutter inheritance, an async
HTTP call, a widget build method and main. Route/client extraction remains out
of scope; the active entries describe declarations only. HTML/Angular goldens
cover elements and template syntax. `angular-inline.ts.golden.json` reports
original TS lines for both multiline and same-line component templates.
