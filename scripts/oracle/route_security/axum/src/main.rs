//! Serves the route-shape-security fixture's Axum router on 127.0.0.1 for
//! the route security oracle (T029): port from argument 1.
#[tokio::main]
async fn main() {
    let port = std::env::args().nth(1).expect("usage: route-security-oracle PORT");
    let listener = tokio::net::TcpListener::bind(format!("127.0.0.1:{port}")).await.unwrap();
    axum::serve(listener, route_shape_security_axum::app()).await.unwrap();
}
