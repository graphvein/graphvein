//! Route shape fixture for Axum 0.3 (docs/contract/claims/route-shape-axum.md):
//! `:name` templates, `RequestParts` extractors whose `headers()` is an
//! Option, a `Result` alias with a default error type.

pub mod error;
pub mod extractor;
pub mod review;
pub mod users;

use axum::routing::{get, head, post};
use axum::{AddExtensionLayer, Router};

#[derive(Clone)]
pub struct ApiContext {
    pub name: String,
}

pub type Result<T, E = error::Error> = std::result::Result<T, E>;

pub fn app() -> Router {
    Router::new()
        .route("/api/users/:id", get(users::get_user).put(users::update_user))
        .route("/api/users", post(users::create_user))
        .route("/api/feed", get(users::feed))
        .route("/api/ping", get(review::ping))
        .route("/api/page", get(review::page))
        .route("/api/hello", get(review::hello))
        .route("/api/created", post(review::created))
        .route("/api/headers-first", post(review::headers_first))
        .route("/api/form-head", head(review::form_head))
        .layer(AddExtensionLayer::new(ApiContext { name: String::new() }))
}
