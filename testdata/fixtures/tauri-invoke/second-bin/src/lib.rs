// Probes b0-bl (T015c2 Revision 3): one crate, one shared
// generate_context!, the proven app in main.rs and a second app per
// src/bin file. The re-export is itself app evidence (bg).
pub use tauri::Builder as AppBuilder;
pub fn context() -> tauri::Context<tauri::Wry> { tauri::generate_context!() }
