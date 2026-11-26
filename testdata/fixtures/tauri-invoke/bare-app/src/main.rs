// Another app without an invoke handler, through an aliased Builder: its
// constructor, its generate_context! and its config file are app evidence
// (Revision 2, M2).
use tauri::Builder as App;

fn main() {
    App::new()
        .run(tauri::generate_context!())
        .expect("error while running the bare app");
}
