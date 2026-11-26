// Probe bc: use rename.
use tauri::Builder as B;
fn main() { B::default().run(probe_app::context()).expect("run"); }
