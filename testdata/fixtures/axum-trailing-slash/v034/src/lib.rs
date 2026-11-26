use axum::{routing::get, Router};

async fn h() -> &'static str {
    "ok"
}

fn inner() -> Router {
    Router::new()
        .route("/", get(h))
        .route("/t/", get(h))
        .route("/u", get(h))
}

fn slashed() -> Router {
    Router::new().route("/x", get(h)).route("/", get(h))
}

pub fn app() -> Router {
    Router::new()
        .route("/t/", get(h))
        .route("/u", get(h))
        .nest("/n", inner())
        .nest("/p/", slashed())
}
