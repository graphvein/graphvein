// Probe bi: function path value.
fn main() { let mk = tauri::Builder::<tauri::Wry>::default; mk().run(probe_app::context()).expect("run"); }
