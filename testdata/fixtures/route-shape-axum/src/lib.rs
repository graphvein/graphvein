//! Route shape fixture for Axum 0.8 (docs/contract/claims/route-shape-axum.md).
//! The oracle builds this crate against axum 0.8.8 and serves `app()`.

pub mod error;
pub mod extract;
pub mod handlers;
pub mod helpers;
pub mod models;
pub mod review;

use axum::routing::{any, get, head, post, put};
use axum::Router;

#[derive(Clone, Default)]
pub struct AppState {
    pub name: String,
}

pub fn app() -> Router {
    let api = Router::new()
        .route("/items/{id}", get(handlers::get_item).delete(handlers::delete_item))
        .route("/items", get(handlers::list_items).post(handlers::create_item))
        .route("/items/{id}/tags/{tag}", put(handlers::tag_item))
        .route("/pair/{a}/{b}", get(handlers::pair))
        .route("/search", get(handlers::search))
        .route("/form", post(handlers::submit_form).get(handlers::form_get))
        .route("/whoami", get(handlers::whoami))
        .route("/echo", post(handlers::echo_text))
        .route("/raw", post(handlers::raw))
        .route("/deep", get(handlers::deep))
        .route("/loop", get(handlers::looping))
        .route("/redirect", get(handlers::go_home))
        .route("/health", get(handlers::health))
        .route("/render", get(handlers::rendered))
        .route("/events", post(handlers::events))
        .route("/wrapped", post(handlers::wrapped))
        .route("/maybe", post(handlers::maybe))
        .route("/files/{*rest}", get(handlers::files))
        .route("/closure", get(|| async { "inline" }))
        .route("/r/guard", get(review::guard_err))
        .route("/r/if", get(review::if_err))
        .route("/r/arms", get(review::arm_err))
        .route("/r/only-err", get(review::only_err))
        .route("/r/serde-as", post(review::serde_as_echo))
        .route("/r/cfg-named", get(review::cfg_named))
        .route("/r/camel", post(review::camel))
        .route("/r/lower", get(review::lower))
        .route("/r/q-forms", get(review::q_forms))
        .route("/r/problem", get(review::problem))
        .route("/r/traced", get(review::traced))
        .route("/r/problem-dyn", get(review::problem_dyn))
        .route("/r/two-json", get(review::two_json))
        .route("/r/shadow", post(review::shadow))
        .route("/r/shadow-helper", post(review::shadow_helper))
        .route("/r/metric", get(review::metric))
        .route("/r/any-form", any(review::any_form))
        .route("/r/form-head", head(review::form_head))
        .route("/r/closure-shadow", get(review::closure_shadow))
        .route("/r/via-extract", get(review::via_extract))
        .route("/r/deep-query", get(review::deep_query))
        .route("/r/via-macro", get(review::via_macro))
        .route("/r/boxed", post(review::boxed));
    Router::new().nest("/v1", api).with_state(AppState::default())
}
