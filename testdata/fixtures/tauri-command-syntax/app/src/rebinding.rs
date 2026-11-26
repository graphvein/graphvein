// Rebinding and replacement shapes that T015c1 added later (syntax
// only; the methods need not exist).
use crate::commands;

pub struct Holder {
    builder: tauri::Builder<tauri::Wry>,
}

// Only a plain local is an `assign` use; a field or deref target is `other`.
impl Holder {
    pub fn store(&mut self) {
        self.builder = tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
    }
}

pub fn store_deref(slot: &mut tauri::Builder<tauri::Wry>) {
    *slot = tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
}

// A later invoke_handler whose argument is not a captured generate_handler!
// still replaces the handler, so its chain is recorded as Uncaptured.
pub fn replaced_by_binding() {
    let app = tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
    let handler = tauri::generate_handler![commands::rooted];
    app.invoke_handler(handler).run(tauri::generate_context!()).unwrap();
}

pub fn replaced_by_closure() {
    let app = tauri::Builder::default().invoke_handler(tauri::generate_handler![commands::greet]);
    app.invoke_handler(|_invoke| true).run(tauri::generate_context!()).unwrap();
}

// A match guard pattern binds the flow name at the root site.
pub fn guard_shadow(other: Option<u8>) {
    let builder = tauri::Builder::default();
    match other {
        Some(value) if let Some(builder) = lookup(value) => {
            builder.invoke_handler(tauri::generate_handler![commands::greet]);
        }
        _ => {}
    }
}

macro_rules! rebind {
    ($name:ident) => {
        let $name = tauri::Builder::default();
    };
}

// A macro may rebind any identifier in its tokens.
pub fn macro_shadow() {
    let builder = tauri::Builder::default().manage(1u8);
    rebind!(builder);
    builder.invoke_handler(tauri::generate_handler![commands::greet]);
}

// cfg on an enclosing block or statement reaches the bindings inside, and
// bindings below the top-level statement list are Conditional.
pub fn gated_blocks(flag: bool) {
    let mut builder = tauri::Builder::default();
    #[cfg(feature = "extra")]
    {
        builder = builder.manage(2u16);
    }
    #[cfg_attr(feature = "gate", cfg(unix))]
    let builder = builder.manage(3u32);
    let mut builder = builder;
    if flag {
        builder = builder.manage(4u64);
    }
    builder.invoke_handler(tauri::generate_handler![commands::greet]);
}
