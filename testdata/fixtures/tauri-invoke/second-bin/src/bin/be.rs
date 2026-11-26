// Probe be: qualified path.
fn main() { <tauri::Builder<tauri::Wry>>::default().run(probe_app::context()).expect("run"); }
