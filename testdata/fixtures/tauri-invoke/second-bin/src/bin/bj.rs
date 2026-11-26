// Probe bj: macro_rules! body.
macro_rules! app { () => { tauri::Builder::default() } }
fn main() { app!().run(probe_app::context()).expect("run"); }
