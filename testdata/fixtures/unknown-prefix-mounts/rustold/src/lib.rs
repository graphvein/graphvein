use axum::routing::get;
use axum::Router;

async fn h() -> &'static str {
    "ok"
}

fn inner() -> Router {
    Router::new().route("/", get(h)).route("/items/:id", get(h))
}

// axum 0.3 (outside the checked versions): below the hop, the nested `/`
// is not stored (D, not_stored); /items/:id is.
pub fn app(prefix: String) -> Router {
    Router::new().nest(&prefix, inner())
}

// A route with a trailing slash at the root: its path is left out on an
// unproven version (route_trailing_slash_unproven; D, slash_unproven).
pub fn root() -> Router {
    Router::new().route("/page/", get(h))
}

