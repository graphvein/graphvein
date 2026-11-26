use axum::body::{Bytes, Full, HttpBody};
use axum::http::{Response, StatusCode};
use axum::response::IntoResponse;

#[derive(Debug)]
pub enum Error {
    Unauthorized,
    NotFound,
    Conflict(String),
}

impl Error {
    fn status_code(&self) -> StatusCode {
        match self {
            Self::Unauthorized => StatusCode::UNAUTHORIZED,
            Self::NotFound => StatusCode::NOT_FOUND,
            Self::Conflict(_) => StatusCode::CONFLICT,
        }
    }
}

impl IntoResponse for Error {
    type Body = Full<Bytes>;
    type BodyError = <Full<Bytes> as HttpBody>::Error;

    fn into_response(self) -> Response<Self::Body> {
        (self.status_code(), format!("{:?}", self)).into_response()
    }
}
