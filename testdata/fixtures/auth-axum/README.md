# auth-axum fixture (T026 AG-B4b)

Hand-written Rust, syntax only (never compiled); no inert-name mapping
needed. `TestAuthAxumContract` indexes it (rows DA-X1, DA-X2, DAD of
docs/contract/claims/architecture-graph.md; golden `auth-axum.golden.json`).
`src/lib.rs`: `route_layer` before and after routes, `layer` after
`nest`/`merge`, a router merged after a layer, a conditional layer, a
factory merged into two parents (layers on some chains only), the closed
list (`ValidateRequestHeaderLayer::bearer/basic`, `from_fn`,
`from_fn_with_state`, `login_required!`) and a method-router layer.
`src/client.rs`: a tower `ServiceBuilder` client stack that must give no
layer fact (kftray's shape). Mapped: no oracle.
