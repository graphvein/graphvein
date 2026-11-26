// A tower client stack (kftray's shape): not an axum Router, so no layer
// fact.
use tower::ServiceBuilder;
use tower_http::validate_request::ValidateRequestHeaderLayer;

pub fn client<S>(svc: S) -> impl Sized {
    ServiceBuilder::new()
        .layer(ValidateRequestHeaderLayer::bearer("token"))
        .service(svc)
}
