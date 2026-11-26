use crate::commands;

pub fn direct() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![commands::greet, commands::rooted])
        .run(tauri::generate_context!())
        .expect("error while running");
}

pub fn twice() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler!(commands::greet))
        .invoke_handler(tauri::generate_handler! {
            commands::plain::plain_bare,
            commands::grouped::grouped_bare,
        })
        .build(tauri::generate_context!())
        .unwrap();
}

// The kftray shape: a cfg-gated if/else rebinding, then two app rebindings.
pub fn rebound() {
    let builder = tauri::Builder::default().manage(1u8);

    #[cfg(feature = "e2e")]
    let builder = if std::env::var_os("E2E").is_some() {
        builder.manage(2u16)
    } else {
        builder
    };

    let app = builder.setup(|_| Ok(()));

    let app = app
        .plugin({
            #[cfg(target_os = "linux")]
            {
                plugin_a()
            }
            #[cfg(not(target_os = "linux"))]
            {
                plugin_b()
            }
        })
        .invoke_handler(tauri::generate_handler![
            commands::greet,
            #[cfg(debug_assertions)]
            commands::debug_only,
            commands::arguments::r#type,
        ])
        .build(tauri::generate_context!())
        .expect("error while building");

    app.run(|_, _| {});
}

pub fn mutable() {
    let mut b = tauri::Builder::<tauri::Wry>::default();
    b = b.manage(3u32);
    let unrelated = 4u64;
    b = b.invoke_handler(tauri::generate_handler![commands::aliased::aliased]);
    let _ = unrelated;
    b.run(tauri::generate_context!()).unwrap();
}

pub fn matched(kind: u8) {
    let base = tauri::Builder::default();
    let chosen = match kind {
        0 => base.manage(5u8),
        1 => {
            base
        }
        _ => {
            panic!("unsupported");
        }
    };
    chosen
        .invoke_handler(tauri::generate_handler![commands::optional])
        .run(tauri::generate_context!())
        .unwrap();
}
