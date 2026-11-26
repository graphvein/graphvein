//! Axum routes of the route-shape-security fixture (T029): typed
//! Authorization extractors and tower-http's ValidateRequestHeaderLayer.
use axum::{routing::get, Router};
use axum_extra::headers::authorization::{Basic, Bearer};
use axum_extra::headers::Authorization;
use axum_extra::TypedHeader;
use tower_http::validate_request::ValidateRequestHeaderLayer;

/// The fixture's router.
pub fn app() -> Router {
    let bearer = Router::new()
        .route("/layer", get(plain))
        .route_layer(ValidateRequestHeaderLayer::bearer("layer-token"));
    let basic = Router::new()
        .route("/layer-basic", get(plain))
        .route_layer(ValidateRequestHeaderLayer::basic("user", "pass"));
    Router::new()
        .route("/typed", get(typed))
        .route("/typed-basic", get(typed_basic))
        .route("/maybe", get(maybe))
        .route("/open", get(plain))
        .merge(bearer)
        .merge(basic)
}

async fn typed(TypedHeader(auth): TypedHeader<Authorization<Bearer>>) -> String {
    auth.token().to_string()
}

async fn typed_basic(TypedHeader(auth): TypedHeader<Authorization<Basic>>) -> String {
    auth.username().to_string()
}

async fn maybe(auth: Option<TypedHeader<Authorization<Bearer>>>) -> String {
    match auth {
        Some(TypedHeader(a)) => a.token().to_string(),
        None => "anonymous".to_string(),
    }
}

async fn plain() -> &'static str {
    "ok"
}
