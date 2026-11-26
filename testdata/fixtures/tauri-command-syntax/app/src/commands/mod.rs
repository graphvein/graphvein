pub mod aliased;
pub mod arguments;
pub mod grouped;
pub mod plain;

#[tauri::command]
pub fn greet(name: &str) -> String {
    format!("hello {name}")
}

#[::tauri::command]
pub async fn rooted() {}

#[cfg_attr(feature = "desktop", tauri::command(rename = "optional_cmd"))]
pub fn optional() {}

/// Only registered in debug builds.
#[tauri::command]
#[cfg(debug_assertions)]
pub(crate) fn debug_only() {}

pub mod inline {
    #[tauri::command]
    pub fn nested_module() {}
}

pub fn outer() {
    #[tauri::command]
    fn nested_fn() {}
}

pub struct Service;

impl Service {
    #[tauri::command]
    pub fn member(&self) {}
}

// Not a command: no attribute, and an unrelated attribute.
pub fn helper() {}

#[inline]
pub fn inlined() {}

// A multi-line cfg_attr predicate is recorded with LF line endings.
#[cfg_attr(
    all(feature = "a",
        feature = "b"),
    tauri::command
)]
pub fn multiline() {}

// Nested cfg_attr: the predicates are combined in order.
#[cfg_attr(feature = "outer", cfg_attr(feature = "inner", tauri::command))]
pub fn nested_cfg_attr() {}

// Every qualifying attribute is recorded, not only the first.
#[poise::command]
#[tauri::command]
pub fn both_attributes() {}

// Known gap: the enclosing module's cfg is not reflected in Cfg.
#[cfg(test)]
mod gated {
    #[tauri::command]
    pub fn in_cfg_module() {}
}

// cfg applied through cfg_attr: Cfg is set, at any nesting depth.
#[cfg_attr(feature = "gate", cfg(unix))]
#[tauri::command]
pub fn cfg_through_cfg_attr() {}

#[cfg_attr(feature = "outer", cfg_attr(feature = "inner", cfg(unix)))]
#[tauri::command]
pub fn cfg_through_nested_cfg_attr() {}

// A cfg_attr that applies no cfg leaves Cfg unset.
#[cfg_attr(feature = "lint", allow(dead_code))]
#[tauri::command]
pub fn cfg_attr_without_cfg() {}
