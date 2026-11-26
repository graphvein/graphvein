//! Forms of the independent review of the Axum shape: error variants
//! with guards and arms on `self`, serde attributes the payload does not
//! model, rename rules, URL-encoded types, tuple header parts, responses
//! with the same status and content type, rebound names, local closures,
//! request parts used other than for headers, and Form on several methods.

use std::collections::HashMap;

use axum::extract::{Form, FromRequestParts, Query};
use axum::http::request::Parts;
use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::{Json, RequestPartsExt};
use serde::{Deserialize, Serialize};
use serde_with::{serde_as, DisplayFromStr};

use crate::models::{Item, Login, SortOrder};

/// A guard on an arm: `Db` answers 404 or 500, `Auth` always 401.
pub enum GuardErr {
    Db(String),
    Auth,
}

impl IntoResponse for GuardErr {
    fn into_response(self) -> Response {
        match self {
            GuardErr::Db(m) if m.is_empty() => StatusCode::NOT_FOUND.into_response(),
            GuardErr::Db(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
            GuardErr::Auth => StatusCode::UNAUTHORIZED.into_response(),
        }
    }
}

pub async fn guard_err(Query(q): Query<HashMap<String, String>>) -> Result<String, GuardErr> {
    if q.contains_key("auth") {
        return Err(GuardErr::Auth);
    }
    Err(GuardErr::Db(q.get("m").cloned().unwrap_or_default()))
}

/// An `if` inside an arm of a status method: `Bad` answers 400 or 409,
/// the wildcard (`Gone` only) 410.
pub enum IfErr {
    Bad(bool),
    Gone,
}

impl IfErr {
    fn status(&self) -> StatusCode {
        match self {
            IfErr::Bad(x) => {
                if *x {
                    StatusCode::BAD_REQUEST
                } else {
                    StatusCode::CONFLICT
                }
            }
            _ => StatusCode::GONE,
        }
    }
}

impl IntoResponse for IfErr {
    fn into_response(self) -> Response {
        (self.status(), "e").into_response()
    }
}

pub async fn if_err(Query(q): Query<HashMap<String, String>>) -> Result<String, IfErr> {
    if q.contains_key("gone") {
        return Err(IfErr::Gone);
    }
    Err(IfErr::Bad(q.contains_key("x")))
}

/// Statuses chosen by arms whose values are `(status, ..).into_response()`.
pub enum ArmErr {
    Missing,
    Taken(String),
}

impl IntoResponse for ArmErr {
    fn into_response(self) -> Response {
        match self {
            ArmErr::Missing => (StatusCode::NOT_FOUND, "missing").into_response(),
            ArmErr::Taken(name) => (StatusCode::CONFLICT, Json(name)).into_response(),
        }
    }
}

pub async fn arm_err(Query(q): Query<HashMap<String, String>>) -> Result<StatusCode, ArmErr> {
    match q.get("name") {
        Some(n) if n.is_empty() => Err(ArmErr::Missing),
        Some(n) => Err(ArmErr::Taken(n.clone())),
        None => Ok(StatusCode::NO_CONTENT),
    }
}

/// A Result that is never Ok: no 200.
pub async fn only_err() -> Result<Json<Item>, ArmErr> {
    Err(ArmErr::Missing)
}

/// serde_with rewrites the field's (de)serializer.
#[serde_as]
#[derive(Deserialize, Serialize)]
pub struct AsStr {
    #[serde_as(as = "DisplayFromStr")]
    pub id: u64,
}

pub async fn serde_as_echo(Json(v): Json<AsStr>) -> Json<AsStr> {
    Json(v)
}

/// A rename rule applied through cfg_attr.
#[derive(Serialize)]
#[cfg_attr(all(), serde(rename_all = "camelCase"))]
pub struct CfgNamed {
    pub some_field: u32,
}

pub async fn cfg_named() -> Json<CfgNamed> {
    Json(CfgNamed { some_field: 1 })
}

/// serde's camelCase field rule: `_foo` is `foo`, `bar_baz` is `barBaz`.
#[derive(Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
pub struct CamelU {
    pub _foo: u32,
    pub bar_baz: u32,
}

pub async fn camel(Json(c): Json<CamelU>) -> Json<CamelU> {
    Json(c)
}

/// serde's lowercase field rule leaves the field unchanged.
#[derive(Deserialize)]
#[serde(rename_all = "lowercase")]
#[allow(non_snake_case)]
pub struct Lower {
    pub userId: u32,
}

pub async fn lower(Query(l): Query<Lower>) -> String {
    l.userId.to_string()
}

/// URL-encoded data: a newtype of a scalar is read, a nested struct and
/// u128 are not.
#[derive(Deserialize)]
pub struct Limit(pub u32);

#[derive(Deserialize)]
pub struct Inner {
    pub a: u32,
}

#[derive(Deserialize)]
pub struct QForms {
    pub limit: Limit,
    pub sort: Option<SortOrder>,
    pub inner: Option<Inner>,
    pub big: Option<u128>,
}

pub async fn q_forms(Query(q): Query<QForms>) -> String {
    format!("{}{}{}{}", q.limit.0, q.sort.is_some(), q.inner.map_or(0, |i| i.a), q.big.unwrap_or(0))
}

#[derive(Serialize)]
pub struct Problem {
    pub title: String,
}

/// A content-type header part replaces the body's content type.
pub async fn problem() -> impl IntoResponse {
    (StatusCode::BAD_REQUEST, [(header::CONTENT_TYPE, "application/problem+json")], Json(Problem { title: "t".into() }))
}

/// A header part that is not content-type keeps the body's.
pub async fn traced() -> (StatusCode, [(&'static str, &'static str); 1], Json<Problem>) {
    (StatusCode::ACCEPTED, [("x-trace", "1")], Json(Problem { title: "t".into() }))
}

/// A content-type part whose value is not a literal: not read.
pub async fn problem_dyn(Query(q): Query<HashMap<String, String>>) -> impl IntoResponse {
    let ct = q.get("ct").cloned().unwrap_or_else(|| "application/json".to_string());
    (StatusCode::CONFLICT, [(header::CONTENT_TYPE, ct)], Json(Problem { title: "t".into() }))
}

#[derive(Serialize)]
pub struct A {
    pub a: u32,
}

#[derive(Serialize)]
pub struct B {
    pub b: String,
}

/// Two JSON bodies with different schemas under one status.
pub async fn two_json(Query(q): Query<HashMap<String, String>>) -> impl IntoResponse {
    if q.contains_key("a") {
        return Json(A { a: 1 }).into_response();
    }
    Json(B { b: "x".into() }).into_response()
}

#[derive(Serialize)]
pub struct User {
    pub id: u64,
    pub name: String,
}

fn lookup(s: &str) -> Option<User> {
    if s.is_empty() {
        None
    } else {
        Some(User { id: 7, name: s.to_string() })
    }
}

/// `body` is rebound by the match arm: it is a User there, not the String.
pub async fn shadow(body: String) -> impl IntoResponse {
    match lookup(&body) {
        Some(body) => Json(body).into_response(),
        None => StatusCode::NOT_FOUND.into_response(),
    }
}

#[derive(Deserialize)]
pub struct Named {
    pub name: String,
}

/// `user` is rebound by `if let`.
pub fn render(user: Named) -> impl IntoResponse {
    if let Some(user) = lookup(&user.name) {
        return Json(user);
    }
    Json(User { id: 0, name: String::new() })
}

pub async fn shadow_helper(Json(input): Json<Named>) -> impl IntoResponse {
    render(input)
}

/// serde_json writes NaN as null.
#[derive(Serialize)]
pub struct Metric {
    pub v: f64,
}

pub async fn metric() -> Json<Metric> {
    Json(Metric { v: f64::NAN })
}

/// Form on `any` reads the query on GET, the body otherwise.
pub async fn any_form(Form(l): Form<Login>) -> String {
    l.user
}

/// Form on HEAD reads the body: RawForm tests for GET only.
pub async fn form_head(Form(l): Form<Login>) -> StatusCode {
    let _ = l;
    StatusCode::OK
}

pub fn respond() -> Json<A> {
    Json(A { a: 1 })
}

/// A local closure shadows the module function `respond`.
pub async fn closure_shadow() -> impl IntoResponse {
    let respond = || Json(B { b: "x".into() });
    respond()
}

#[derive(Deserialize)]
pub struct Filter {
    pub term: String,
}

/// Runs the Query extractor on the parts: the query is read.
pub struct ViaExtract(pub String);

impl<S: Send + Sync> FromRequestParts<S> for ViaExtract {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        let Query(f) = parts.extract::<Query<Filter>>().await.map_err(|_| StatusCode::BAD_REQUEST)?;
        Ok(ViaExtract(f.term))
    }
}

