use axum::{Router, routing::get};
use super::custom::h;
pub fn factory() -> Router { Router::new().route("/nested", get(h)) }
