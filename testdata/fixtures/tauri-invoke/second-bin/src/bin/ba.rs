// Probe ba: type alias.
type B = tauri::Builder<tauri::Wry>;
fn main() { B::default().run(probe_app::context()).expect("run"); }
