// Probe e1 (T015c2 Revision 3): cfg(P) and cfg(not(P)) arms
// cover every build, so the ungated greet is unreachable.
#[tauri::command(rename = "greet")]
fn g1() {}
#[tauri::command(rename = "greet")]
fn g2() {}
#[tauri::command]
fn greet() {}
fn main() {
    tauri::Builder::default().invoke_handler(tauri::generate_handler![
        #[cfg(feature = "a")] g1,
        #[cfg(not(feature = "a"))] g2,
        greet
    ]).run(tauri::generate_context!()).expect("run");
}
