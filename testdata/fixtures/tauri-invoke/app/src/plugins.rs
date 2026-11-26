use tauri::plugin::{Builder, TauriPlugin};
use tauri::Runtime;

// A plugin registry named by its builder.
pub fn audit<R: Runtime>() -> TauriPlugin<R> {
    Builder::new("audit")
        .setup(|_app, _api| Ok(()))
        .invoke_handler(tauri::generate_handler![crate::commands::greet, record])
        .build()
}

// A plugin registry with #![plugin(a_b)], which names it a-b; its entries
// hold the commands whose names are unknown or that are not commands.
pub fn named<R: Runtime>() -> TauriPlugin<R> {
    Builder::new("a-b")
        .invoke_handler(tauri::generate_handler![
            #![plugin(a_b)]
            crate::commands::dynamic_name,
            crate::bare::twice,
            crate::negatives::poise_cmd,
            crate::negatives::clap_like,
        ])
        .build()
}

#[tauri::command]
fn record() {}
