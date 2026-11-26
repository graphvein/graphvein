use async_trait::async_trait;
use axum::body::Body;
use axum::extract::{FromRequest, RequestParts};
use axum::http::header::AUTHORIZATION;

use crate::error::Error;

const SCHEME_PREFIX: &str = "Token ";

pub struct AuthUser {
    pub token: String,
}

#[async_trait]
impl FromRequest<Body> for AuthUser {
    type Rejection = Error;

    async fn from_request(req: &mut RequestParts<Body>) -> Result<Self, Self::Rejection> {
        let header = req
            .headers()
            .ok_or(Error::Unauthorized)?
            .get(AUTHORIZATION)
            .ok_or(Error::Unauthorized)?;
        let text = header.to_str().map_err(|_| Error::Unauthorized)?;
        if !text.starts_with(SCHEME_PREFIX) {
            return Err(Error::Unauthorized);
        }
        Ok(AuthUser { token: text[SCHEME_PREFIX.len()..].to_string() })
    }
}
