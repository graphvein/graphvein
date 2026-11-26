use tauri::{
    AppHandle,
    Manager,
    command,
};

#[command]
pub async fn grouped_bare(app: AppHandle) {
    let _ = app.get_webview_window("main");
}
