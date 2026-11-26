// Probe bk: glob import.
use tauri::*;
fn main() { Builder::default().run(probe_app::context()).expect("run"); }
