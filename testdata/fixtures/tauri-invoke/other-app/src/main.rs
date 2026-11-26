// Another app whose invoke handler is a closure, not generate_handler!: the
// frontend may belong to it, so app invokes are not proven (Revision 2, M2).
fn main() {
    tauri::Builder::default()
        .invoke_handler(|invoke| {
            let _ = invoke;
            true
        })
        .run(tauri::generate_context!())
        .expect("error while running the other app");
}
