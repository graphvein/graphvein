use std::collections::HashMap;

use axum::extract::{Form, Path, Query, Request, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Redirect};
use axum::Json;
use axum_extra::headers::UserAgent;
use axum_extra::TypedHeader;

use crate::error::AppError;
use crate::extract::{ApiKey, Caller, Tenant};
use crate::helpers;
use crate::models::{Batch, Envelope, Event, Health, Item, Kind, Login, NewItem, Page, TagParams, TagResult};
use crate::AppState;

pub async fn get_item(Path(id): Path<u32>) -> Json<Item> {
    Json(Item { id, ..Item::default() })
}

pub async fn delete_item(Path(id): Path<u32>, _key: ApiKey) -> StatusCode {
    let _ = id;
    StatusCode::NO_CONTENT
}

pub async fn list_items(
    State(state): State<AppState>,
    Query(page): Query<Page>,
) -> Result<Json<Envelope<Vec<Item>>>, AppError> {
    if page.q.is_empty() || state.name.is_empty() && page.limit > 100 {
        return Err(AppError::Invalid(page.q));
    }
    Ok(Json(Envelope { data: Vec::new(), count: helpers::count() }))
}

pub async fn create_item(caller: Caller, Json(input): Json<NewItem>) -> Result<(StatusCode, Json<Item>), AppError> {
    if caller.token.is_empty() || caller.token == "revoked" {
        return Err(AppError::Unauthorized);
    }
    let item = Item { id: 1, name: input.item_name, note: input.note, tags: input.tags, kind: input.kind, secret: String::new() };
    Ok((StatusCode::CREATED, Json(item)))
}

pub async fn tag_item(Path(params): Path<TagParams>) -> impl IntoResponse {
    let result = TagResult { id: params.id, label: params.label };
    (StatusCode::OK, Json(result))
}

pub async fn pair(Path((a, b)): Path<(String, i64)>) -> String {
    format!("{a}{b}")
}

pub async fn search(Query(q): Query<HashMap<String, String>>, headers: HeaderMap) -> impl IntoResponse {
    let term = q.get("term").cloned().unwrap_or_default();
    let trace = helpers::trace(&headers);
    let _ = helpers::reset(headers.clone());
    if headers.contains_key("x-dry-run") {
        return StatusCode::NO_CONTENT.into_response();
    }
    Json(Envelope { data: vec![term], count: trace.map_or(0, |t| t.len()) }).into_response()
}

pub async fn submit_form(Form(login): Form<Login>) -> Redirect {
    let _ = login;
    Redirect::to("/")
}

pub async fn form_get(Form(login): Form<Login>) -> String {
    login.user
}

pub async fn whoami(TypedHeader(agent): TypedHeader<UserAgent>, headers: HeaderMap) -> String {
    let id = headers.get("x-request-id").and_then(|v| v.to_str().ok()).unwrap_or("");
    format!("{}{}", agent.as_str(), id)
}

pub async fn echo_text(body: String) -> String {
    body
}

pub async fn raw(req: Request) -> impl IntoResponse {
    if helpers::inspect(&req) {
        return StatusCode::ACCEPTED;
    }
    StatusCode::OK
}

pub async fn deep(headers: HeaderMap) -> StatusCode {
    if helpers::level1(&headers) {
        StatusCode::OK
    } else {
        StatusCode::FORBIDDEN
    }
}

pub async fn looping(headers: HeaderMap) -> StatusCode {
    let mut n = 0;
    for (name, _) in headers.iter() {
        n += name.as_str().len();
    }
    let ok = helpers::ping(&headers, 3) && n > 0;
    std::mem::drop(headers);
    if ok {
        StatusCode::OK
    } else {
        StatusCode::GONE
    }
}

pub async fn go_home() -> Redirect {
    Redirect::permanent("/")
}

pub async fn health() -> impl IntoResponse {
    Json(Health { up_since: 0, ok: true })
}

pub async fn rendered() -> impl IntoResponse {
    helpers::render(7)
}

pub async fn events(Json(events): Json<Vec<Event>>) -> StatusCode {
    if events.is_empty() {
        StatusCode::BAD_REQUEST
    } else {
        StatusCode::ACCEPTED
    }
}

pub async fn wrapped(Json(batch): Json<Envelope<Batch>>) -> Json<Envelope<Kind>> {
    Json(Envelope { data: Kind::Book, count: batch.count })
}

pub async fn maybe(body: Option<Json<NewItem>>) -> StatusCode {
    match body {
        Some(_) => StatusCode::OK,
        None => StatusCode::NO_CONTENT,
    }
}

pub async fn files(Path(rest): Path<String>, tenant: Tenant) -> String {
    format!("{rest}{}", tenant.0)
}
