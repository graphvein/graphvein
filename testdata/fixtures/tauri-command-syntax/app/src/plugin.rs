use tauri::plugin::{Builder, TauriPlugin};
use tauri::{command, Runtime};

#[command]
pub fn ping() {}

pub fn init<R: Runtime>() -> TauriPlugin<R> {
    Builder::<R>::new("my-plugin")
        .invoke_handler(tauri::generate_handler![#![plugin(my_plugin)] ping])
        .build()
}

pub fn scoped() -> TauriPlugin<tauri::Wry> {
    tauri::plugin::Builder::new("x")
        .invoke_handler(tauri::generate_handler![self::ping])
        .build()
}
