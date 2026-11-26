# java-best-effort-external fixture (T021-B3, row BE-8)

The main Java fixture (`../java-best-effort/`) has refused (BE-2) files,
which row BE-8 lets hide the imports they may declare (T021-B3; narrowed to
their packages in T021-B4). This root has no refused or failed file, so
every import that names no scanned type reaches row JV-I8 (external
`java:` nodes). Golden
`../java-best-effort-external.golden.json`, test
`internal/index/java_test.go`; row coverage over both roots
`internal/lang/java/java_test.go` (`TestFixtureCoversEveryRow`). No file
uses an inert name: the fixture has no ignore file and no ignored path.

| Row | Case |
|---|---|
| JV-I2 | `App.java` line 3 (`com.example.app.model.Item`) |
| JV-I8 | `App.java` lines 4-6 |
