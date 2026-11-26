// Probe b0: plain path.
fn main() { tauri::Builder::default().run(probe_app::context()).expect("run"); }
