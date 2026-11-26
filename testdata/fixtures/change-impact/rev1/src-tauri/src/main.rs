#[tauri::command]
fn greet(name: String) -> String {
    format!("Hello {}", name)
}

#[tauri::command]
fn farewell() -> String {
    "bye".into()
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, farewell])
        .run(tauri::generate_context!())
        .expect("error while running");
}
