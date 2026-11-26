use axum::extract::Request;
use axum::http::{header, HeaderMap, StatusCode};
use axum::response::IntoResponse;
use axum::Json;

use crate::models::{Envelope, TagResult};

pub fn bearer(headers: &HeaderMap) -> Result<String, StatusCode> {
    let value = headers.get(header::AUTHORIZATION).ok_or(StatusCode::UNAUTHORIZED)?;
    let text = value.to_str().map_err(|_| StatusCode::UNAUTHORIZED)?;
    Ok(text.trim_start_matches("Bearer ").to_string())
}

pub fn trace(headers: &HeaderMap) -> Option<String> {
    let id = headers.get("x-trace-id")?;
    id.to_str().ok().map(str::to_string)
}

pub fn inspect(req: &Request) -> bool {
    req.headers().contains_key("x-debug")
}

pub fn level1(h: &HeaderMap) -> bool {
    h.contains_key("x-l1") && level2(h)
}

pub fn level2(h: &HeaderMap) -> bool {
    h.contains_key("x-l2") && level3(h)
}

pub fn level3(h: &HeaderMap) -> bool {
    h.contains_key("x-l3") && level4(h)
}

pub fn level4(h: &HeaderMap) -> bool {
    h.contains_key("x-l4")
}

pub fn ping(h: &HeaderMap, n: u32) -> bool {
    n > 0 && h.contains_key("x-ping") && pong(h, n - 1)
}

pub fn pong(h: &HeaderMap, n: u32) -> bool {
    n == 0 || ping(h, n - 1)
}

pub fn render(id: u32) -> impl IntoResponse {
    (StatusCode::ACCEPTED, Json(Envelope { data: TagResult { id, label: String::new() }, count: 1 }))
}

pub fn count() -> usize {
    1
}

pub fn reset(mut h: HeaderMap) -> bool {
    h = HeaderMap::new();
    h.contains_key("x-after-reset")
}
