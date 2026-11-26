const name = window.location.hash

window.__TAURI__.core.invoke('greet')
window.__TAURI_INTERNALS__.invoke(name)
