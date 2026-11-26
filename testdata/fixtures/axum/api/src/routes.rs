use axum::{Router as Api, routing::{self, get, any, on, get_service, MethodFilter}};
use crate::handlers::*;
use crate::handlers::{self, a as first}; use self::custom::h as a;
const PATH: &str = "/";
#[path = "custom.rs"]
mod custom;
mod nested;
pub fn factory() -> Api {
    let r = Api::new().route(PATH, get(first).post(handlers::b));
    let r = r.route("/any", any(first)).route("/filter", on(MethodFilter::GET | MethodFilter::POST, first));
    let mut r = r.route("/service", get_service(svc)).route("/closure", routing::get(|| async {}));
    r = r.route(r#"/:id/*rest"#, get(a)).route("/{id}", get(custom::h));
    r = r.merge(nested::factory()).layer(layer).with_state(state);
    r
}
pub fn dead() -> Api { Api::default().route("/", get(first)) }
pub fn dynamic() -> Api {
    let mr = get(first);
    Api::new().route("/dynamic", mr).route(format!("/{}", id), get(first))
}
