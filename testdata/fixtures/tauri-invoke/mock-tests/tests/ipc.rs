use tauri::test::mock_builder;

#[tauri::command]
fn pong() {}

#[test]
fn mocked() {
    mock_builder()
        .invoke_handler(tauri::generate_handler![pong])
        .build(tauri::generate_context!())
        .unwrap();
}

#[test]
fn mocked_binding() {
    let app = tauri::test::mock_builder();
    app.invoke_handler(tauri::generate_handler![pong])
        .build(tauri::generate_context!())
        .unwrap();
}
