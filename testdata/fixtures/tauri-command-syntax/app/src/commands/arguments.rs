const NAME: &str = "computed";

#[tauri::command(rename = "renamed", rename_all = "snake_case")]
pub fn with_rename() {}

#[tauri::command(root = "crate", async)]
pub fn with_root() {}

#[tauri::command(rename = r#"raw"#,)]
pub async fn raw_literal() {}

#[tauri::command(rename = NAME)]
pub fn non_literal() {}

#[tauri::command(unknown)]
pub fn bare_path() {}

#[tauri::command]
pub fn r#type() {}
