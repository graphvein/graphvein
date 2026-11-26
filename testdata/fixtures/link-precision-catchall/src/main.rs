use axum::{routing::get, Router};

async fn asset() -> &'static str {
    "ok"
}

#[tokio::main]
async fn main() {
    let app = Router::new()
        .route("/t035-axum/assets/logo", get(asset))
        .route("/t035-axum/assets/{*rest}", get(asset));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:3000").await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