pub async fn via_extract(v: ViaExtract) -> String {
    v.0
}

/// A repository macro named like tracing's `info!`: it runs the Query
/// extractor, so the query is read.
macro_rules! info {
    ($parts:expr) => {
        $parts.extract::<Query<Filter>>().await.map_err(|_| StatusCode::BAD_REQUEST)?
    };
}

pub struct ViaMacro(pub String);

impl<S: Send + Sync> FromRequestParts<S> for ViaMacro {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        let Query(f) = info!(parts);
        Ok(ViaMacro(f.term))
    }
}

pub async fn via_macro(v: ViaMacro) -> String {
    v.0
}

pub fn level2(parts: &Parts) -> String {
    parts.uri.query().unwrap_or("").to_string()
}

pub fn level1(parts: &Parts) -> String {
    level2(parts)
}

/// Reads the query two calls below its impl: at depth 2 the chain is cut.
pub struct DeepTenant(pub String);

impl<S: Send + Sync> FromRequestParts<S> for DeepTenant {
    type Rejection = StatusCode;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        Ok(DeepTenant(level1(parts)))
    }
}

pub async fn deep_query(t: DeepTenant) -> String {
    t.0
}

/// A missing field deserializes `Box<Option<T>>` to None.
#[derive(Deserialize)]
pub struct Boxed {
    pub note: Box<Option<String>>,
    pub n: u32,
}

pub async fn boxed(Json(b): Json<Boxed>) -> StatusCode {
    let _ = (b.note, b.n);
    StatusCode::ACCEPTED
}
