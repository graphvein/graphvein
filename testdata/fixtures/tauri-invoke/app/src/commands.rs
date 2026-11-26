use tauri::command as cmd;

#[tauri::command]
pub fn greet(name: String) -> String {
    format!("hello {name}")
}

#[cmd]
pub fn aliased() {}

#[cfg_attr(feature = "desktop", tauri::command)]
pub fn desktop_only() {}

// The last rename wins.
#[tauri::command(rename = "first", rename = "renamed")]
pub fn rename_me() {}

// A non-literal rename does not compile; the name is unknown.
#[tauri::command(rename = NAME)]
pub fn dynamic_name() {}

const NAME: &str = "dynamic";

#[tauri::command]
pub fn debug_cmd() {}

#[cfg(test)]
pub mod testing {
    #[tauri::command]
    pub fn test_cmd() {}
}

// Two arms may carry `status`: the cfg-gated legacy arm comes first, so each
// is the target in some build (Revision 2, M1).
#[tauri::command(rename = "status")]
pub fn legacy_status() {}

#[tauri::command]
pub fn status() {}
