//! Version-dependent Axum forms: the charset of text responses, Form on
//! HEAD and on `any`, HeaderMap before a body extractor.

use axum::extract::Form;
use axum::http::{HeaderMap, StatusCode};
use axum::response::Html;
use axum::routing::{any, get, head, post};
use axum::{Json, Router};

#[derive(serde::Deserialize, serde::Serialize)]
pub struct Login {
    pub user: String,
}

pub async fn text() -> String {
    "hi".to_string()
}

pub async fn page() -> Html<&'static str> {
    Html("<p>hi</p>")
}

pub async fn form_head(Form(l): Form<Login>) -> StatusCode {
    let _ = l;
    StatusCode::OK
}

pub async fn form_any(Form(l): Form<Login>) -> String {
    l.user
}

pub async fn headers_first(headers: HeaderMap, Json(l): Json<Login>) -> StatusCode {
    if headers.contains_key("x-a") && !l.user.is_empty() {
        StatusCode::OK
    } else {
        StatusCode::ACCEPTED
    }
}

pub fn app() -> Router {
    Router::new()
        .route("/text", get(text))
        .route("/page", get(page))
        .route("/form-head", head(form_head))
        .route("/form-any", any(form_any))
        .route("/headers-first", post(headers_first))
}
