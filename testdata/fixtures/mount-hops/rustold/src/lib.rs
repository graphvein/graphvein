use axum::routing::get;
use axum::Router;

async fn root() -> &'static str {
    "root"
}

async fn list() -> &'static str {
    "list"
}

fn inner() -> Router {
    Router::new().route("/", get(root)).route("/items/:id", get(list))
}

// axum 0.3: the nested `/` route is version-dependent and is not stored.
pub fn app(prefix: String) -> Router {
    Router::new().nest(&prefix, inner())
}
