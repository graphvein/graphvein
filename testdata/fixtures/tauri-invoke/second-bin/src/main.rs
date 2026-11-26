#[tauri::command]
fn greet() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet])
        .run(probe_app::context())
        .expect("run");
}
