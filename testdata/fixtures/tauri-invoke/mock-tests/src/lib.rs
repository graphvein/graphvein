// Test-only builders neither prove nor block the production app
// (Revision 2, M2 and m5): a registry inside a #[cfg(test)] module, and
// mock_builder registries in tests/.
#[tauri::command]
pub fn ping() {}

#[cfg(test)]
mod tests {
    #[test]
    fn builds() {
        tauri::Builder::default()
            .invoke_handler(tauri::generate_handler![crate::ping])
            .build(tauri::generate_context!())
            .unwrap();
    }
}
