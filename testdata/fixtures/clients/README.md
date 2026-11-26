# Client calls fixture

Hand-authored fetch/axios, imported instance/factory, defaults and per-call bases;
wrappers through three levels, a fourth caller and a cycle; shadowed transports;
Express specificity, optional, wildcard, ANY, and twelve-way ambiguity;
FastAPI/Axum HEAD differences; R9 opaque helpers and proven environment bases;
R10 parameter-only suffix; configuration replay. No ignore-name mapping is needed.

clients.golden.json is a source-authored projection of the expected call-site
attributes. Span hashes, containment and inferred edges are checked separately.

_axios-package.json is inert in the main fixture. The workspace-shadow subtest
copies it to package.json in a temporary fixture, making the scanned fake.ts
module shadow all external axios imports. No live ignore files are committed.
