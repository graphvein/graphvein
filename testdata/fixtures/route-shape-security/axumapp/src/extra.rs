//! Axum forms the oracle crate does not compile (this file is not in the
//! crate's module tree): axum-login's login_required! layer (row SH-S16),
//! a from_fn layer and a repository extractor, which stay as written.
use axum::{middleware, routing::get, Router};

pub fn extra() -> Router {
    Router::new()
        .route("/login-required", get(plain))
        .route_layer(axum_login::login_required!(Backend, login_url = "/login"))
        .route("/from-fn", get(plain))
        .route_layer(middleware::from_fn(check))
        .route("/axum-custom", get(custom))
}

async fn plain() -> &'static str {
    "ok"
}

async fn custom(_user: crate::extra::AuthUser) -> &'static str {
    "ok"
}

pub struct AuthUser;

impl<S: Send + Sync> axum::extract::FromRequestParts<S> for AuthUser {
    type Rejection = axum::http::StatusCode;

    async fn from_request_parts(parts: &mut axum::http::request::Parts, _state: &S) -> Result<Self, Self::Rejection> {
        if parts.headers.contains_key("x-user") {
            Ok(AuthUser)
        } else {
            Err(axum::http::StatusCode::UNAUTHORIZED)
        }
    }
}

async fn check(req: axum::extract::Request, next: middleware::Next) -> axum::response::Response {
    next.run(req).await
}
