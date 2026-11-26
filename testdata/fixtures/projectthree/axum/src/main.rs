use axum::{routing::get, Router};

async fn current_user() -> &'static str {
    "user"
}

async fn list_articles() -> &'static str {
    "articles"
}

pub fn app() -> Router {
    Router::new()
        .route("/t022r/user", get(current_user))
        .route("/t022r/articles", get(list_articles))
}
