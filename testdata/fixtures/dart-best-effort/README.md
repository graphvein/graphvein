# dart-best-effort fixture (T021-B4)

One case per row of the Dart claim contract (`docs/lang/best-effort.md`);
golden `../dart-best-effort.golden.json`, test
`internal/index/dart_test.go`, row coverage
`internal/lang/dart/dart_test.go` (`TestFixtureCoversEveryRow`). There is
no Dart oracle (T021 open point O13 (a)). No file uses an inert name: the
fixture has no ignore file and no ignored path. Two packages: `acme_app`
(root `pubspec.yaml`) and `acme_core` (`packages/core/pubspec.yaml`, a
quoted name with a comment). Some files are not valid Dart on purpose
(`lib/src/broken.dart`; `Wrong` in `lib/src/modern.dart`); nothing here is
compiled.

| Row | Case |
|---|---|
| BE-1 | every `.dart` file: file record `precision: "best_effort"` |
| BE-2 | `lib/src/broken.dart` (an unfinished edit: ERROR; its intact method `keep` is not claimed) |
| BE-3 | not a fixture case: `TestDartExtractorPanicIsFileLocal` (`internal/index`) |
| BE-4 | every node's `contains` edge; `Shape.inField` (a local function in a field initializer's lambda is owned by the type) |
| BE-5 | `Shape.operator -@2` (unary after binary), `extension on List<int>@2` |
| BE-7 | every node's span and `content_hash` (the `///` comment is outside `Shape`'s span; `@immutable` is inside) |
| DA-T1 | `Shape`, `Square`, `Circle`, `Runner` (`mixin: true`), `Robot` (mixin application), `Result` (`sealed`), `Messages` (in a `*.g.dart` part, O9), `Point`, `Named` (primary constructors), `Empty` (`class Empty;`) |
| DA-T2 | `Walker`, `Swimmer` (`base mixin`) |
| DA-T3 | `Doubling`; anonymous `extension on List<int>` (twice), `extension on Map<String,T>?`, `extension on void Function()` |
| DA-T4 | `Meters` |
| DA-T5 | `Color`, `Level` (primary constructor) |
| DA-F1 | `run`, `greeting` (in a part file), `platform` (two files), `alert` (`external`, annotated), `main` |
| DA-F2 | `Shape.toString` (annotations with a comment between), `Shape.origin` (`static`), `Shape.draw` (abstract), `Shape.paint`, `Walker.walk`, `Color.describe`, ... |
| DA-F3 | `Shape.area`, `Messages.hello`, `Doubling.doubled`, top-level `answer` |
| DA-F4 | `Shape.area=`, top-level `answer=` |
| DA-F5 | `operator +`, binary and unary `operator -`, `operator ==`, `operator []`, `operator []=` |
| DA-F6 | `Shape.paint.stroke`, `Shape.paint.stroke.dot`, `Shape.paint.inLambda` (lambda: no segment), `Shape.inField`, `main.helper`, `inTopLambda` (no owner) |
| DA-C1 | `Shape.new`, `Shape.named`, `Shape.unit` (const), `Shape.square` (factory), `Shape.circle` (redirecting), `Square.new`, `Meters.zero`, `Color.new` (const, enum), `Messages.new`, `Concise.legacy`, `Made._`, `Classic.new` (`factory Classic()`), `Classic._`, `Torn.new` (`Torn.new()`) and `TornConst.new` (`const TornConst.new()`): `.new` is the unnamed constructor |
| DA-C2 | `Concise.new` (`new(this.x)`), `Concise.named` (`new named()`: a `new` token with no `.` before it), `Concise.zero` (`const new zero()`), `Constant.new` (`const new()`) |
| DA-C3 | `Concise.make` (`factory make()`: the name comparison), `Concise.red` (redirecting), `Made.new` (`factory()`) |
| DA-S | annotated and commented `Shape.toString`; `alert` (annotation and top-level `external`); bodyless `Shape.draw` and declarations with initializers (`;` included) |
| DA-G1 | `Concise.untyped`, `Concise.later` (untyped methods that the grammar reads as constructors) and `TornOther`'s `Other.new()` (`.new` after a name that is not the type) are not claimed |
| DA-G2 | `Wrong`'s `factory Other.make()` is not claimed |
| DA-R1 | fields, `Callback` (typedef), enum values, primary constructors and the primary constructor body in `Named`, `handler` (a top-level variable) |
| DA-I1 | `lib/app.dart` `dart:async`, `export 'dart:math'` |
| DA-I2 | `lib/app.dart` `src/shapes.dart` (and the raw string `r'src/shapes.dart'`, one edge with two evidences), `app.dart` (self-import: no edge), the conditional URIs, `export 'src/kinds.dart'`, both `part` directives; `lib/src/shapes.dart` `kinds.dart`; missing `src/missing.dart` and `../../outside.dart` (above the root): `unresolved_import` |
| DA-I3 | `lib/app.dart` `package:acme_app/src/kinds.dart`, `export 'package:acme_app/src/modern.dart'`, missing `package:acme_app/missing.dart` (`unresolved_import`); `packages/core/lib/core.dart` `package:acme_core/src/engine.dart` |
| DA-I4 | `lib/app.dart` `package:acme_core/core.dart`; `packages/core/lib/core.dart` `package:acme_app/app.dart` |
| DA-I5 | `lib/app.dart` `package:http/http.dart`, the conditional `package:web/web.dart` |
| DA-I6 | `lib/app.dart` lines 10-12 |
| DA-I7 | `library acme.app;`, `part of '../app.dart';` (two files) |
| DA-I8 | `lib/app.dart` `https://example.com/remote.dart`, `'src/' 'shapes.dart'` (adjacent strings) |
| D | everything else; the walk descends |
