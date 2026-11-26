use axum::{Router, routing::get};
async fn handler() {}
fn app() -> Router {
    Router::new().route("/t011g/rust", get(handler))
}
fn composed() -> Router {
    Router::new().nest("/t011g/test-rust", scope_fixture::shared())
}
