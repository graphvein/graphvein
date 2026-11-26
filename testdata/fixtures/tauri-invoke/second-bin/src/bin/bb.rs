// Probe bb: Default::default() typed as Builder.
fn main() { let b: tauri::Builder<tauri::Wry> = Default::default(); b.run(probe_app::context()).expect("run"); }
