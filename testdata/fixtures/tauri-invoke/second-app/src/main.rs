// A second proven app registry: a direct chain consumed by run, in a
// tokio::main function (the attribute keeps the body, Revision 2, m3).
#[tauri::command]
fn greet() {}

#[tokio::main]
async fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet])
        .run(tauri::generate_context!())
        .expect("error while running the second app");
}
