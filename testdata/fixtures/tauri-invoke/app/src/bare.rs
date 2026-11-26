use tauri::command;

#[command]
pub async fn bare_cmd() {}

// Two Tauri attributes do not compile: not a command.
#[command]
#[tauri::command]
pub fn twice() {}
