use axum::{Router, routing::get};
fn handler() {}
fn app() -> Router {
    Router::new().route("/api/rust-head", get(handler))
}
