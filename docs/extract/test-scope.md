# Test scope

Scope labels retain test code in the graph. internal/testscope.Classify is the
pure classifier shared by route/client linkers and available to future queries
and UI. Paths are repository-relative, case-sensitive, and use forward slashes.

Built-ins classify a file as test when any directory segment is test, tests,
__tests__, __mocks__ or testdata; its Python basename is test_*.py, *_test.py or
conftest.py; its Go basename is *_test.go; or its JS/TS basename matches
?(*.)+(spec|test).?([mc])[jt]s?(x) or *.cy.{js,jsx,ts,tsx}. Rust cfg(test) bodies
inside src, Cargo examples/benches and documentation examples are not included.

Optional tests.include adds patterns; tests.exclude wins over both includes and
built-ins. See [configuration](../config.md) for syntax and strict validation.
Configuration is applied every link pass, without changing scanner visibility
or reparsing unchanged source.

A chain is test-scope if its root definition, a composition call, contributing
FastAPI helper body or invocation site, or route registration is in a test file.
Prefix constants and settings defaults do not affect scope. Path entries merge
by template and slash flag: if any chain is production, scope is omitted;
otherwise scope is test. This applies to Express, FastAPI and Axum. Route nodes
are labelled by registration file; client_call nodes by invocation file,
including wrapper-derived invocation sites. Other node kinds/files are unlabelled.
Identities, spans and hashes stay unchanged.

Matching applies scope only to production calls, after the existing winning
specificity/method/slash-twin selection. A mixed tier drops test-only entries;
one remaining target links at existing confidence, several stay ambiguous.
An entirely test-only tier gives test_only, sorted candidates capped at ten,
full candidate_count when at least two (zero for one), and no calls_route edge.
It never falls through to a weaker production match. Test calls ignore scope
and match exactly as before. Node status order is dynamic > unmatched >
test_only > ambiguous; all linked entries produce linked. Wrapper status is
unchanged. Impact and callers still traverse test calls; diagnostics and CLI
flags/format are unchanged apart from the attributes. Attribute string values
are checked for U+0000-U+001F in fixture and corpus verification.

See [ADR 0021](../adr/0021-test-scope.md).
