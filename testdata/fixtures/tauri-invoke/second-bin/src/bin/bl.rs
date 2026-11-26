// Probe bl: renamed turbofish.
use tauri::{Builder as B, Wry};
fn main() { B::<Wry>::default().run(probe_app::context()).expect("run"); }
