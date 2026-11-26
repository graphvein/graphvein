use axum::{
    routing::{get, post},
    Router,
};

async fn root() -> &'static str {
    "root"
}

async fn list() -> &'static str {
    "list"
}

async fn show() -> &'static str {
    "show"
}

async fn health() -> &'static str {
    "ok"
}

const V2: &str = "/v2";

fn extra() -> Router {
    Router::new().route("/", post(root)).route("/extra", get(list))
}

fn api() -> Router {
    Router::new()
        .route("/", get(root))
        .route("/users", get(list))
        .route("/users/{id}", get(show))
        .route("/slash/", get(show))
        .nest(V2, Router::new().route("/", get(root)).route("/x", get(list)))
        .merge(extra())
}

// The prefix is a parameter: a hop. Its routes below it are stored.
pub fn app(base: &str) -> Router {
    Router::new()
        .route("/live", get(health))
        .nest(base, api())
}

// Two hops with the same text and inline routers (elided in the hash):
// twins, no value can be tied to either.
pub fn twin(a: String) -> Router {
    let r = Router::new();
    let r = r.nest(&a, Router::new().route("/one", get(list)));
    r.nest(&a, Router::new().route("/two", get(list)))
}

fn deep() -> Router {
    Router::new().route("/", get(root)).route("/d", get(list))
}

// A hop below the read prefix /outer: the value covers only the hop's part.
pub fn outer(base: &str) -> Router {
    Router::new().nest("/outer", Router::new().nest(base, deep()))
}

fn deeper() -> Router {
    Router::new().route("/e", get(list))
}

// A hop below another unknown hop: its value is the whole prefix from the
// root.
pub fn stacked(a: &str, b: &str) -> Router {
    Router::new().nest(a, Router::new().nest(b, deeper()))
}

fn served() -> Router {
    Router::new().route("/", get(root)).route("/s", get(list))
}

// A nest_service hop: the nested `/` is answered at the value and at the
// value with a trailing slash.
pub fn service_hop(base: &str) -> Router {
    Router::new().nest_service(base, served())
}
