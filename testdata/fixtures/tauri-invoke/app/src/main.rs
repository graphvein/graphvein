mod bare;
mod commands;
mod negatives;
mod plugins;

// The kftray shape: the builder is rebound through a cfg-gated if/else, then
// `let app = app.…` reads the earlier `app` inside its own initializer.
// The function attributes are builtins and tauri::mobile_entry_point, which
// keep the body unchanged (Revision 2, m3).
#[cfg_attr(mobile, tauri::mobile_entry_point)]
#[allow(clippy::let_and_return)]
fn main() {
    let builder = tauri::Builder::default()
        .plugin(plugins::audit())
        .manage(0u32);
    #[cfg(feature = "e2e")]
    let builder = if std::env::var("E2E").is_ok() {
        builder.plugin(plugins::named())
    } else {
        builder
    };
    let app = builder.setup(|_app| Ok(()));
    let app = app
        .on_window_event(|_window, _event| {})
        .invoke_handler(tauri::generate_handler![
            commands::greet,
            bare::bare_cmd,
            commands::aliased,
            commands::desktop_only,
            commands::rename_me,
            #[cfg(debug_assertions)]
            commands::debug_cmd,
            commands::greet,
            #[cfg(test)]
            commands::testing::test_cmd,
            #[cfg(feature = "legacy")]
            commands::legacy_status,
            commands::status,
        ])
        .build(tauri::generate_context!())
        .expect("error while building the app");
    log::info!("started {}", env!("CARGO_PKG_NAME"));
    app.run(|_handle, _event| {});
}
