use crate::commands;

pub fn empty() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![])
        .run(tauri::generate_context!())
        .unwrap();
}

pub fn malformed() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![commands::greet commands::rooted])
        .run(tauri::generate_context!())
        .unwrap();
}

pub fn let_bound() {
    let handler = tauri::generate_handler![commands::greet, crate::commands::inline::nested_module];
    tauri::Builder::default()
        .invoke_handler(handler)
        .run(tauri::generate_context!())
        .unwrap();
}
