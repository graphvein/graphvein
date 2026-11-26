use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;

use crate::models::ErrorBody;

#[derive(Debug)]
pub enum AppError {
    NotFound,
    Unauthorized,
    Invalid(String),
    Internal,
}

impl AppError {
    fn status(&self) -> StatusCode {
        match self {
            AppError::NotFound => StatusCode::NOT_FOUND,
            AppError::Unauthorized => StatusCode::UNAUTHORIZED,
            AppError::Invalid(_) => StatusCode::UNPROCESSABLE_ENTITY,
            AppError::Internal => StatusCode::INTERNAL_SERVER_ERROR,
        }
    }
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let message = format!("{:?}", self);
        (self.status(), Json(ErrorBody { message })).into_response()
    }
}
