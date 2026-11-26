use axum::extract::{Extension, Path, Query};
use axum::Json;

use crate::extractor::AuthUser;
use crate::{ApiContext, Result};

#[derive(serde::Serialize, serde::Deserialize)]
pub struct UserBody<T> {
    pub user: T,
}

#[derive(serde::Deserialize)]
pub struct NewUser {
    pub username: String,
    pub email: String,
}

#[derive(serde::Deserialize, Default)]
#[serde(default)]
pub struct UpdateUser {
    pub email: Option<String>,
    pub bio: Option<String>,
}

#[derive(serde::Serialize)]
pub struct User {
    pub username: String,
    pub bio: Option<String>,
}

#[derive(serde::Deserialize)]
pub struct FeedQuery {
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}

pub async fn get_user(Path(id): Path<i64>, ctx: Extension<ApiContext>) -> Result<Json<UserBody<User>>> {
    if id < 0 {
        return Err(crate::error::Error::NotFound);
    }
    Ok(Json(UserBody { user: User { username: ctx.name.clone(), bio: None } }))
}

pub async fn update_user(
    auth: AuthUser,
    Path(id): Path<i64>,
    Json(req): Json<UserBody<UpdateUser>>,
) -> Result<Json<UserBody<User>>> {
    let _ = (auth.token, id);
    Ok(Json(UserBody { user: User { username: String::new(), bio: req.user.bio } }))
}

pub async fn create_user(Json(req): Json<UserBody<NewUser>>) -> Result<Json<UserBody<User>>> {
    if req.user.email.is_empty() {
        return Err(crate::error::Error::Conflict(req.user.username));
    }
    Ok(Json(UserBody { user: User { username: req.user.username, bio: None } }))
}

pub async fn feed(auth: AuthUser, query: Query<FeedQuery>) -> Result<Json<Vec<String>>> {
    let _ = (auth.token, query.limit, query.offset);
    Ok(Json(Vec::new()))
}
