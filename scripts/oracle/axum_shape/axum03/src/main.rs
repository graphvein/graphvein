// Oracle for the Axum 0.3 route shape fixture: serves the app on
// 127.0.0.1 until killed.
#[tokio::main]
async fn main() {
    let port: u16 = std::env::args().nth(1).and_then(|p| p.parse().ok()).unwrap_or(0);
    let addr = std::net::SocketAddr::from(([127, 0, 0, 1], port));
    eprintln!("listening {}", addr);
    axum::Server::bind(&addr).serve(route_shape_axum03::app().into_make_service()).await.unwrap();
}
