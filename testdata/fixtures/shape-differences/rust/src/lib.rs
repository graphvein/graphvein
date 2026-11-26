use axum::routing::post;
use axum::{Json, Router};
use serde::Deserialize;

/// `userName` is accepted for `user_name` (also with deny_unknown_fields).
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct NewUser {
    #[serde(alias = "userName")]
    pub user_name: String,
    pub age: u32,
}

/// Without an alias the names are exactly the fields.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct NewTeam {
    pub title: String,
}

async fn create_user(Json(input): Json<NewUser>) -> String {
    input.user_name
}

async fn create_team(Json(input): Json<NewTeam>) -> String {
    input.title
}

pub fn app() -> Router {
    Router::new()
        .route("/w4/x/users", post(create_user))
        .route("/w4/x/teams", post(create_team))
}
