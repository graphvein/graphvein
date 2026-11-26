use axum::{
    routing::{get, post},
    Router,
};

async fn h() -> &'static str {
    "ok"
}

pub struct Config {
    pub base: String,
}

fn api() -> Router {
    Router::new()
        .route("/", get(h))
        .route("/users", get(h))
        .route("/users/{id}", get(h).post(h))
        .route("/assets/{*rest}", get(h))
        .route("/slash/", get(h))
        .nest("/v1", Router::new().route("/status", get(h)))
}

// UP-7, nest(p, r): the prefix is a configuration field.
pub fn app(cfg: &Config) -> Router {
    Router::new()
        .route("/health", get(h))
        .nest(&cfg.base, api())
}

fn served() -> Router {
    Router::new().route("/", get(h)).route("/svc/ping", post(h))
}

// UP-7, nest_service(p, s): the nested `/` is answered at the prefix with
// and without a trailing slash.
pub fn service(base: &str) -> Router {
    Router::new().nest_service(base, served())
}

fn inner() -> Router {
    Router::new().route("/deep/path", get(h))
}

// UP-7 below the read prefix /x (above "/x").
pub fn outer(base: &str) -> Router {
    Router::new().nest("/x", Router::new().nest(base, inner()))
}

fn shared() -> Router {
    Router::new().route("/shared/item", get(h))
}

// UP-9: a router nested at a read prefix and at an unknown one keeps its
// known path and gets no entry.
pub fn both(base: &str) -> Router {
    Router::new().nest("/known", shared()).nest(base, shared())
}
