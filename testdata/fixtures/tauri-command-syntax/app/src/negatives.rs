// Clap's derive helper attribute is not on a function item: no record.
#[derive(clap::Parser)]
#[command(name = "x")]
pub struct Cli {
    #[command(subcommand)]
    pub action: Option<String>,
}

// Captured syntactically; T015c2 rejects it by resolution.
#[poise::command(slash_command)]
pub async fn poise_cmd() {}

// A handler inside a macro_rules! body is an opaque token tree: not captured.
macro_rules! handlers {
    () => {
        tauri::generate_handler![crate::commands::greet]
    };
}

pub fn use_macro() {
    let _ = handlers!();
}

// A command function inside a macro_rules! body is not captured either.
macro_rules! define_command {
    () => {
        #[tauri::command]
        pub fn in_macro() {}
    };
}

define_command!();
