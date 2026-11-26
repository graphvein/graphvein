// Probe bg: re-export through the library.
fn main() { probe_app::AppBuilder::default().run(probe_app::context()).expect("run"); }
