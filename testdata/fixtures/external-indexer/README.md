# External producer fixture

`snapshot.golden.ndjson` is a fixed contract v1 snapshot, reused from the small
hand-verified contract fixture: two empty files, three nodes and two edges.
`dump.golden.json` pins the published graph. No prior golden files were changed.
The files intentionally have synthetic spans; contract v1 does not check source
bounds. The helper-process test binary emits this stream, with explicit mutation
modes for failures and an additional invocation diagnostic for `echo-args`.

No live ignore files or ignored directory names are present; no inert-name mapping
is required. Executables, configs, stores and copies are created only in temporary
test directories. Tests never build a producer executable.
