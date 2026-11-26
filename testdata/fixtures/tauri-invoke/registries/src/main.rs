// Each function is one registry the proof rejects; the reason is in the
// comment. None of them is the app's registry.
use tauri::{command, generate_context, generate_handler, Builder};

#[command]
fn ping() {}

// G1: another crate's generate_handler is not Tauri's (no node).
fn foreign() {
    Builder::default()
        .invoke_handler(my_macros::generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// Invalid token tree (missing comma): tauri_handler_invalid.
fn malformed() {
    Builder::default()
        .invoke_handler(generate_handler![ping ping])
        .run(generate_context!())
        .unwrap();
}

// G2 [context]: path-form invoke_handler.
fn path_form() {
    Builder::invoke_handler(Builder::default(), generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// G2 [context]: a let-bound handler.
fn let_bound() {
    let handler = generate_handler![ping];
    Builder::default().invoke_handler(handler).run(generate_context!()).unwrap();
}

// G3 [incomplete]: `?` inside the chain.
fn incomplete() -> tauri::Result<()> {
    Builder::default()
        .setup(|_app| Ok(()))
        .any_thread()
        .try_into_builder()?
        .invoke_handler(generate_handler![ping])
        .run(generate_context!())
}

// G4 [not_last] for the first handler; the second is never consumed (G5).
fn later_handler() -> Builder<tauri::Wry> {
    Builder::default()
        .invoke_handler(generate_handler![ping])
        .invoke_handler(generate_handler![ping])
}

// G5 [not_consumed]: the builder escapes through its binding.
fn escapes() {
    let app = Builder::default().invoke_handler(generate_handler![ping]);
    install(app);
}

// G6 [method]: an extension-trait method on the chain.
fn extension() {
    Builder::default()
        .with_defaults()
        .invoke_handler(generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// G7 [generic]: a turbofish root.
fn generic_root() {
    Builder::<tauri::Wry>::new()
        .invoke_handler(generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// G7 [root]: a builder from a helper.
fn helper_root() {
    make_builder()
        .invoke_handler(generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// G8 [flow_binding]: an if without else has no value on every path.
fn partial_if(flag: bool) {
    let builder = if flag { Builder::default() };
    builder.invoke_handler(generate_handler![ping]).run(generate_context!()).unwrap();
}

// G9 [shadowed]: an if-let rebinds the root name.
fn shadowed(other: Option<Builder<tauri::Wry>>) {
    let builder = Builder::default();
    if let Some(builder) = other {
        builder.invoke_handler(generate_handler![ping]).run(generate_context!()).unwrap();
    }
}

// G10: a destructuring let is never a recorded binding; it shadows the name.
fn destructured() {
    let builder = Builder::default();
    let (builder, _count) = (builder, 1);
    builder.invoke_handler(generate_handler![ping]).run(generate_context!()).unwrap();
}

// G11 [macro]: a macro_rules! in the body.
fn body_macro() {
    macro_rules! noop {
        () => {};
    }
    noop!();
    let builder = Builder::default();
    builder.invoke_handler(generate_handler![ping]).run(generate_context!()).unwrap();
}

// G11 [macro]: an opaque proc-macro invocation in the body.
fn opaque_macro() {
    let builder = Builder::default();
    my_macros::configure!();
    builder.invoke_handler(generate_handler![ping]).run(generate_context!()).unwrap();
}

// G11 [macro]: an attribute macro on the function may rewrite its body
// (Revision 2, m3).
#[my_macros::rewrite]
fn attribute_macro() {
    Builder::default()
        .invoke_handler(generate_handler![ping])
        .run(generate_context!())
        .unwrap();
}

// [mock_builder]: tauri::test::mock_builder hosts no webview; the registry
// neither proves nor blocks an app (Revision 2, M2).
fn mock_root() {
    tauri::test::mock_builder()
        .invoke_handler(generate_handler![ping])
        .build(generate_context!())
        .unwrap();
}

// [plugin_name]: #![plugin(y)] names a different plugin than the builder.
fn plugin_mismatch() -> tauri::plugin::TauriPlugin<tauri::Wry> {
    tauri::plugin::Builder::new("x")
        .invoke_handler(generate_handler![#![plugin(y)] ping])
        .build()
}

fn main() {}
