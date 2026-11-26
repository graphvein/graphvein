use axum::{Router, routing::get};
use std::sync::Arc;

pub struct Item {
    pub name: String,
}

impl Item {
    pub fn new(name: String) -> Self {
        Self { name }
    }
}

pub async fn handler() -> &'static str {
    "items"
}

pub fn app() -> Router {
    Router::new().route("/api/items", get(handler))
}
