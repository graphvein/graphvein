use axum::extract::FromRequestParts;
use axum::http::request::Parts;
use axum::http::StatusCode;

use crate::helpers;

/// Reads the `x-api-key` header itself.
pub struct ApiKey(pub String);

impl<S: Send + Sync> FromRequestParts<S> for ApiKey {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        // Only formatted: the query stays provably none.
        let _trace = format!("{:?}", parts);
        let key = parts
            .headers
            .get("x-api-key")
            .and_then(|v| v.to_str().ok())
            .ok_or(StatusCode::UNAUTHORIZED)?;
        Ok(ApiKey(key.to_owned()))
    }
}

/// Reads the bearer token through a helper.
pub struct Caller {
    pub token: String,
}

impl<S: Send + Sync> FromRequestParts<S> for Caller {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        let token = helpers::bearer(&parts.headers)?;
        Ok(Caller { token })
    }
}

/// Reads the raw query string: the query is not provably empty.
pub struct Tenant(pub String);

impl<S: Send + Sync> FromRequestParts<S> for Tenant {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        Ok(Tenant(parts.uri.query().unwrap_or("").to_string()))
    }
}
