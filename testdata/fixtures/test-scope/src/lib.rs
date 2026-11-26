use axum::{Router, routing::get};
async fn shared_handler() {}
pub fn shared() -> Router {
    Router::new().route("/shared-rust", get(shared_handler))
}
