// Neither attribute resolves to tauri::command.
#[poise::command(slash_command)]
pub async fn poise_cmd() {}

#[command(name = "cli")]
pub fn clap_like() {}
