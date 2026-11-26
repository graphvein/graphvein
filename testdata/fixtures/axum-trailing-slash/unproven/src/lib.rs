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

fn svc() -> Router {
    Router::new()
        .route("/", get(h))
        .route("/a", get(h))
        .route("/t/", get(h))
}

fn wrapped() -> Router {
    Router::new().route("/w", get(h))
}

pub fn app() -> Router {
    Router::new()
        .route("/t/", get(h))
        .route("/u", get(h))
        .nest("/n", inner())
        .nest("/p/", slashed())
        .nest_service("/svc", svc())
        .nest_service("/wrap", tower::ServiceBuilder::new().service(wrapped()))
}
