//! Version-dependent forms in axum 0.3: text responses without a charset,
//! HeaderMap taking the headers before a later extractor, Form on HEAD
//! reading the body.

use axum::extract::{Form, Json};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{Html, IntoResponse};

use crate::users::{NewUser, UserBody};

#[derive(serde::Deserialize)]
pub struct Login {
    pub user: String,
}

pub async fn ping() -> &'static str {
    "pong"
}

pub async fn page() -> Html<&'static str> {
    Html("<p>hi</p>")
}

pub async fn hello() -> impl IntoResponse {
    "hello"
}

pub async fn created() -> (StatusCode, String) {
    (StatusCode::CREATED, "x".to_string())
}

/// HeaderMap first: the Json extractor then finds no headers.
pub async fn headers_first(headers: HeaderMap, Json(req): Json<UserBody<NewUser>>) -> StatusCode {
    if headers.contains_key("x-a") && !req.user.username.is_empty() {
        StatusCode::OK
    } else {
        StatusCode::ACCEPTED
    }
}

/// Form on HEAD reads the body (only GET reads the query).
pub async fn form_head(Form(l): Form<Login>) -> StatusCode {
    if l.user.is_empty() {
        StatusCode::ACCEPTED
    } else {
        StatusCode::OK
    }
}
