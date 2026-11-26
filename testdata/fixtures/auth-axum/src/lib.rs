// Axum layers (rows DA-X1, DA-X2). Syntax only: never compiled or run.
use axum::{middleware, routing::get, Router};
use axum_login::login_required;
use tower_http::cors::CorsLayer;
use tower_http::trace::TraceLayer;
use tower_http::validate_request::ValidateRequestHeaderLayer;
use tower::ServiceBuilder;
use tower_http::request_id::MakeRequestUuid;
use tower_http::ServiceBuilderExt;

mod client;

async fn h() {}

async fn auth(req: axum::extract::Request, next: middleware::Next) -> axum::response::Response {
    next.run(req).await
}

struct Backend;

// route_layer wraps only the routes added before it.
pub fn api() -> Router {
    Router::new()
        .route("/open", get(h))
        .route_layer(ValidateRequestHeaderLayer::bearer("token"))
        .route("/after-bearer", get(h))
        .route_layer(middleware::from_fn(auth))
}

pub fn admin() -> Router {
    Router::new()
        .route("/users", get(h))
        .route("/method-layer", get(h).layer(CorsLayer::permissive()))
        .route_layer(login_required!(Backend, login_url = "/login"))
        .layer(ValidateRequestHeaderLayer::basic("user", "password"))
}

pub fn shared() -> Router {
    Router::new().route("/shared", get(h))
}

// A nested or merged router keeps its own layers and gains the parent's
// later ones; a router merged after a layer does not.
pub fn app(flag: bool, state: u8) -> Router {
    let mut r = Router::new()
        .route("/health", get(h))
        .nest("/api", api())
        .merge(admin())
        .layer(TraceLayer::new_for_http())
        .merge(shared());
    if flag {
        r = r.layer(middleware::from_fn_with_state(state, auth));
    }
    r.route("/late", get(h))
}

// shared() is also merged here: its route has two chains.
pub fn other() -> Router {
    Router::new().merge(shared()).layer(CorsLayer::permissive())
}

// DA-X3: a method router's layer wraps the methods chained before it only.
pub fn methods() -> Router {
    Router::new().route("/m", get(h).post(h).layer(CorsLayer::permissive()).put(h))
}

// DA-X4: a tower ServiceBuilder stack names each inner layer, outermost
// first; a method no list names leaves the stack unnamed.
pub fn stacked() -> Router {
    Router::new()
        .route("/stacked", get(h))
        .layer(
            ServiceBuilder::new()
                .set_x_request_id(MakeRequestUuid)
                .layer(TraceLayer::new_for_http())
                .layer(middleware::from_fn(auth))
                .propagate_x_request_id()
                .into_inner(),
        )
}

pub fn unlisted() -> Router {
    Router::new()
        .route("/unlisted", get(h))
        .layer(ServiceBuilder::new().layer(TraceLayer::new_for_http()).custom_layer())
}
