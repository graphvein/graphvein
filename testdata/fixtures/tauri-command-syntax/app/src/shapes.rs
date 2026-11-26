// Chain shapes and binders that T015c1 added later (syntax only; the
// methods need not exist).
use crate::commands;

// Parentheses are transparent: both invoke_handler calls are on one chain.
pub fn parenthesized() {
    (tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]))
        .invoke_handler(tauri::generate_handler![commands::rooted])
        .run(tauri::generate_context!())
        .unwrap();
}

// `?` inside a chain keeps every method and marks the chain incomplete.
pub fn tried() -> Result<(), tauri::Error> {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![commands::greet])
        .checked()?
        .invoke_handler(tauri::generate_handler![commands::rooted])
        .run(tauri::generate_context!())?;
    Ok(())
}

// `.await` below the first method: the root is kept, the chain is incomplete.
pub async fn awaited() {
    make_async()
        .await
        .invoke_handler(tauri::generate_handler![commands::greet])
        .run(tauri::generate_context!())
        .unwrap();
}

// The chain's value escapes as a tail, a return value or an argument.
pub fn make() -> tauri::Builder<tauri::Wry> {
    tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet])
}

pub fn from_make() {
    make()
        .invoke_handler(tauri::generate_handler![commands::rooted])
        .run(tauri::generate_context!())
        .unwrap();
}

pub fn returned() -> tauri::Builder<tauri::Wry> {
    return tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
}

pub fn passed() {
    helper(tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]));
}

// An unrecognized consumer (a reference) makes the chain incomplete.
pub fn borrowed() {
    let _ = &tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
}

// Binders other than plain let/assign that may hide a flow name at a root.
pub fn if_let_shadow(other: Option<tauri::Builder<tauri::Wry>>) {
    let builder = tauri::Builder::default();
    if let Some(builder) = other {
        builder
            .invoke_handler(tauri::generate_handler![commands::greet])
            .run(tauri::generate_context!())
            .unwrap();
    }
    let _ = builder;
}

pub fn closure_shadow() {
    let builder = tauri::Builder::default();
    let wrap = |builder: tauri::Builder<tauri::Wry>| builder.invoke_handler(tauri::generate_handler![commands::greet]);
    wrap(builder).run(tauri::generate_context!()).unwrap();
}

pub fn match_shadow(other: Option<tauri::Builder<tauri::Wry>>) {
    let b = tauri::Builder::default();
    match other {
        Some(b) => b.invoke_handler(tauri::generate_handler![commands::greet]),
        None => b,
    }
    .run(tauri::generate_context!())
    .unwrap();
}

pub fn for_shadow(all: Vec<tauri::Builder<tauri::Wry>>) {
    let builder = tauri::Builder::default();
    for builder in all {
        builder.invoke_handler(tauri::generate_handler![commands::greet]);
    }
    let _ = builder;
}

pub fn destructured_shadow() {
    let builder = tauri::Builder::default();
    let (builder, _) = (builder, 0u8);
    builder.invoke_handler(tauri::generate_handler![commands::greet]);
}

pub fn param_shadow(builder: tauri::Builder<tauri::Wry>) {
    let app = builder;
    app.invoke_handler(tauri::generate_handler![commands::greet]);
}

// A closure parameter that does not enclose the root site is no shadow (the
// kftray shape: `.setup(move |app| ..)` inside `let app = ..`).
pub fn unrelated_closure() {
    let app = tauri::Builder::default().setup(|app| {
        let _ = app;
        Ok(())
    });
    app.invoke_handler(tauri::generate_handler![commands::greet]);
}
