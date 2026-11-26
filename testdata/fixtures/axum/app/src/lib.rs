use axum::Router; const LOCAL_PREFIX: &str = "/local";
use route_kit::{factory, PREFIX};
mod inline {
    pub fn h() {}
    pub fn router() -> axum::Router { axum::Router::new().route("/inline", axum::routing::get(self::h)) }
}
pub fn app() -> Router {
    let mut app = Router::<State>::new().nest(PREFIX, factory());
    if enabled { app = app.merge(inline::router()); }
    #[cfg(feature = "x")]
    let app = app.nest("/copy", factory()).nest(LOCAL_PREFIX, factory());
    app.nest(format!("/{}", value), unresolved_prefix())
}
fn unresolved_prefix() -> Router { Router::new().route("/lost", axum::routing::get(inline::h)) }
struct LocalRouter;
impl LocalRouter { fn new() -> Self { Self } fn route(self, _: &str, _: ()) -> Self { self } }
fn lookalike() { use LocalRouter as Router; Router::new().route("/false", ()); }
mod fake { struct Router; impl Router { fn new() -> Self { Self } fn route(self, _: &str, _: ()) -> Self { self } } fn lookalike() { Router::new().route("/also-false", ()); } }
