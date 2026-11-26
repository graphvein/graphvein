// The only app registry sits in a test file. An unresolved entry precedes
// pong, so pong may be shadowed by it.
use tauri::command;

#[command]
fn ping() {}

#[command]
fn pong() {}

fn main() {
    tauri::Builder::new()
        .invoke_handler(tauri::generate_handler![ping, missing::thing, pong])
        .run(tauri::generate_context!())
        .unwrap();
}
