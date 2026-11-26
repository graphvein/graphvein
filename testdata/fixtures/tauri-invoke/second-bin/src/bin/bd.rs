// Probe bd: turbofish.
fn main() { tauri::Builder::<tauri::Wry>::new().run(probe_app::context()).expect("run"); }
