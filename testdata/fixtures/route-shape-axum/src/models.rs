use std::collections::HashMap;

use serde::{Deserialize, Serialize};

/// Response item: `note` is left out when empty, `kind` is an enum.
#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Item {
    pub id: u32,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub note: Option<String>,
    pub tags: Vec<String>,
    pub kind: Kind,
    #[serde(skip_serializing)]
    pub secret: String,
}

#[derive(Serialize, Deserialize, Clone, Copy, Debug, Default)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum Kind {
    #[default]
    Book,
    LongPlay,
    #[serde(rename = "tape")]
    Cassette,
}

/// Request body with container and field attributes.
#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct NewItem {
    pub item_name: String,
    #[serde(rename = "type")]
    pub kind: Kind,
    #[serde(default)]
    pub tags: Vec<String>,
    pub note: Option<String>,
    #[serde(skip)]
    pub internal: u8,
    pub price_cents: i64,
    pub ratio: f64,
    pub meta: Option<Meta>,
    pub labels: HashMap<String, String>,
    #[serde(alias = "flagged")]
    pub is_flagged: bool,
}

#[derive(Deserialize, Debug)]
pub struct Meta {
    pub source: String,
    pub weight: Option<f32>,
}

/// Query parameters: `limit` defaults, `cursor` is optional.
#[derive(Deserialize, Debug)]
pub struct Page {
    pub q: String,
    #[serde(default)]
    pub limit: u32,
    pub cursor: Option<String>,
    #[serde(rename = "sort-by")]
    pub sort_by: Option<SortOrder>,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "lowercase")]
pub enum SortOrder {
    Asc,
    Desc,
}

#[derive(Deserialize, Debug)]
pub struct TagParams {
    pub id: u32,
    #[serde(rename = "tag")]
    pub label: String,
}

#[derive(Deserialize, Debug)]
#[serde(default)]
pub struct Login {
    pub user: String,
    pub remember: bool,
}

impl Default for Login {
    fn default() -> Self {
        Login { user: "anonymous".to_string(), remember: false }
    }
}

#[derive(Serialize, Debug)]
pub struct TagResult {
    pub id: u32,
    pub label: String,
}

/// A generic envelope instantiated by the handlers.
#[derive(Serialize, Deserialize, Debug)]
pub struct Envelope<T> {
    pub data: T,
    pub count: usize,
}

#[derive(Serialize, Debug)]
pub struct ErrorBody {
    pub message: String,
}

#[derive(Serialize, Debug)]
#[serde(rename_all = "kebab-case")]
pub struct Health {
    pub up_since: u64,
    pub ok: bool,
}

/// Untagged and internally tagged enums are not read (unknown).
#[derive(Deserialize, Debug)]
#[serde(untagged)]
pub enum Event {
    Click { x: i32, y: i32 },
    Key(String),
}

#[derive(Deserialize, Debug)]
pub struct Batch {
    pub events: Vec<Event>,
    #[serde(flatten)]
    pub extra: HashMap<String, String>,
}
