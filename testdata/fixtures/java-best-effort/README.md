# java-best-effort fixture (T021-B2)

One case per row of the Java claim contract (`docs/lang/best-effort.md`);
golden `../java-best-effort.golden.json`, test
`internal/index/java_test.go`, row coverage
`internal/lang/java/java_test.go` (`TestFixtureCoversEveryRow`), javac
oracle `scripts/oracle/java_decls.sh` (43 of 43 declarations agree). No
file uses an inert name: the fixture has no ignore file and no ignored path.

| Row | Case |
|---|---|
| BE-1 | every `.java` file: file record `precision: "best_effort"` |
| BE-2 | `Broken.java` (ERROR in the class header; its method `keep` is not claimed), `Missing.java` (only a MISSING `;`), `com/acme/secret/Vault.java`, `tools/Scratch.java` (no package declaration) |
| BE-3 | not a fixture case: `TestJavaExtractorPanicIsFileLocal` (`internal/index`) |
| BE-4 | every node's `contains` edge |
| BE-5 | `Sample.blocks.Local`, `Sample.blocks.Local@2` |
| BE-6 | `Sample.Sample`, `Sample.overloaded`, `Sample.Callback.call`, `Sample.Color.Color`, `Sample.Point.Point` (compact constructor: record header types); receiver parameter dropped |
| BE-7 | every node's span and `content_hash` (Javadoc outside `Sample`'s span) |
| BE-8 | `Imports.java` line 17 (`com.acme.secret.Vault`): only the refused `Vault.java` declares package `com.acme.secret`. `Broken.java` and `Missing.java` (package `com.acme.app`) and `Scratch.java` (unnamed package, which no import can name) hide nothing else (T021-B4) |
| JV-T1 | `Sample`, `Circle`, `Order`, `Twin` (two files), `Imports` |
| JV-T2 | `Sample.Callback`, `Shape` (`sealed ... permits`) |
| JV-T3 | `Sample.Color` |
| JV-T4 | `Sample.Point` |
| JV-T5 | `Sample.Marker` |
| JV-T6 | `Sample.Inner`, `Sample.Inner.Deeper`, `Order.Line`, local classes `Sample.blocks.Local`, `Sample.run.InLambda` (in a lambda: no owner segment) |
| JV-F1 | `Sample.run`, `Sample.generic`, `Order.total`, ... |
| JV-F2 | `Sample.Sample`, enum and record constructors, compact constructor `Point { }` |
| JV-F3 | `Sample.Marker.value`, `Sample.Marker.weights` |
| JV-F4 | `tools/Hello.java` (`main`, `greet` without an owner) |
| JV-R1 | the anonymous class in `Sample.run` (`anonymous`, `Hidden`), the `RED` constant body (`special`) |
| JV-R2 | fields, initializers, enum constants, record components, `module-info.java`, `package-info.java` |
| JV-I1 | every `package` declaration |
| JV-I2 | `Imports.java` lines 3-4 (`Order`, member type `Order.Line`); line 5 is a self-import (no edge) |
| JV-I3 | `Imports.java` lines 6-7 |
| JV-I4 | `Imports.java` line 8 |
| JV-I5 | `Imports.java` line 9 (`com.acme.dup.Twin` in `module-a` and `module-b`) |
| JV-I6 | `Imports.java` line 10 |
| JV-I7 | `Imports.java` lines 11-12 (`com.acme.app.model.QOrder`; `com.acme.app.gen.Generated` under package `com.acme.app`) |
| JV-I8 | `Imports.java` lines 13-16, `Sample.java` line 3 (no refused file declares their packages); also `../java-best-effort-external/` |
