#[tauri::command]
fn greet() {}
#[tauri::command]
fn other() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, other])
        .run(tauri::generate_context!())
        .expect("run");
}
